package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/nats"
	"github.com/saviobatista/sbs-logger/internal/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testContainers holds the test containers for integration tests
type testContainers struct {
	nats testcontainers.Container
}

// setupTestContainers creates and starts test containers
func setupTestContainers(t *testing.T) (*testContainers, error) {
	ctx := context.Background()

	// Create NATS container with JetStream enabled
	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "nats:2.9-alpine",
			ExposedPorts: []string{"4222/tcp"},
			Cmd:          []string{"-js"},
			WaitingFor:   wait.ForLog("Server is ready"),
		},
		Started: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start NATS container: %w", err)
	}

	// Get NATS port
	natsPort, err := natsContainer.MappedPort(ctx, "4222")
	if err != nil {
		return nil, fmt.Errorf("failed to get NATS port: %w", err)
	}

	containers := &testContainers{
		nats: natsContainer,
	}

	// Set environment variables for the test
	os.Setenv("NATS_URL", fmt.Sprintf("nats://localhost:%s", natsPort.Port()))
	os.Setenv("OUTPUT_DIR", t.TempDir())

	return containers, nil
}

// TestLoggerMain_Integration tests the main function with real NATS server
func TestLoggerMain_Integration(t *testing.T) {
	// Set up test containers
	containers, err := setupTestContainers(t)
	if err != nil {
		t.Fatalf("Failed to set up test containers: %v", err)
	}
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Get NATS URL from environment
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		t.Fatal("NATS_URL environment variable not set")
	}

	// Create NATS client with retry logic
	var client *nats.Client
	for i := 0; i < 5; i++ {
		client, err = nats.New(natsURL)
		if err == nil {
			break
		}
		t.Logf("Attempt %d: Failed to create NATS client: %v", i+1, err)
		time.Sleep(time.Second)
	}
	if err != nil {
		t.Fatalf("Failed to create NATS client after retries: %v", err)
	}
	defer client.Close()

	// Test environment parsing
	outputDir, parsedNATSURL := parseEnvironment()
	if outputDir == "" {
		t.Error("Expected output directory to be set")
	}
	if parsedNATSURL == "" {
		t.Error("Expected NATS URL to be set")
	}

	// Test logger creation
	logger := NewLogger(outputDir)
	if logger == nil {
		t.Fatal("Expected logger to be created")
	}

	// Test logger start
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go logger.Start(ctx)

	// Give logger time to initialize
	time.Sleep(100 * time.Millisecond)

	// Test message writing
	testMessage := &types.SBSMessage{
		Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n",
		Timestamp: time.Now(),
		Source:    "test-source",
	}

	err = logger.WriteMessage(testMessage)
	if err != nil {
		t.Errorf("Failed to write message: %v", err)
	}

	// Verify message was written to file
	expectedDate := time.Now().UTC().Format("2006-01-02")
	expectedPath := filepath.Join(outputDir, fmt.Sprintf("sbs_%s.log", expectedDate))

	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Errorf("Failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "ABC123") {
		t.Error("Expected message content to be written to file")
	}

	// Test NATS subscription
	messageReceived := make(chan *types.SBSMessage, 1)
	err = client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		messageReceived <- msg
	})
	if err != nil {
		t.Fatalf("Failed to subscribe to SBS messages: %v", err)
	}

	// Publish a message to NATS
	err = client.PublishSBSMessage(testMessage)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	// Wait for message to be received
	select {
	case receivedMsg := <-messageReceived:
		if receivedMsg.Raw != testMessage.Raw {
			t.Errorf("Expected message %q, got %q", testMessage.Raw, receivedMsg.Raw)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

// TestLoggerMain_EnvironmentVariables tests main function with different environment variables
func TestLoggerMain_EnvironmentVariables(t *testing.T) {
	// Set up test containers
	containers, err := setupTestContainers(t)
	if err != nil {
		t.Fatalf("Failed to set up test containers: %v", err)
	}
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	tests := []struct {
		name        string
		outputDir   string
		natsURL     string
		expectError bool
	}{
		{
			name:        "default environment variables",
			outputDir:   "",
			natsURL:     "",
			expectError: false,
		},
		{
			name:        "custom environment variables",
			outputDir:   "/tmp/custom-logs",
			natsURL:     "nats://custom:4222",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			if tt.outputDir != "" {
				os.Setenv("OUTPUT_DIR", tt.outputDir)
			} else {
				os.Unsetenv("OUTPUT_DIR")
			}
			if tt.natsURL != "" {
				os.Setenv("NATS_URL", tt.natsURL)
			} else {
				os.Unsetenv("NATS_URL")
			}

			// Test environment parsing
			outputDir, natsURL := parseEnvironment()

			if tt.outputDir == "" {
				// Should use default
				if outputDir != "./logs" {
					t.Errorf("Expected default output dir './logs', got %q", outputDir)
				}
			} else {
				// Should use custom value
				if outputDir != tt.outputDir {
					t.Errorf("Expected output dir %q, got %q", tt.outputDir, outputDir)
				}
			}

			if tt.natsURL == "" {
				// Should use default
				if natsURL != "nats://nats:4222" {
					t.Errorf("Expected default NATS URL 'nats://nats:4222', got %q", natsURL)
				}
			} else {
				// Should use custom value
				if natsURL != tt.natsURL {
					t.Errorf("Expected NATS URL %q, got %q", tt.natsURL, natsURL)
				}
			}
		})
	}
}

// TestLoggerMain_OutputDirectoryCreation tests main function output directory creation
func TestLoggerMain_OutputDirectoryCreation(t *testing.T) {
	// Set up test containers
	containers, err := setupTestContainers(t)
	if err != nil {
		t.Fatalf("Failed to set up test containers: %v", err)
	}
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	tests := []struct {
		name        string
		outputDir   string
		expectError bool
	}{
		{
			name:        "valid output directory",
			outputDir:   t.TempDir(),
			expectError: false,
		},
		{
			name:        "default output directory",
			outputDir:   "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set output directory
			if tt.outputDir != "" {
				os.Setenv("OUTPUT_DIR", tt.outputDir)
			} else {
				os.Unsetenv("OUTPUT_DIR")
			}

			// Test directory creation logic
			outputDir := os.Getenv("OUTPUT_DIR")
			if outputDir == "" {
				outputDir = "./logs" // Default output directory
			}

			// Test directory creation
			err := os.MkdirAll(outputDir, 0o750)
			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// Verify directory exists
			if !tt.expectError {
				if _, err := os.Stat(outputDir); os.IsNotExist(err) {
					t.Errorf("Expected directory %s to exist", outputDir)
				}
			}
		})
	}
}

