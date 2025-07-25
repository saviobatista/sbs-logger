package nats

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/saviobatista/sbs-logger/internal/types"
)

const (
	SubjectSBSRaw = "sbs.raw"
)

// Interfaces for dependency injection
type NATSConnector interface {
	Connect(url string) (NATSConnection, error)
}

type NATSConnection interface {
	Close()
	JetStream() (JetStreamContext, error)
}

type JetStreamContext interface {
	AddStream(cfg *nats.StreamConfig) (*nats.StreamInfo, error)
	Publish(subj string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error)
	Subscribe(subj string, cb nats.MsgHandler, opts ...nats.SubOpt) (*nats.Subscription, error)
}

// Real implementations
type realNATSConnector struct{}

func (r *realNATSConnector) Connect(url string) (NATSConnection, error) {
	conn, err := nats.Connect(url)
	if err != nil {
		return nil, err
	}
	return &realNATSConnection{conn}, nil
}

type realNATSConnection struct {
	conn *nats.Conn
}

func (r *realNATSConnection) Close() {
	r.conn.Close()
}

func (r *realNATSConnection) JetStream() (JetStreamContext, error) {
	js, err := r.conn.JetStream()
	if err != nil {
		return nil, err
	}
	return &realJetStreamContext{js}, nil
}

type realJetStreamContext struct {
	js nats.JetStreamContext
}

func (r *realJetStreamContext) AddStream(cfg *nats.StreamConfig) (*nats.StreamInfo, error) {
	return r.js.AddStream(cfg)
}

func (r *realJetStreamContext) Publish(subj string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
	return r.js.Publish(subj, data, opts...)
}

func (r *realJetStreamContext) Subscribe(subj string, cb nats.MsgHandler, opts ...nats.SubOpt) (*nats.Subscription, error) {
	return r.js.Subscribe(subj, cb, opts...)
}

// Client represents a NATS client
type Client struct {
	conn      NATSConnection
	js        JetStreamContext
	connector NATSConnector
}

// Default connector for production use
var defaultConnector = &realNATSConnector{}

// New creates a new NATS client
func New(url string) (*Client, error) {
	return NewWithConnector(url, defaultConnector)
}

// NewWithConnector creates a new NATS client with a custom connector (for testing)
func NewWithConnector(url string, connector NATSConnector) (*Client, error) {
	conn, err := connector.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "SBS_RAW",
		Subjects: []string{SubjectSBSRaw},
		Storage:  nats.FileStorage,
		MaxAge:   24 * time.Hour,
	})
	if err != nil && !strings.Contains(err.Error(), "stream name already in use") {
		conn.Close()
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	return &Client{
		conn:      conn,
		js:        js,
		connector: connector,
	}, nil
}

// PublishSBSMessage publishes an SBS message to NATS
func (c *Client) PublishSBSMessage(msg *types.SBSMessage) error {
	if c.js == nil {
		return fmt.Errorf("JetStream context not initialized")
	}

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
	if c.js == nil {
		return fmt.Errorf("JetStream context not initialized")
	}

	if handler == nil {
		return fmt.Errorf("handler function cannot be nil")
	}

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
