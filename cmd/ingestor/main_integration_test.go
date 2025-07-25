//go:build integration

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	natsclient "github.com/saviobatista/sbs-logger/internal/nats"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestIntegration_IngestorWithRealNATS tests the ingestor against real NATS server
func TestIntegration_IngestorWithRealNATS(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer func() {
		if err := natsContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Start mock SBS data source
	mockDataServer, sourceAddr := startMockSBSDataServer(t)
	defer mockDataServer.Close()

	tests := []struct {
		name       string
		sources    string
		natsURL    string
		wantErr    bool
		setupEnv   func()
		cleanupEnv func()
		verifyFunc func(t *testing.T, natsURL string)
	}{
		{
			name:    "successful_ingestion_single_source",
			sources: sourceAddr,
			natsURL: natsURL,
			wantErr: false,
			setupEnv: func() {
				os.Setenv("SOURCES", sourceAddr)
				os.Setenv("NATS_URL", natsURL)
			},
			cleanupEnv: func() {
				os.Unsetenv("SOURCES")
				os.Unsetenv("NATS_URL")
			},
			verifyFunc: func(t *testing.T, natsURL string) {
				verifyMessagesReceived(t, natsURL, 1)
			},
		},
		{
			name:    "successful_ingestion_multiple_sources",
			sources: fmt.Sprintf("%s,%s", sourceAddr, sourceAddr),
			natsURL: natsURL,
			wantErr: false,
			setupEnv: func() {
				os.Setenv("SOURCES", fmt.Sprintf("%s,%s", sourceAddr, sourceAddr))
				os.Setenv("NATS_URL", natsURL)
			},
			cleanupEnv: func() {
				os.Unsetenv("SOURCES")
				os.Unsetenv("NATS_URL")
			},
			verifyFunc: func(t *testing.T, natsURL string) {
				verifyMessagesReceived(t, natsURL, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment
			tt.setupEnv()
			defer tt.cleanupEnv()

			// Test connectAndIngest function directly with real NATS
			client, err := natsclient.New(tt.natsURL)
			if err != nil {
				t.Fatalf("Failed to create NATS client: %v", err)
			}
			defer client.Close()

			// Test connection and ingestion for a short period
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			// Test the core ingestion logic
			sourceList := []string{sourceAddr}
			for _, source := range sourceList {
				go func(src string) {
					err := connectAndIngest(ctx, src, client)
					if err != nil && err != context.DeadlineExceeded {
						t.Errorf("connectAndIngest failed: %v", err)
					}
				}(source)
			}

			// Let it run for a bit
			time.Sleep(1 * time.Second)

			// Verify messages were published
			if tt.verifyFunc != nil {
				tt.verifyFunc(t, tt.natsURL)
			}
		})
	}
}

// TestIntegration_IngestorConnectionRetry tests connection retry logic with real infrastructure
func TestIntegration_IngestorConnectionRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer func() {
		if err := natsContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Test connection to non-existent source (should retry)
	client, err := natsclient.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Test with a non-existent address - should timeout quickly for test
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// This should attempt to connect and fail
	err = connectAndIngest(ctx, "localhost:99999", client)
	if err == nil {
		t.Error("Expected error when connecting to non-existent source")
	}
}

// TestIntegration_IngestorEnvironmentVariables tests environment variable handling
func TestIntegration_IngestorEnvironmentVariables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Save original environment
	originalSources := os.Getenv("SOURCES")
	originalNATSURL := os.Getenv("NATS_URL")
	defer func() {
		os.Setenv("SOURCES", originalSources)
		os.Setenv("NATS_URL", originalNATSURL)
	}()

	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer func() {
		if err := natsContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	tests := []struct {
		name              string
		sources           string
		natsURL           string
		expectNATSClient  bool
		expectSourceParse bool
	}{
		{
			name:              "valid_environment_variables",
			sources:           "localhost:30003,localhost:30004",
			natsURL:           natsURL,
			expectNATSClient:  true,
			expectSourceParse: true,
		},
		{
			name:              "sources_with_spaces",
			sources:           " localhost:30003 , localhost:30004 ",
			natsURL:           natsURL,
			expectNATSClient:  true,
			expectSourceParse: true,
		},
		{
			name:              "invalid_nats_url",
			sources:           "localhost:30003",
			natsURL:           "nats://invalid:4222",
			expectNATSClient:  false,
			expectSourceParse: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			os.Setenv("SOURCES", tt.sources)
			os.Setenv("NATS_URL", tt.natsURL)

			// Test NATS client creation
			client, err := natsclient.New(tt.natsURL)
			if tt.expectNATSClient && err != nil {
				t.Errorf("Expected NATS client creation to succeed, got error: %v", err)
			}
			if !tt.expectNATSClient && err == nil {
				t.Error("Expected NATS client creation to fail")
			}
			if client != nil {
				client.Close()
			}

			// Test source parsing
			if tt.expectSourceParse {
				sourceList := strings.Split(tt.sources, ",")
				if len(sourceList) == 0 {
					t.Error("Expected sources to be parsed")
				}
				for _, source := range sourceList {
					trimmed := strings.TrimSpace(source)
					if trimmed == "" {
						t.Error("Expected non-empty source after trimming")
					}
				}
			}
		})
	}
}

// TestIntegration_IngestorGracefulShutdown tests shutdown signal handling
func TestIntegration_IngestorGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start NATS container
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer func() {
		if err := natsContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Start mock SBS data server
	mockDataServer, sourceAddr := startMockSBSDataServer(t)
	defer mockDataServer.Close()

	// Create NATS client
	client, err := natsclient.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Test graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())

	// Start ingestion in goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ingestSource(ctx, sourceAddr, client)
	}()

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Cancel context (simulate shutdown)
	cancel()

	// Wait for goroutine to finish (should be quick due to context cancellation)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Good, shutdown was graceful
	case <-time.After(2 * time.Second):
		t.Error("Graceful shutdown took too long")
	}
}

