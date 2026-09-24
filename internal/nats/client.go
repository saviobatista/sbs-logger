package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/saviobatista/sbs-logger/internal/types"
)

const (
	SubjectSBSRaw = "sbs.raw"
)

// Client represents a NATS client
type Client struct {
	conn *nats.Conn
	js   nats.JetStreamContext
}

// New creates a new NATS client
func New(url string) (*Client, error) {
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	// Create stream if it doesn't exist
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "SBS_RAW",
		Subjects: []string{SubjectSBSRaw},
		Storage:  nats.FileStorage,
		MaxAge:   24 * time.Hour,
	})
	if err != nil && !strings.Contains(err.Error(), "stream name already in use") {
		nc.Close()
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	return &Client{
		conn: nc,
		js:   js,
	}, nil
}

// PublishSBSMessage publishes an SBS message to NATS
func (c *Client) PublishSBSMessage(msg *types.SBSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	_, err = c.js.Publish(SubjectSBSRaw, data)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// SubscribeSBSRaw subscribes to raw SBS messages, replaying the stream from
// its start (the logger wants every message it missed while down).
func (c *Client) SubscribeSBSRaw(handler func(*types.SBSMessage)) error {
	return c.subscribe(handler)
}

// SubscribeSBSRawFromNow subscribes to raw SBS messages published from now
// on. The tracker uses it: its job is the current state, and replaying a
// 24h stream at the receiver's own rate left it minutes behind, where every
// state it wrote was already "ended" by the 5-minute rule.
func (c *Client) SubscribeSBSRawFromNow(handler func(*types.SBSMessage)) error {
	return c.subscribe(handler, nats.DeliverNew())
}

func (c *Client) subscribe(handler func(*types.SBSMessage), opts ...nats.SubOpt) error {
	_, err := c.js.Subscribe(SubjectSBSRaw, func(msg *nats.Msg) {
		var sbsMsg types.SBSMessage
		if err := json.Unmarshal(msg.Data, &sbsMsg); err != nil {
			fmt.Printf("Error unmarshaling message: %v\n", err)
			return
		}
		handler(&sbsMsg)
	}, opts...)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	return nil
}

// Durable consumer names. Stable names let a restarted service resume from
// its last acknowledged message and tell the consumers apart in monitoring.
const (
	ConsumerLogger  = "sbs-logger"
	ConsumerTracker = "sbs-tracker"
)

// nakDelay is how long a failed batch waits before it is redelivered, so a
// database outage does not turn into a tight retry loop.
const nakDelay = 5 * time.Second

// ConsumeOptions configures Consume.
type ConsumeOptions struct {
	// Durable is the consumer name. The consumer survives disconnects and
	// restarts; the service resumes after its last acknowledged message.
	Durable string
	// Batch is the maximum number of messages handed to the handler at once.
	Batch int
	// MaxWait is how long a fetch waits for the first message.
	MaxWait time.Duration
	// AckWait is how long the server waits for the ack of a delivered
	// message before redelivering it. It must cover one handler call.
	AckWait time.Duration
}

func (o *ConsumeOptions) defaults() {
	if o.Batch <= 0 {
		o.Batch = 256
	}
	if o.MaxWait <= 0 {
		o.MaxWait = time.Second
	}
	if o.AckWait <= 0 {
		o.AckWait = time.Minute
	}
}

// Consume reads sbs.raw through a durable pull consumer and hands the
// messages to handler in batches, until ctx is done. The messages of a batch
// are acknowledged after handler returns nil and negatively acknowledged
// (redelivered) when it returns an error.
//
// A consumer created here starts with the messages published from now on
// (DeliverNew): the first start does not replay the whole 24h stream, and
// every later start resumes where the consumer stopped.
//
// Pull instead of push: the service asks for the next batch when it is done
// with the previous one, so a slow handler never has messages waiting in the
// client past AckWait (the old push consumers got those redelivered and
// processed twice), and a batch can be written in one go.
func (c *Client) Consume(ctx context.Context, opts ConsumeOptions, handler func([]*types.SBSMessage) error) error {
	if handler == nil {
		return fmt.Errorf("nil handler")
	}
	if opts.Durable == "" {
		return fmt.Errorf("durable consumer name is required")
	}
	opts.defaults()

	sub, err := c.js.PullSubscribe(SubjectSBSRaw, opts.Durable,
		nats.DeliverNew(),
		nats.AckExplicit(),
		nats.AckWait(opts.AckWait),
		nats.MaxAckPending(opts.Batch*4),
	)
	if err != nil {
		return fmt.Errorf("failed to create pull consumer %s: %w", opts.Durable, err)
	}
	// No Unsubscribe on exit: for a consumer the library created, it would
	// delete the durable consumer. Closing the connection keeps it.

	for ctx.Err() == nil {
		msgs, err := sub.Fetch(opts.Batch, nats.MaxWait(opts.MaxWait))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			if errors.Is(err, nats.ErrConnectionClosed) || errors.Is(err, nats.ErrBadSubscription) {
				return nil
			}
			fmt.Printf("Error fetching messages: %v\n", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			continue
		}

		batch := make([]*types.SBSMessage, 0, len(msgs))
		for _, m := range msgs {
			var sbsMsg types.SBSMessage
			if err := json.Unmarshal(m.Data, &sbsMsg); err != nil {
				fmt.Printf("Error unmarshaling message: %v\n", err)
				_ = m.Term() // never valid, do not redeliver
				continue
			}
			batch = append(batch, &sbsMsg)
		}
		if len(batch) == 0 {
			continue
		}

		if err := handler(batch); err != nil {
			fmt.Printf("Batch of %d messages failed, will be redelivered: %v\n", len(batch), err)
			for _, m := range msgs {
				_ = m.NakWithDelay(nakDelay)
			}
			continue
		}
		for _, m := range msgs {
			if err := m.Ack(); err != nil {
				fmt.Printf("Error acknowledging message: %v\n", err)
			}
		}
	}
	return nil
}

// Close closes the NATS connection
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}
