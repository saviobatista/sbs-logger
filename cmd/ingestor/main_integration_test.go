package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/nats"
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

// TestNATSIntegration tests integration with real NATS server
func TestNATSIntegration(t *testing.T) {
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
	client, err := nats.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Test that we can publish a message
	msg := &types.SBSMessage{
		Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0",
		Timestamp: time.Now().UTC(),
		Source:    "test-source",
	}

	err = client.PublishSBSMessage(msg)
	if err != nil {
		t.Errorf("Failed to publish message: %v", err)
	}
}

// TestIngestSourceIntegration tests the full ingestion pipeline with real NATS
func TestIngestSourceIntegration(t *testing.T) {
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
	client, err := nats.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Create mock TCP server
	listener, err := createMockTCPServer([]string{
		"MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\r\n",
		"MSG,1,1,1,DEF456,1,2021-01-01,00:00:01.000,2021-01-01,00:00:01.000,TEST456,11000,500,200,41.7128,-75.0060,0,0,0,0\r\n",
	})
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer listener.Close()

	source := listener.Addr().String()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start ingestion in a goroutine
	go ingestSource(ctx, source, client)

	// Wait for context to complete
	<-ctx.Done()

	// Verify that messages were actually published to NATS by subscribing to the subject
	messageReceived := make(chan *types.SBSMessage, 2)
	err = client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		messageReceived <- msg
	})
	if err != nil {
		t.Fatalf("Failed to subscribe to verify messages: %v", err)
	}

	// Wait for messages to be received
	receivedCount := 0
	timeout := time.After(5 * time.Second)
	for receivedCount < 2 {
		select {
		case msg := <-messageReceived:
			receivedCount++
			t.Logf("Received message %d: %s", receivedCount, msg.Raw)
		case <-timeout:
			t.Errorf("Timeout waiting for messages. Received %d, expected 2", receivedCount)
			return
		}
	}

	if receivedCount != 2 {
		t.Errorf("Expected 2 messages, received %d", receivedCount)
	}
}

// TestConnectAndIngestIntegration tests the full connect and ingest flow
func TestConnectAndIngestIntegration(t *testing.T) {
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
	client, err := nats.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Create mock TCP server
	listener, err := createMockTCPServer([]string{
		"MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\r\n",
	})
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer listener.Close()

	source := listener.Addr().String()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Test the full connect and ingest flow
	err = connectAndIngest(ctx, source, client)
	if err != nil {
		// EOF is expected when the mock server closes the connection
		if !strings.Contains(fmt.Sprintf("%v", err), "EOF") {
			t.Errorf("Expected no error or EOF, got: %v", err)
		}
	}
}

// TestConnectWithRetryIntegration tests connection retry with real network
func TestConnectWithRetryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test successful connection
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	source := listener.Addr().String()

	// Test with a timeout to prevent infinite retry
	done := make(chan error, 1)
	go func() {
		conn, err := connectWithRetry(source)
		if conn != nil {
			_ = conn.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Connection attempt timed out unexpectedly")
	}
}

// TestNATSMessageFlow_Integration tests the complete message flow from ingestion to NATS
func TestNATSMessageFlow_Integration(t *testing.T) {
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
	client, err := nats.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Create mock TCP server with various message types
	listener, err := createMockTCPServer([]string{
		"MSG,8,111,11111,111111,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\r\n",
		"MSG,4,111,11111,111111,DEF456,1,2021-01-01,00:00:01.000,2021-01-01,00:00:01.000,TEST456,11000,500,200,41.7128,-75.0060,0,0,0,0\r\n",
		"MSG,1,111,11111,111111,GHI789,1,2021-01-01,00:00:02.000,2021-01-01,00:00:02.000,TEST789,12000,550,220,42.7128,-76.0060,0,0,0,0\r\n",
		"MSG,5,111,11111,111111,JKL012,1,2021-01-01,00:00:03.000,2021-01-01,00:00:03.000,TEST012,13000,600,240,43.7128,-77.0060,0,0,0,0\r\n",
	})
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer listener.Close()

	source := listener.Addr().String()

	// Channel to receive messages
	messageReceived := make(chan *types.SBSMessage, 4)
	allMessagesReceived := make(chan bool, 1)

	// Subscribe to SBS messages
	err = client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		messageReceived <- msg
	})
	if err != nil {
		t.Fatalf("Failed to subscribe to SBS messages: %v", err)
	}

	// Give subscription time to establish
	time.Sleep(100 * time.Millisecond)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start ingestion in a goroutine
	go ingestSource(ctx, source, client)

	// Collect received messages
	received := make([]*types.SBSMessage, 0, 4)
	go func() {
		for i := 0; i < 4; i++ {
			select {
			case msg := <-messageReceived:
				received = append(received, msg)
				t.Logf("Received message %d: %s", i+1, msg.Raw)
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
		if len(received) != 4 {
			t.Errorf("Expected 4 messages, got %d", len(received))
		}

		// Verify we received the expected number of messages
		if len(received) != 4 {
			t.Errorf("Expected 4 messages, got %d", len(received))
		}

		// Verify message contents by checking key parts
		expectedPatterns := []string{
			"ABC123", "TEST123", "10000", "450", "180", "40.7128", "-74.0060",
			"DEF456", "TEST456", "11000", "500", "200", "41.7128", "-75.0060",
			"GHI789", "TEST789", "12000", "550", "220", "42.7128", "-76.0060",
			"JKL012", "TEST012", "13000", "600", "240", "43.7128", "-77.0060",
		}

		// Check that all expected patterns are found in the received messages
		foundPatterns := 0
		for _, pattern := range expectedPatterns {
			for _, msg := range received {
				if strings.Contains(msg.Raw, pattern) {
					foundPatterns++
					break
				}
			}
		}

		if foundPatterns != len(expectedPatterns) {
			t.Errorf("Expected to find %d patterns, found %d", len(expectedPatterns), foundPatterns)
		}

		// Verify all messages have proper timestamps and sources
		for _, msg := range received {
			if msg.Timestamp.IsZero() {
				t.Error("Message timestamp should not be zero")
			}
			if msg.Source == "" {
				t.Error("Message source should not be empty")
			}
		}

		t.Logf("✓ All %d messages received and verified", len(received))

		// Verify all messages have proper timestamps and sources
		for _, msg := range received {
			if msg.Timestamp.IsZero() {
				t.Error("Message timestamp should not be zero")
			}
			if msg.Source == "" {
				t.Error("Message source should not be empty")
			}
		}
	case <-time.After(10 * time.Second):
		t.Error("Timeout waiting for messages")
	}
}
