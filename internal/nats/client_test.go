package nats

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/savio/sbs-logger/internal/types"
)

func TestNew_ValidURL(t *testing.T) {
	// This test requires a NATS server running on localhost:4222
	// For now, we'll test the function structure without actual connection
	url := "nats://localhost:4222"

	// Test that the function doesn't panic
	// Note: This will fail if NATS is not running, but that's expected
	client, err := New(url)
	if err != nil {
		// Expected if NATS is not running
		t.Logf("Expected error when NATS is not running: %v", err)
		return
	}

	if client == nil {
		t.Fatal("New() returned nil client")
	}

	if client.conn == nil {
		t.Error("Expected NATS connection to be initialized")
	}

	if client.js == nil {
		t.Error("Expected JetStream context to be initialized")
	}

	// Clean up
	client.Close()
}

func TestNew_InvalidURL(t *testing.T) {
	url := "invalid://url:12345"

	client, err := New(url)
	if err == nil {
		t.Error("New() should fail with invalid URL")
		client.Close()
		return
	}

	if client != nil {
		t.Error("New() should return nil client on error")
	}
}

func TestNew_EmptyURL(t *testing.T) {
	url := ""

	client, err := New(url)
	if err == nil {
		t.Error("New() should fail with empty URL")
		client.Close()
		return
	}

	if client != nil {
		t.Error("New() should return nil client on error")
	}
}

func TestClient_Close(t *testing.T) {
	// Test close without initialization
	client := &Client{}

	// Close should not panic
	client.Close()
}

func TestClient_CloseWithConnection(t *testing.T) {
	// This test requires NATS to be running
	client, err := New("nats://localhost:4222")
	if err != nil {
		t.Skip("NATS not available, skipping test")
	}

	// Close should not panic
	client.Close()
}

func TestClient_PublishSBSMessage(t *testing.T) {
	// This test requires NATS to be running
	client, err := New("nats://localhost:4222")
	if err != nil {
		t.Skip("NATS not available, skipping test")
	}
	defer client.Close()

	msg := &types.SBSMessage{
		Raw:       "MSG,8,111,11111,111111,ABC123,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
		Timestamp: time.Now(),
		Source:    "test-source",
	}

	err = client.PublishSBSMessage(msg)
	if err != nil {
		t.Fatalf("PublishSBSMessage() failed: %v", err)
	}
}

func TestClient_PublishSBSMessage_NilMessage(t *testing.T) {
	// This test requires NATS to be running
	client, err := New("nats://localhost:4222")
	if err != nil {
		t.Skip("NATS not available, skipping test")
	}
	defer client.Close()

	err = client.PublishSBSMessage(nil)
	if err == nil {
		t.Error("PublishSBSMessage() should fail with nil message")
	}
}

