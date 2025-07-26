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
	natsContainer, err := natscontainer.RunContainer(ctx,
		testcontainers.WithImage("nats:2.9-alpine"),
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
		containers.nats.Terminate(context.Background())
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
		containers.nats.Terminate(context.Background())
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
		"MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n",
		"MSG,1,1,1,DEF456,1,2021-01-01,00:00:01.000,2021-01-01,00:00:01.000,TEST456,11000,500,200,41.7128,-75.0060,0,0,0,0\n",
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

	// Note: In a real integration test, we would verify that messages
	// were actually published to NATS by subscribing to the subject
	// For now, we just verify that the ingestion process completed
	// without errors (the context cancellation is expected)
}

// TestConnectAndIngestIntegration tests the full connect and ingest flow
func TestConnectAndIngestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		containers.nats.Terminate(context.Background())
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
		"MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n",
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
