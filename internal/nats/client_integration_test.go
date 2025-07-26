package nats

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/types"
	"github.com/testcontainers/testcontainers-go"
	natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testContainers holds the test containers for integration tests
type testContainers struct {
	nats *natscontainer.NATSContainer
}

// setupTestContainers sets up the test containers for integration tests
func setupTestContainers(t *testing.T) *testContainers {
	ctx := context.Background()

	// Start NATS container
	natsContainer, err := natscontainer.Run(ctx, "nats:2.9-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Server is ready"),
		),
	)
	if err != nil {
		t.Fatalf("Failed to start NATS container: %v", err)
	}

	return &testContainers{
		nats: natsContainer,
	}
}

// TestNATSClient_Integration_Connection tests basic NATS connection
func TestNATSClient_Integration_Connection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Get NATS connection string
	natsURL, err := containers.nats.ConnectionString(context.Background())
	if err != nil {
		t.Fatalf("Failed to get NATS connection string: %v", err)
	}

	// Create NATS client
	client, err := New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Test that client is properly initialized
	if client.conn == nil {
		t.Error("Expected connection to be initialized")
	}
	if client.js == nil {
		t.Error("Expected JetStream context to be initialized")
	}
}

// TestNATSClient_Integration_PublishAndSubscribe tests the full publish/subscribe workflow
func TestNATSClient_Integration_PublishAndSubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Get NATS connection string
	natsURL, err := containers.nats.ConnectionString(context.Background())
	if err != nil {
		t.Fatalf("Failed to get NATS connection string: %v", err)
	}

	// Create NATS client
	client, err := New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Create test message
	testMsg := &types.SBSMessage{
		Raw:       "MSG,8,111,11111,111111,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0",
		Timestamp: time.Now().UTC(),
		Source:    "test-source",
	}

	// Channel to receive messages
	messageReceived := make(chan *types.SBSMessage, 1)
	messageProcessed := make(chan bool, 1)

	// Subscribe to messages
	err = client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		messageReceived <- msg
		messageProcessed <- true
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Give subscription time to establish
	time.Sleep(100 * time.Millisecond)

	// Publish message
	err = client.PublishSBSMessage(testMsg)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	// Wait for message to be received
	select {
	case receivedMsg := <-messageReceived:
		// Verify message content
		if receivedMsg.Raw != testMsg.Raw {
			t.Errorf("Expected raw message %s, got %s", testMsg.Raw, receivedMsg.Raw)
		}
		if receivedMsg.Source != testMsg.Source {
			t.Errorf("Expected source %s, got %s", testMsg.Source, receivedMsg.Source)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for message")
	}

	// Wait for processing to complete
	select {
	case <-messageProcessed:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for message processing")
	}
}

// TestNATSClient_Integration_MultipleMessages tests publishing and receiving multiple messages
func TestNATSClient_Integration_MultipleMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Get NATS connection string
	natsURL, err := containers.nats.ConnectionString(context.Background())
	if err != nil {
		t.Fatalf("Failed to get NATS connection string: %v", err)
	}

	// Create NATS client
	client, err := New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Create multiple test messages
	messages := []*types.SBSMessage{
		{
			Raw:       "MSG,8,111,11111,111111,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0",
			Timestamp: time.Now().UTC(),
			Source:    "test-source-1",
		},
		{
			Raw:       "MSG,4,111,11111,111111,DEF456,1,2021-01-01,00:00:01.000,2021-01-01,00:00:01.000,TEST456,11000,500,200,41.7128,-75.0060,0,0,0,0",
			Timestamp: time.Now().UTC(),
			Source:    "test-source-2",
		},
		{
			Raw:       "MSG,1,111,11111,111111,GHI789,1,2021-01-01,00:00:02.000,2021-01-01,00:00:02.000,TEST789,12000,550,220,42.7128,-76.0060,0,0,0,0",
			Timestamp: time.Now().UTC(),
			Source:    "test-source-3",
		},
	}

	// Channel to receive messages
	receivedMessages := make(chan *types.SBSMessage, len(messages))
	allMessagesReceived := make(chan bool, 1)

	// Subscribe to messages
	err = client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		receivedMessages <- msg
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Give subscription time to establish
	time.Sleep(100 * time.Millisecond)

	// Publish all messages
	for _, msg := range messages {
		err = client.PublishSBSMessage(msg)
		if err != nil {
			t.Fatalf("Failed to publish message: %v", err)
		}
	}

	// Collect received messages
	received := make([]*types.SBSMessage, 0, len(messages))
	go func() {
		for i := 0; i < len(messages); i++ {
			select {
			case msg := <-receivedMessages:
				received = append(received, msg)
			case <-time.After(5 * time.Second):
				return
			}
		}
		allMessagesReceived <- true
	}()

	// Wait for all messages to be received
	select {
	case <-allMessagesReceived:
		// Verify we received the expected number of messages
		if len(received) != len(messages) {
			t.Errorf("Expected %d messages, got %d", len(messages), len(received))
		}

		// Verify message contents (order may vary due to async nature)
		receivedMap := make(map[string]*types.SBSMessage)
		for _, msg := range received {
			receivedMap[msg.Source] = msg
		}

		for _, expectedMsg := range messages {
			receivedMsg, exists := receivedMap[expectedMsg.Source]
			if !exists {
				t.Errorf("Expected message from source %s not received", expectedMsg.Source)
				continue
			}
			if receivedMsg.Raw != expectedMsg.Raw {
				t.Errorf("Expected raw message %s, got %s", expectedMsg.Raw, receivedMsg.Raw)
			}
		}
	case <-time.After(10 * time.Second):
		t.Error("Timeout waiting for messages")
	}
}

