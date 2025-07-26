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

	"github.com/saviobatista/sbs-logger/internal/types"
)

// Mock NATS client for unit testing
type mockNATSClient struct {
	publishedMessages []*types.SBSMessage
	publishError      error
	closed            bool
	mu                sync.RWMutex
}

func (m *mockNATSClient) PublishSBSMessage(msg *types.SBSMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishError != nil {
		return m.publishError
	}
	m.publishedMessages = append(m.publishedMessages, msg)
	return nil
}

func (m *mockNATSClient) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

func (m *mockNATSClient) SubscribeSBSRaw(handler func(*types.SBSMessage)) error {
	return nil // Not used in ingestor
}

// GetPublishedMessagesCount returns the number of published messages
func (m *mockNATSClient) GetPublishedMessagesCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.publishedMessages)
}

// GetPublishedMessages returns a copy of the published messages
func (m *mockNATSClient) GetPublishedMessages() []*types.SBSMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messages := make([]*types.SBSMessage, len(m.publishedMessages))
	copy(messages, m.publishedMessages)
	return messages
}

// IsClosed returns whether the client is closed
func (m *mockNATSClient) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}

// TestParseEnvironment tests environment variable parsing
func TestParseEnvironment(t *testing.T) {
	// Save original environment
	originalSources := os.Getenv("SOURCES")
	originalNATSURL := os.Getenv("NATS_URL")
	defer func() {
		os.Setenv("SOURCES", originalSources)
		os.Setenv("NATS_URL", originalNATSURL)
	}()

	tests := []struct {
		name            string
		sources         string
		natsURL         string
		expectError     bool
		expectedSources []string
		expectedNATSURL string
	}{
		{
			name:        "missing SOURCES",
			sources:     "",
			natsURL:     "",
			expectError: true,
		},
		{
			name:            "single source with default NATS",
			sources:         "localhost:30003",
			natsURL:         "",
			expectError:     false,
			expectedSources: []string{"localhost:30003"},
			expectedNATSURL: "nats://nats:4222",
		},
		{
			name:            "multiple sources with custom NATS",
			sources:         "localhost:30003, localhost:30004, localhost:30005",
			natsURL:         "nats://custom:4222",
			expectError:     false,
			expectedSources: []string{"localhost:30003", "localhost:30004", "localhost:30005"},
			expectedNATSURL: "nats://custom:4222",
		},
		{
			name:            "sources with spaces",
			sources:         " localhost:30003 , localhost:30004 ",
			natsURL:         "",
			expectError:     false,
			expectedSources: []string{"localhost:30003", "localhost:30004"},
			expectedNATSURL: "nats://nats:4222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			os.Setenv("SOURCES", tt.sources)
			os.Setenv("NATS_URL", tt.natsURL)

			sources, natsURL, err := parseEnvironment()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
				return
			}

			if len(sources) != len(tt.expectedSources) {
				t.Errorf("Expected %d sources, got %d", len(tt.expectedSources), len(sources))
				return
			}

			for i, expected := range tt.expectedSources {
				if sources[i] != expected {
					t.Errorf("Expected source[%d]=%q, got %q", i, expected, sources[i])
				}
			}

			if natsURL != tt.expectedNATSURL {
				t.Errorf("Expected NATS URL %q, got %q", tt.expectedNATSURL, natsURL)
			}
		})
	}
}

// TestConnectWithRetry tests the connection retry logic
func TestConnectWithRetry(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		setupServer func() (net.Listener, error)
		expectError bool
		maxDuration time.Duration
	}{
		{
			name:   "successful connection",
			source: "localhost:0", // Will be replaced
			setupServer: func() (net.Listener, error) {
				return net.Listen("tcp", "localhost:0")
			},
			expectError: false,
			maxDuration: 2 * time.Second,
		},
		{
			name:        "connection failure",
			source:      "localhost:99999", // Invalid port
			setupServer: nil,               // No server setup needed for failure case
			expectError: true,
			maxDuration: 100 * time.Millisecond, // Short timeout for failure case
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actualSource string

			if tt.setupServer != nil {
				listener, err := tt.setupServer()
				if err != nil {
					if !tt.expectError {
						t.Fatalf("Failed to create mock server: %v", err)
					}
					actualSource = tt.source
				} else {
					defer func() {
						if err := listener.Close(); err != nil {
							t.Errorf("Failed to close listener: %v", err)
						}
					}()
					actualSource = listener.Addr().String()
				}
			} else {
				actualSource = tt.source
			}

			// Test with a timeout to prevent infinite retry
			done := make(chan error, 1)
			go func() {
				conn, err := connectWithRetry(actualSource)
				if conn != nil {
					_ = conn.Close()
				}
				done <- err
			}()

			select {
			case err := <-done:
				if tt.expectError && err == nil {
					t.Error("Expected error, got nil")
				}
				if !tt.expectError && err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			case <-time.After(tt.maxDuration):
				if !tt.expectError {
					t.Error("Connection attempt timed out unexpectedly")
				}
				// For error cases, timeout is expected due to retry loop
			}
		})
	}
}