// TestLoggerMain_NATSConnection tests main function NATS connection logic
func TestLoggerMain_NATSConnection(t *testing.T) {
	// Set up test containers
	containers, err := setupTestContainers(t)
	if err != nil {
		t.Fatalf("Failed to set up test containers: %v", err)
	}
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Get NATS URL from environment
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		t.Fatal("NATS_URL environment variable not set")
	}

	// Test NATS client creation (main function logic)
	client, err := nats.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Test message publishing (main function logic)
	testMessage := &types.SBSMessage{
		Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n",
		Timestamp: time.Now(),
		Source:    "test-source",
	}

	err = client.PublishSBSMessage(testMessage)
	if err != nil {
		t.Errorf("Failed to publish message: %v", err)
	}

	// Test message subscription (main function logic)
	messageReceived := make(chan *types.SBSMessage, 1)
	err = client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		messageReceived <- msg
	})
	if err != nil {
		t.Fatalf("Failed to subscribe to SBS messages: %v", err)
	}

	// Publish another message to trigger subscription
	err = client.PublishSBSMessage(testMessage)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	// Wait for message to be received
	select {
	case receivedMsg := <-messageReceived:
		if receivedMsg.Raw != testMessage.Raw {
			t.Errorf("Expected message %q, got %q", testMessage.Raw, receivedMsg.Raw)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

// TestLoggerMain_MessageWriting tests main function message writing logic
func TestLoggerMain_MessageWriting(t *testing.T) {
	// Set up test containers
	containers, err := setupTestContainers(t)
	if err != nil {
		t.Fatalf("Failed to set up test containers: %v", err)
	}
	defer func() {
		if err := containers.nats.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Set up output directory
	outputDir := t.TempDir()
	os.Setenv("OUTPUT_DIR", outputDir)

	// Create logger (main function logic)
	logger := NewLogger(outputDir)
	if logger == nil {
		t.Fatal("Expected logger to be created")
	}

	// Start logger (main function logic)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go logger.Start(ctx)

	// Give logger time to initialize
	time.Sleep(100 * time.Millisecond)

	// Test message writing (main function logic)
	testMessage := &types.SBSMessage{
		Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n",
		Timestamp: time.Now(),
		Source:    "test-source",
	}

	err = logger.WriteMessage(testMessage)
	if err != nil {
		t.Errorf("Failed to write message: %v", err)
	}

	// Verify message was written to file (main function logic)
	expectedDate := time.Now().UTC().Format("2006-01-02")
	expectedPath := filepath.Join(outputDir, fmt.Sprintf("sbs_%s.log", expectedDate))

	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Errorf("Failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "ABC123") {
		t.Error("Expected message content to be written to file")
	}

	// Test multiple messages
	for i := 0; i < 5; i++ {
		message := &types.SBSMessage{
			Raw:       fmt.Sprintf("MSG,3,1,1,DEF%d,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST%d,10000,450,180,40.7128,-74.0060,0,0,0,0\n", i, i),
			Timestamp: time.Now(),
			Source:    fmt.Sprintf("test-source-%d", i),
		}

		err := logger.WriteMessage(message)
		if err != nil {
			t.Errorf("Failed to write message %d: %v", i, err)
		}
	}

	// Verify all messages were written
	content, err = os.ReadFile(expectedPath)
	if err != nil {
		t.Errorf("Failed to read log file: %v", err)
	}

	contentStr := string(content)
	expectedMessages := 6 // 1 initial + 5 additional
	actualMessages := strings.Count(contentStr, "MSG,")

	if actualMessages != expectedMessages {
		t.Errorf("Expected %d messages, found %d", expectedMessages, actualMessages)
	}
}

// TestLoggerMain_ErrorHandling tests main function error handling
func TestLoggerMain_ErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		setupEnv    func()
		expectError bool
	}{
		{
			name: "invalid NATS URL",
			setupEnv: func() {
				os.Setenv("NATS_URL", "nats://invalid:4222")
				os.Setenv("OUTPUT_DIR", t.TempDir())
			},
			expectError: true,
		},
		{
			name: "invalid output directory",
			setupEnv: func() {
				os.Setenv("NATS_URL", "nats://localhost:4222")
				os.Setenv("OUTPUT_DIR", "/invalid/path/that/doesnt/exist")
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			tt.setupEnv()

			// Test environment parsing
			outputDir, natsURL := parseEnvironment()

			if tt.name == "invalid NATS URL" {
				// Should still parse the URL even if invalid
				if natsURL == "" {
					t.Error("Expected NATS URL to be parsed")
				}
			}

			if tt.name == "invalid output directory" {
				// Should still parse the directory even if invalid
				if outputDir == "" {
					t.Error("Expected output directory to be parsed")
				}
			}
		})
	}
}