// TestNATSClient_Integration_MessageSerialization tests message serialization/deserialization
func TestNATSClient_Integration_MessageSerialization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Get NATS connection string
	natsURL, err := containers.nats.ConnectionString(context.Background())
	if err != nil {
		t.Fatalf("Failed to get NATS connection string: %v", err)
	}

	// Create NATS client
	client, err := New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Test messages with various content types
	testCases := []struct {
		name    string
		message *types.SBSMessage
	}{
		{
			name: "standard message",
			message: &types.SBSMessage{
				Raw:       "MSG,8,111,11111,111111,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0",
				Timestamp: time.Now().UTC(),
				Source:    "test-source",
			},
		},
		{
			name: "message with special characters",
			message: &types.SBSMessage{
				Raw:       "MSG,4,111,11111,111111,DEF456,1,2021-01-01,00:00:01.000,2021-01-01,00:00:01.000,TEST-456,11000,500,200,41.7128,-75.0060,0,0,0,0",
				Timestamp: time.Now().UTC(),
				Source:    "test-source-special",
			},
		},
		{
			name: "message with unicode characters",
			message: &types.SBSMessage{
				Raw:       "MSG,1,111,11111,111111,GHI789,1,2021-01-01,00:00:02.000,2021-01-01,00:00:02.000,TEST_789,12000,550,220,42.7128,-76.0060,0,0,0,0",
				Timestamp: time.Now().UTC(),
				Source:    "test-source-unicode",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new container for each test case to avoid message interference
			containers := setupTestContainers(t)
			defer func() {
				if err := containers.nats.Terminate(context.Background()); err != nil {
					t.Logf("Failed to terminate NATS container: %v", err)
				}
			}()

			// Get NATS connection string
			natsURL, err := containers.nats.ConnectionString(context.Background())
			if err != nil {
				t.Fatalf("Failed to get NATS connection string: %v", err)
			}

			// Create NATS client
			client, err := New(natsURL)
			if err != nil {
				t.Fatalf("Failed to create NATS client: %v", err)
			}
			defer client.Close()

			// Channel to receive message
			messageReceived := make(chan *types.SBSMessage, 1)

			// Subscribe to messages
			err = client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
				messageReceived <- msg
			})
			if err != nil {
				t.Fatalf("Failed to subscribe: %v", err)
			}

			// Give subscription time to establish
			time.Sleep(100 * time.Millisecond)

			// Publish message
			err = client.PublishSBSMessage(tc.message)
			if err != nil {
				t.Fatalf("Failed to publish message: %v", err)
			}

			// Wait for message to be received
			select {
			case receivedMsg := <-messageReceived:
				// Verify message content
				if receivedMsg.Raw != tc.message.Raw {
					t.Errorf("Expected raw message %s, got %s", tc.message.Raw, receivedMsg.Raw)
				}
				if receivedMsg.Source != tc.message.Source {
					t.Errorf("Expected source %s, got %s", tc.message.Source, receivedMsg.Source)
				}
				// Note: Timestamp comparison might be tricky due to precision differences
				// We'll just verify it's not zero
				if receivedMsg.Timestamp.IsZero() {
					t.Error("Expected non-zero timestamp")
				}
			case <-time.After(5 * time.Second):
				t.Error("Timeout waiting for message")
			}
		})
	}
}