// TestConnectAndIngest tests the connectAndIngest function with mock server
func TestConnectAndIngest(t *testing.T) {
	tests := []struct {
		name           string
		setupServer    func() (net.Listener, error)
		setupMockNATS  func() *mockNATSClient
		expectError    bool
		expectMessages int
		maxDuration    time.Duration
	}{
		{
			name: "successful connect and ingest",
			setupServer: func() (net.Listener, error) {
				return createMockTCPServer([]string{
					"MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n",
				})
			},
			setupMockNATS: func() *mockNATSClient {
				return &mockNATSClient{}
			},
			expectError:    false,
			expectMessages: 1,
			maxDuration:    2 * time.Second,
		},
		{
			name: "connection failure",
			setupServer: func() (net.Listener, error) {
				return nil, fmt.Errorf("server error")
			},
			setupMockNATS: func() *mockNATSClient {
				return &mockNATSClient{}
			},
			expectError:    true,
			expectMessages: 0,
			maxDuration:    1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actualSource string

			if tt.expectError {
				actualSource = "invalid:99999"
			} else {
				listener, err := tt.setupServer()
				if err != nil {
					t.Fatalf("Failed to create mock server: %v", err)
				}
				defer func() {
					if err := listener.Close(); err != nil {
						t.Errorf("Failed to close listener: %v", err)
					}
				}()
				actualSource = listener.Addr().String()
			}

			mockClient := tt.setupMockNATS()
			ctx, cancel := context.WithTimeout(context.Background(), tt.maxDuration)
			defer cancel()

			err := connectAndIngest(ctx, actualSource, mockClient)

			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectError && err != nil {
				// EOF is expected when the mock server closes the connection
				if !strings.Contains(err.Error(), "EOF") {
					t.Errorf("Expected no error or EOF, got: %v", err)
				}
			}

			if mockClient.GetPublishedMessagesCount() != tt.expectMessages {
				t.Errorf("Expected %d messages, got %d", tt.expectMessages, mockClient.GetPublishedMessagesCount())
			}
		})
	}
}

// TestNATSClientInterface tests that our mock implements the expected interface
func TestNATSClientInterface(t *testing.T) {
	mock := &mockNATSClient{}

	// Test that our mock can be used as a NATSClient
	var client NATSClient = mock

	// Test the interface methods
	msg := &types.SBSMessage{
		Raw:       "test message",
		Timestamp: time.Now(),
		Source:    "test",
	}

	err := client.PublishSBSMessage(msg)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	client.Close()

	// Add a small delay to ensure the Close() method has completed
	time.Sleep(10 * time.Millisecond)

	if !mock.IsClosed() {
		t.Error("Expected mock to be marked as closed")
	}
}

// Helper functions

// parseEnvironment extracts the core environment parsing logic for testing
func parseEnvironment() ([]string, string, error) {
	sources := os.Getenv("SOURCES")
	if sources == "" {
		return nil, "", fmt.Errorf("SOURCES environment variable is required")
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats:4222" // Default to Docker service name
	}

	// Parse sources
	sourceList := strings.Split(sources, ",")
	for i, source := range sourceList {
		sourceList[i] = strings.TrimSpace(source)
	}

	return sourceList, natsURL, nil
}

// createMockTCPServer creates a mock TCP server that sends predefined messages
func createMockTCPServer(messages []string) (net.Listener, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	go func() {
		defer listener.Close()

		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}

			go func(conn net.Conn) {
				defer conn.Close()

				// Send messages with small delay
				for _, msg := range messages {
					time.Sleep(10 * time.Millisecond)
					_, err := conn.Write([]byte(msg))
					if err != nil {
						return
					}
				}

				// Keep connection open briefly then close
				time.Sleep(100 * time.Millisecond)
			}(conn)
		}
	}()

	return listener, nil
}
