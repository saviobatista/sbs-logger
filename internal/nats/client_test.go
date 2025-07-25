package nats

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/saviobatista/sbs-logger/internal/types"
)

// Mock implementations for testing
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
	return &nats.StreamInfo{}, nil
}

func (m *mockJetStream) Publish(subj string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
	if m.publishError != nil {
		return nil, m.publishError
	}
	return &nats.PubAck{}, nil
}

func (m *mockJetStream) Subscribe(subj string, cb nats.MsgHandler, opts ...nats.SubOpt) (*nats.Subscription, error) {
	if m.subscribeError != nil {
		return nil, m.subscribeError
	}
	// Simulate calling the callback with test data
	if cb != nil {
		testData, _ := json.Marshal(&types.SBSMessage{Raw: "test", Source: "test"})
		cb(&nats.Msg{Data: testData})
	}
	return &nats.Subscription{}, nil
}

// Test NewWithConnector function
func TestNewWithConnector(t *testing.T) {
	t.Run("connect fails", func(t *testing.T) {
		connector := &mockConnector{
			connectError: errors.New("connection failed"),
		}

		client, err := NewWithConnector("nats://test", connector)
		if err == nil {
			t.Error("Expected error")
		}
		if !strings.Contains(err.Error(), "failed to connect to NATS") {
			t.Errorf("Expected connect error, got: %v", err)
		}
		if client != nil {
			t.Error("Expected nil client")
		}
	})

	t.Run("jetstream fails", func(t *testing.T) {
		conn := &mockConnection{
			jetStreamError: errors.New("jetstream failed"),
		}
		connector := &mockConnector{
			connection: conn,
		}

		client, err := NewWithConnector("nats://test", connector)
		if err == nil {
			t.Error("Expected error")
		}
		if !strings.Contains(err.Error(), "failed to get JetStream context") {
			t.Errorf("Expected jetstream error, got: %v", err)
		}
		if client != nil {
			t.Error("Expected nil client")
		}
		if !conn.closed {
			t.Error("Expected connection to be closed")
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

		client, err := NewWithConnector("nats://test", connector)
		if err == nil {
			t.Error("Expected error")
		}
		if !strings.Contains(err.Error(), "failed to create stream") {
			t.Errorf("Expected stream error, got: %v", err)
		}
		if client != nil {
			t.Error("Expected nil client")
		}
		if !conn.closed {
			t.Error("Expected connection to be closed")
		}
	})

	t.Run("stream already exists (success)", func(t *testing.T) {
		js := &mockJetStream{
			addStreamError: errors.New("stream name already in use"),
		}
		conn := &mockConnection{
			jetStream: js,
		}
		connector := &mockConnector{
			connection: conn,
		}

		client, err := NewWithConnector("nats://test", connector)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if client == nil {
			t.Error("Expected client")
		}
		if conn.closed {
			t.Error("Expected connection to remain open")
		}
	})

	t.Run("success", func(t *testing.T) {
		js := &mockJetStream{}
		conn := &mockConnection{
			jetStream: js,
		}
		connector := &mockConnector{
			connection: conn,
		}

		client, err := NewWithConnector("nats://test", connector)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if client == nil {
			t.Error("Expected client")
		}
		if conn.closed {
			t.Error("Expected connection to remain open")
		}
	})
}

// Test New function (uses default connector)
func TestNew(t *testing.T) {
	// This will fail with real NATS server not running, which is expected
	client, err := New("nats://localhost:4222")
	if err == nil {
		// If it succeeds, clean up
		if client != nil {
			client.Close()
		}
	} else {
		// Expected when NATS is not running
		if !strings.Contains(err.Error(), "failed to connect to NATS") {
			t.Errorf("Expected connection error, got: %v", err)
		}
	}
}

// Test PublishSBSMessage
func TestPublishSBSMessage(t *testing.T) {
	t.Run("nil jetstream", func(t *testing.T) {
		client := &Client{js: nil}
		err := client.PublishSBSMessage(&types.SBSMessage{})
		if err == nil {
			t.Error("Expected error")
		}
		if !strings.Contains(err.Error(), "JetStream context not initialized") {
			t.Errorf("Expected jetstream error, got: %v", err)
		}
	})

	t.Run("publish fails", func(t *testing.T) {
		js := &mockJetStream{
			publishError: errors.New("publish failed"),
		}
		client := &Client{js: js}

		err := client.PublishSBSMessage(&types.SBSMessage{Raw: "test"})
		if err == nil {
			t.Error("Expected error")
		}
		if !strings.Contains(err.Error(), "failed to publish message") {
			t.Errorf("Expected publish error, got: %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		js := &mockJetStream{}
		client := &Client{js: js}

		err := client.PublishSBSMessage(&types.SBSMessage{Raw: "test"})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})
}

// Test SubscribeSBSRaw
func TestSubscribeSBSRaw(t *testing.T) {
	t.Run("nil jetstream", func(t *testing.T) {
		client := &Client{js: nil}
		err := client.SubscribeSBSRaw(func(*types.SBSMessage) {})
		if err == nil {
			t.Error("Expected error")
		}
		if !strings.Contains(err.Error(), "JetStream context not initialized") {
			t.Errorf("Expected jetstream error, got: %v", err)
		}
	})

	t.Run("nil handler", func(t *testing.T) {
		js := &mockJetStream{}
		client := &Client{js: js}

		err := client.SubscribeSBSRaw(nil)
		if err == nil {
			t.Error("Expected error")
		}
		if !strings.Contains(err.Error(), "handler function cannot be nil") {
			t.Errorf("Expected handler error, got: %v", err)
		}
	})

	t.Run("subscribe fails", func(t *testing.T) {
		js := &mockJetStream{
			subscribeError: errors.New("subscribe failed"),
		}
		client := &Client{js: js}

		err := client.SubscribeSBSRaw(func(*types.SBSMessage) {})
		if err == nil {
			t.Error("Expected error")
		}
		if !strings.Contains(err.Error(), "failed to subscribe") {
			t.Errorf("Expected subscribe error, got: %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		js := &mockJetStream{}
		client := &Client{js: js}

		messageReceived := false
		err := client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
			messageReceived = true
		})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if !messageReceived {
			t.Error("Expected message to be received")
		}
	})
}

// Test Close
func TestClose(t *testing.T) {
	t.Run("nil connection", func(t *testing.T) {
		client := &Client{conn: nil}
		client.Close() // Should not panic
	})

	t.Run("with connection", func(t *testing.T) {
		conn := &mockConnection{}
		client := &Client{conn: conn}

		client.Close()
		if !conn.closed {
			t.Error("Expected connection to be closed")
		}
	})
}