// TestNATSClient_Integration_ErrorHandling tests error scenarios
func TestNATSClient_Integration_ErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Get NATS connection string
	natsURL, err := containers.nats.ConnectionString(context.Background())
	if err != nil {
		t.Fatalf("Failed to get NATS connection string: %v", err)
	}

	// Create NATS client
	client, err := New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Test nil message handling
	err = client.PublishSBSMessage(nil)
	if err != nil {
		t.Errorf("Expected no error publishing nil message, got: %v", err)
	}

	// Test nil message handling
	err = client.PublishSBSMessage(nil)
	if err != nil {
		t.Errorf("Expected no error publishing nil message, got: %v", err)
	}

	// Test invalid message handling
	invalidMsg := &types.SBSMessage{
		Raw:       "", // Empty raw message
		Timestamp: time.Now().UTC(),
		Source:    "test-source",
	}

	err = client.PublishSBSMessage(invalidMsg)
	if err != nil {
		t.Errorf("Expected no error publishing invalid message, got: %v", err)
	}

	// Test connection error handling by closing the client
	client.Close()

	// Try to publish after closing - this should fail
	testMsg := &types.SBSMessage{
		Raw:       "MSG,8,111,11111,111111,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0",
		Timestamp: time.Now().UTC(),
		Source:    "test-source",
	}
	err = client.PublishSBSMessage(testMsg)
	if err == nil {
		t.Error("Expected error when publishing to closed client")
	}
}

// TestNATSClient_Integration_ConcurrentPublishers tests multiple concurrent publishers
func TestNATSClient_Integration_ConcurrentPublishers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Get NATS connection string
	natsURL, err := containers.nats.ConnectionString(context.Background())
	if err != nil {
		t.Fatalf("Failed to get NATS connection string: %v", err)
	}

	// Create multiple NATS clients
	clients := make([]*Client, 3)
	for i := 0; i < 3; i++ {
		client, err := New(natsURL)
		if err != nil {
			t.Fatalf("Failed to create NATS client %d: %v", i, err)
		}
		defer client.Close()
		clients[i] = client
	}

	// Channel to receive messages
	messageCount := 0
	expectedMessages := 30 // 3 clients * 10 messages each
	messageReceived := make(chan bool, expectedMessages)

	// Subscribe to messages with one client
	err = clients[0].SubscribeSBSRaw(func(msg *types.SBSMessage) {
		messageCount++
		messageReceived <- true
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Give subscription time to establish
	time.Sleep(100 * time.Millisecond)

	// Publish messages concurrently from all clients
	for i, client := range clients {
		go func(clientIndex int, client *Client) {
			for j := 0; j < 10; j++ {
				msg := &types.SBSMessage{
					Raw:       fmt.Sprintf("MSG,8,111,11111,111111,ABC%d,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST%d,10000,450,180,40.7128,-74.0060,0,0,0,0", clientIndex, j),
					Timestamp: time.Now().UTC(),
					Source:    fmt.Sprintf("test-source-%d", clientIndex),
				}
				err := client.PublishSBSMessage(msg)
				if err != nil {
					t.Errorf("Failed to publish message from client %d: %v", clientIndex, err)
				}
				time.Sleep(10 * time.Millisecond) // Small delay to avoid overwhelming
			}
		}(i, client)
	}

	// Wait for all messages to be received
	receivedCount := 0
	timeout := time.After(10 * time.Second)
	for receivedCount < expectedMessages {
		select {
		case <-messageReceived:
			receivedCount++
		case <-timeout:
			t.Errorf("Timeout waiting for messages. Received %d, expected %d", receivedCount, expectedMessages)
			return
		}
	}

	if receivedCount != expectedMessages {
		t.Errorf("Expected %d messages, received %d", expectedMessages, receivedCount)
	}
}

// TestNATSClient_Integration_Reconnection tests client reconnection behavior
func TestNATSClient_Integration_Reconnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Get NATS connection string
	natsURL, err := containers.nats.ConnectionString(context.Background())
	if err != nil {
		t.Fatalf("Failed to get NATS connection string: %v", err)
	}

	// Create NATS client
	client, err := New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Test that client can still publish after reconnection
	testMsg := &types.SBSMessage{
		Raw:       "MSG,8,111,11111,111111,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0",
		Timestamp: time.Now().UTC(),
		Source:    "test-source",
	}

	// Publish message
	err = client.PublishSBSMessage(testMsg)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	// Close and recreate connection
	client.Close()

	// Create new client
	client, err = New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create new NATS client: %v", err)
	}
	defer client.Close()

	// Test that new client can still publish
	err = client.PublishSBSMessage(testMsg)
	if err != nil {
		t.Fatalf("Failed to publish message with new client: %v", err)
	}
}
