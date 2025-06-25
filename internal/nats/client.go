package nats

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/savio/sbs-logger/internal/types"
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

// SubscribeSBSRaw subscribes to raw SBS messages
func (c *Client) SubscribeSBSRaw(handler func(*types.SBSMessage)) error {
	_, err := c.js.Subscribe(SubjectSBSRaw, func(msg *nats.Msg) {
		var sbsMsg types.SBSMessage
		if err := json.Unmarshal(msg.Data, &sbsMsg); err != nil {
			fmt.Printf("Error unmarshaling message: %v\n", err)
			return
		}
		handler(&sbsMsg)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	return nil
}

// Close closes the NATS connection
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}