func TestClient_SubscribeSBSRaw(t *testing.T) {
	// This test requires NATS to be running
	client, err := New("nats://localhost:4222")
	if err != nil {
		t.Skip("NATS not available, skipping test")
	}
	defer client.Close()

	messageReceived := make(chan *types.SBSMessage, 1)

	handler := func(msg *types.SBSMessage) {
		messageReceived <- msg
	}

	err = client.SubscribeSBSRaw(handler)
	if err != nil {
		t.Fatalf("SubscribeSBSRaw() failed: %v", err)
	}

	// Publish a test message
	testMsg := &types.SBSMessage{
		Raw:       "MSG,8,111,11111,111111,ABC123,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
		Timestamp: time.Now(),
		Source:    "test-source",
	}

	err = client.PublishSBSMessage(testMsg)
	if err != nil {
		t.Fatalf("PublishSBSMessage() failed: %v", err)
	}

	// Wait for message to be received
	select {
	case receivedMsg := <-messageReceived:
		if receivedMsg == nil {
			t.Fatal("Received nil message")
		}
		if receivedMsg.Raw != testMsg.Raw {
			t.Errorf("Expected Raw %s, got %s", testMsg.Raw, receivedMsg.Raw)
		}
		if receivedMsg.Source != testMsg.Source {
			t.Errorf("Expected Source %s, got %s", testMsg.Source, receivedMsg.Source)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestClient_SubscribeSBSRaw_NilHandler(t *testing.T) {
	// This test requires NATS to be running
	client, err := New("nats://localhost:4222")
	if err != nil {
		t.Skip("NATS not available, skipping test")
	}
	defer client.Close()

	err = client.SubscribeSBSRaw(nil)
	if err == nil {
		t.Error("SubscribeSBSRaw() should fail with nil handler")
	}
}

func TestClient_PublishAndSubscribe(t *testing.T) {
	// This test requires NATS to be running
	client, err := New("nats://localhost:4222")
	if err != nil {
		t.Skip("NATS not available, skipping test")
	}
	defer client.Close()

	messageReceived := make(chan *types.SBSMessage, 1)

	handler := func(msg *types.SBSMessage) {
		messageReceived <- msg
	}

	// Subscribe first
	err = client.SubscribeSBSRaw(handler)
	if err != nil {
		t.Fatalf("SubscribeSBSRaw() failed: %v", err)
	}

	// Publish multiple messages
	messages := []*types.SBSMessage{
		{
			Raw:       "MSG,8,111,11111,111111,ABC123,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
			Timestamp: time.Now(),
			Source:    "test-source-1",
		},
		{
			Raw:       "MSG,4,111,11111,111111,DEF456,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111",
			Timestamp: time.Now(),
			Source:    "test-source-2",
		},
	}

	for _, msg := range messages {
		err = client.PublishSBSMessage(msg)
		if err != nil {
			t.Fatalf("PublishSBSMessage() failed: %v", err)
		}
	}

	// Wait for messages to be received
	for i := 0; i < len(messages); i++ {
		select {
		case receivedMsg := <-messageReceived:
			if receivedMsg == nil {
				t.Fatal("Received nil message")
			}
			// Check that we received one of the expected messages
			found := false
			for _, expectedMsg := range messages {
				if receivedMsg.Raw == expectedMsg.Raw && receivedMsg.Source == expectedMsg.Source {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Received unexpected message: %+v", receivedMsg)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Timeout waiting for message %d", i+1)
		}
	}
}

func TestSubjectSBSRaw_Constant(t *testing.T) {
	// Test that the constant is defined correctly
	if SubjectSBSRaw != "sbs.raw" {
		t.Errorf("Expected SubjectSBSRaw to be 'sbs.raw', got %s", SubjectSBSRaw)
	}
}

func TestClient_ConnectionState(t *testing.T) {
	// This test requires NATS to be running
	client, err := New("nats://localhost:4222")
	if err != nil {
		t.Skip("NATS not available, skipping test")
	}
	defer client.Close()

	// Test that the connection is established
	if client.conn == nil {
		t.Fatal("Connection should be established")
	}

	// Test that JetStream context is available
	if client.js == nil {
		t.Fatal("JetStream context should be available")
	}
}

func TestClient_Reconnection(t *testing.T) {
	// This test requires NATS to be running
	client, err := New("nats://localhost:4222")
	if err != nil {
		t.Skip("NATS not available, skipping test")
	}
	defer client.Close()

	// Test that we can publish after connection
	msg := &types.SBSMessage{
		Raw:       "MSG,8,111,11111,111111,ABC123,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
		Timestamp: time.Now(),
		Source:    "test-source",
	}

	err = client.PublishSBSMessage(msg)
	if err != nil {
		t.Fatalf("PublishSBSMessage() failed: %v", err)
	}
}

func TestClient_MessageSerialization(t *testing.T) {
	// Test message serialization without NATS
	msg := &types.SBSMessage{
		Raw:       "MSG,8,111,11111,111111,ABC123,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
		Timestamp: time.Now(),
		Source:    "test-source",
	}

	// Test that the message can be marshaled
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	if len(data) == 0 {
		t.Error("Marshaled data should not be empty")
	}

	// Test that the message can be unmarshaled
	var unmarshaledMsg types.SBSMessage
	err = json.Unmarshal(data, &unmarshaledMsg)
	if err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	if unmarshaledMsg.Raw != msg.Raw {
		t.Errorf("Expected Raw %s, got %s", msg.Raw, unmarshaledMsg.Raw)
	}

	if unmarshaledMsg.Source != msg.Source {
		t.Errorf("Expected Source %s, got %s", msg.Source, unmarshaledMsg.Source)
	}
}
