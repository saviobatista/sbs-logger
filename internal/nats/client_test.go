package nats

import (
	"errors"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/saviobatista/sbs-logger/internal/types"
)

// Simplified mock implementations for fast unit tests
type mockConnector struct {
	connectError error
	connection   *mockConnection
}

func (m *mockConnector) Connect(url string) (NATSConnection, error) {
	if m.connectError != nil {
		return nil, m.connectError
	}
	return m.connection, nil
}

type mockConnection struct {
	jetStreamError error
	jetStream      *mockJetStream
	closed         bool
}

func (m *mockConnection) Close() {
	m.closed = true
}

func (m *mockConnection) JetStream() (JetStreamContext, error) {
	if m.jetStreamError != nil {
		return nil, m.jetStreamError
	}
	return m.jetStream, nil
}

type mockJetStream struct {
	addStreamError error
	publishError   error
	subscribeError error
}

func (m *mockJetStream) AddStream(cfg *nats.StreamConfig) (*nats.StreamInfo, error) {
	if m.addStreamError != nil {
		return nil, m.addStreamError
	}
	return &nats.StreamInfo{Config: *cfg}, nil
}

func (m *mockJetStream) Publish(subj string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
	if m.publishError != nil {
		return nil, m.publishError
	}
	return &nats.PubAck{Stream: "SBS_RAW", Sequence: 1}, nil
}

func (m *mockJetStream) Subscribe(subj string, cb nats.MsgHandler, opts ...nats.SubOpt) (*nats.Subscription, error) {
	if m.subscribeError != nil {
		return nil, m.subscribeError
	}
	return &nats.Subscription{}, nil
}

// Test critical error paths for fast feedback
func TestNewWithConnector_ErrorPaths(t *testing.T) {
	t.Run("connection fails", func(t *testing.T) {
		connector := &mockConnector{
			connectError: errors.New("connection failed"),
		}

		client, err := NewWithConnector("nats://test", connector)
		if err == nil {
			t.Fatal("expected error when connection fails")
		}
		if client != nil {
			t.Fatal("expected nil client when connection fails")
		}
		if !strings.Contains(err.Error(), "failed to connect to NATS") {
			t.Errorf("expected connection error, got: %v", err)
		}
	})

	t.Run("jetstream fails", func(t *testing.T) {
		conn := &mockConnection{
			jetStreamError: errors.New("jetstream failed"),
		}
		connector := &mockConnector{
			connection: conn,
		}

		_, err := NewWithConnector("nats://test", connector)
		if err == nil {
			t.Fatal("expected error when jetstream fails")
		}
		if !conn.closed {
			t.Error("expected connection to be closed when jetstream fails")
		}
	})

	t.Run("stream creation fails", func(t *testing.T) {
		js := &mockJetStream{
			addStreamError: errors.New("stream creation failed"),
		}
		conn := &mockConnection{
			jetStream: js,
		}
		connector := &mockConnector{
			connection: conn,
		}

		_, err := NewWithConnector("nats://test", connector)
		if err == nil {
			t.Fatal("expected error when stream creation fails")
		}
		if !conn.closed {
			t.Error("expected connection to be closed when stream creation fails")
		}
	})

	t.Run("success path", func(t *testing.T) {
		js := &mockJetStream{}
		conn := &mockConnection{
			jetStream: js,
		}
		connector := &mockConnector{
			connection: conn,
		}

		client, err := NewWithConnector("nats://test", connector)
		if err != nil {
			t.Fatalf("expected no error on success, got: %v", err)
		}
		if client == nil {
			t.Fatal("expected client on success")
		}
	})
}

// Test business logic validation
func TestPublishSBSMessage_Validation(t *testing.T) {
	t.Run("nil jetstream", func(t *testing.T) {
		client := &Client{js: nil}

		err := client.PublishSBSMessage(&types.SBSMessage{Raw: "test"})
		if err == nil {
			t.Fatal("expected error with nil jetstream")
		}
		if !strings.Contains(err.Error(), "JetStream context not initialized") {
			t.Errorf("expected jetstream not initialized error, got: %v", err)
		}
	})

	t.Run("publish error", func(t *testing.T) {
		js := &mockJetStream{
			publishError: errors.New("publish failed"),
		}
		client := &Client{js: js}

		err := client.PublishSBSMessage(&types.SBSMessage{Raw: "test"})
		if err == nil {
			t.Fatal("expected error when publish fails")
		}
		if !strings.Contains(err.Error(), "failed to publish message") {
			t.Errorf("expected publish error, got: %v", err)
		}
	})
}

// Test subscription validation
func TestSubscribeSBSRaw_Validation(t *testing.T) {
	t.Run("nil jetstream", func(t *testing.T) {
		client := &Client{js: nil}

		err := client.SubscribeSBSRaw(func(*types.SBSMessage) {})
		if err == nil {
			t.Fatal("expected error with nil jetstream")
		}
		if !strings.Contains(err.Error(), "JetStream context not initialized") {
			t.Errorf("expected jetstream not initialized error, got: %v", err)
		}
	})

	t.Run("nil handler", func(t *testing.T) {
		js := &mockJetStream{}
		client := &Client{js: js}

		err := client.SubscribeSBSRaw(nil)
		if err == nil {
			t.Fatal("expected error with nil handler")
		}
		if !strings.Contains(err.Error(), "handler function cannot be nil") {
			t.Errorf("expected nil handler error, got: %v", err)
		}
	})

	t.Run("subscribe error", func(t *testing.T) {
		js := &mockJetStream{
			subscribeError: errors.New("subscribe failed"),
		}
		client := &Client{js: js}

		err := client.SubscribeSBSRaw(func(*types.SBSMessage) {})
		if err == nil {
			t.Fatal("expected error when subscribe fails")
		}
		if !strings.Contains(err.Error(), "failed to subscribe") {
			t.Errorf("expected subscribe error, got: %v", err)
		}
	})
}

// Test connection management
func TestClose(t *testing.T) {
	t.Run("nil connection", func(t *testing.T) {
		client := &Client{conn: nil}
		// Should not panic
		client.Close()
	})

	t.Run("with connection", func(t *testing.T) {
		conn := &mockConnection{}
		client := &Client{conn: conn}

		client.Close()
		if !conn.closed {
			t.Error("expected connection to be closed")
		}
	})
}