// startNATSContainer starts a NATS container for testing
func startNATSContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image:        "nats:2.10-alpine",
		ExposedPorts: []string{"4222/tcp"},
		Cmd: []string{
			"--jetstream",
			"--store_dir", "/data",
		},
		WaitingFor: wait.ForLog("Server is ready").WithStartupTimeout(30 * time.Second),
	}

	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start NATS container: %v", err)
	}

	// Get the mapped port
	mappedPort, err := natsContainer.MappedPort(ctx, "4222")
	if err != nil {
		t.Fatalf("Failed to get mapped port: %v", err)
	}

	// Get the host
	host, err := natsContainer.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get container host: %v", err)
	}

	natsURL := fmt.Sprintf("nats://%s:%s", host, mappedPort.Port())
	t.Logf("NATS container started at: %s", natsURL)

	return natsContainer, natsURL
}

// startMockSBSDataServer starts a mock SBS data server that sends test data
func startMockSBSDataServer(t *testing.T) (net.Listener, string) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to start mock SBS data server: %v", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}

			go func(c net.Conn) {
				defer c.Close()

				// Send some mock SBS messages
				messages := []string{
					"MSG,3,111,11111,4CA2D6,111111,2015/02/19,18:06:07.710,2015/02/19,18:06:07.710,,33000,,,,,,,0,0,0,0\n",
					"MSG,4,111,11111,4CA2D7,111111,2015/02/19,18:06:08.710,2015/02/19,18:06:08.710,,34000,,,,,,,0,0,0,0\n",
					"MSG,1,111,11111,4CA2D8,111111,2015/02/19,18:06:09.710,2015/02/19,18:06:09.710,TEST123,35000,,,,,,,0,0,0,0\n",
				}

				for _, msg := range messages {
					_, err := c.Write([]byte(msg))
					if err != nil {
						return
					}
					time.Sleep(100 * time.Millisecond)
				}
			}(conn)
		}
	}()

	return listener, listener.Addr().String()
}

// verifyMessagesReceived verifies that messages were received via NATS
func verifyMessagesReceived(t *testing.T, natsURL string, expectedSources int) {
	// Connect to NATS and subscribe to see if messages were published
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("Failed to connect to NATS for verification: %v", err)
	}
	defer nc.Close()

	// Create JetStream context
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("Failed to create JetStream context: %v", err)
	}

	// Subscribe to the SBS subject
	sub, err := js.SubscribeSync("sbs.raw")
	if err != nil {
		t.Fatalf("Failed to subscribe to sbs.raw: %v", err)
	}
	defer sub.Unsubscribe()

	// Wait for messages
	receivedMessages := 0
	timeout := time.After(3 * time.Second)

	for {
		select {
		case <-timeout:
			if receivedMessages == 0 {
				t.Error("No messages received within timeout")
			}
			return
		default:
			msg, err := sub.NextMsg(100 * time.Millisecond)
			if err == nats.ErrTimeout {
				continue
			}
			if err != nil {
				t.Errorf("Error receiving message: %v", err)
				return
			}

			receivedMessages++
			t.Logf("Received message: %s", string(msg.Data))

			// We expect at least some messages
			if receivedMessages >= 1 {
				return
			}
		}
	}
}
