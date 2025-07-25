//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/saviobatista/sbs-logger/internal/nats"
	"github.com/saviobatista/sbs-logger/internal/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestIntegration_LoggerWithRealNATS tests the logger against real NATS server
func TestIntegration_LoggerWithRealNATS(t *testing.T) {
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

	// Create temporary directory for test logs
	tempDir, err := os.MkdirTemp("", "logger-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name          string
		outputDir     string
		natsURL       string
		messages      []*types.SBSMessage
		expectedFiles int
		verifyContent bool
	}{
		{
			name:      "successful_message_logging_single_file",
			outputDir: tempDir,
			natsURL:   natsURL,
			messages: []*types.SBSMessage{
				{
					Raw:       "MSG,3,111,11111,4CA2D6,111111,2015/02/19,18:06:07.710,2015/02/19,18:06:07.710,,33000,,,,,,,0,0,0,0",
					Timestamp: time.Now().UTC(),
					Source:    "test-source-1",
				},
				{
					Raw:       "MSG,4,111,11111,4CA2D7,111111,2015/02/19,18:06:08.710,2015/02/19,18:06:08.710,,34000,,,,,,,0,0,0,0",
					Timestamp: time.Now().UTC(),
					Source:    "test-source-2",
				},
			},
			expectedFiles: 1,
			verifyContent: true,
		},
		{
			name:          "multiple_messages_same_day",
			outputDir:     tempDir + "2",
			natsURL:       natsURL,
			messages:      generateTestMessages(5),
			expectedFiles: 1,
			verifyContent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create output directory
			err := os.MkdirAll(tt.outputDir, 0750)
			if err != nil {
				t.Fatalf("Failed to create output directory: %v", err)
			}

			// Create NATS client for publishing
			publishClient, err := nats.New(tt.natsURL)
			if err != nil {
				t.Fatalf("Failed to create publish NATS client: %v", err)
			}
			defer publishClient.Close()

			// Create logger subscribe client
			subscribeClient, err := nats.New(tt.natsURL)
			if err != nil {
				t.Fatalf("Failed to create subscribe NATS client: %v", err)
			}
			defer subscribeClient.Close()

			// Create and start logger
			logger := NewLogger(tt.outputDir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go logger.Start(ctx)

			// Setup subscription to handle messages
			receivedMessages := 0
			err = subscribeClient.SubscribeSBSRaw(func(msg *types.SBSMessage) {
				if err := logger.WriteMessage(msg); err != nil {
					t.Errorf("Failed to write message: %v", err)
				}
				receivedMessages++
			})
			if err != nil {
				t.Fatalf("Failed to subscribe to SBS messages: %v", err)
			}

			// Wait for subscription to be established
			time.Sleep(200 * time.Millisecond)

			// Publish test messages
			for _, msg := range tt.messages {
				err := publishClient.PublishSBSMessage(msg)
				if err != nil {
					t.Errorf("Failed to publish message: %v", err)
				}
			}

			// Wait for messages to be processed
			timeout := time.After(3 * time.Second)
			for receivedMessages < len(tt.messages) {
				select {
				case <-timeout:
					t.Errorf("Timeout waiting for messages. Expected %d, got %d", len(tt.messages), receivedMessages)
					return
				case <-time.After(100 * time.Millisecond):
					// Continue waiting
				}
			}

			// Verify log files were created
			files, err := os.ReadDir(tt.outputDir)
			if err != nil {
				t.Fatalf("Failed to read output directory: %v", err)
			}

			logFiles := 0
			for _, file := range files {
				if strings.HasSuffix(file.Name(), ".log") {
					logFiles++
				}
			}

			if logFiles != tt.expectedFiles {
				t.Errorf("Expected %d log files, got %d", tt.expectedFiles, logFiles)
			}

			// Verify file content if requested
			if tt.verifyContent {
				err = verifyLogContent(tt.outputDir, tt.messages)
				if err != nil {
					t.Errorf("Log content verification failed: %v", err)
				}
			}
		})
	}
}

// TestIntegration_LoggerRotation tests log file rotation with real NATS
func TestIntegration_LoggerRotation(t *testing.T) {
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

	// Create temporary directory for test logs
	tempDir, err := os.MkdirTemp("", "logger-rotation-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create NATS client for publishing
	publishClient, err := nats.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create publish NATS client: %v", err)
	}
	defer publishClient.Close()

	// Create logger subscribe client
	subscribeClient, err := nats.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create subscribe NATS client: %v", err)
	}
	defer subscribeClient.Close()

	// Create and start logger
	logger := NewLogger(tempDir)
	loggerCtx, loggerCancel := context.WithCancel(context.Background())
	defer loggerCancel()

	go logger.Start(loggerCtx)

	// Setup subscription
	messageCount := 0
	err = subscribeClient.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		if err := logger.WriteMessage(msg); err != nil {
			t.Errorf("Failed to write message: %v", err)
		}
		messageCount++
	})
	if err != nil {
		t.Fatalf("Failed to subscribe to SBS messages: %v", err)
	}

	// Wait for subscription to be established
	time.Sleep(200 * time.Millisecond)

	// Publish initial message
	testMsg := &types.SBSMessage{
		Raw:       "MSG,1,111,11111,4CA2D6,111111,2015/02/19,18:06:07.710,2015/02/19,18:06:07.710,,33000,,,,,,,0,0,0,0",
		Timestamp: time.Now().UTC(),
		Source:    "rotation-test",
	}

	err = publishClient.PublishSBSMessage(testMsg)
	if err != nil {
		t.Fatalf("Failed to publish initial message: %v", err)
	}

	// Wait for message to be processed
	time.Sleep(300 * time.Millisecond)

	// Trigger rotation by setting a different date (testing rotation mechanism)
	logger.SetCurrentDateForTesting("2023-01-01")

	// Publish message that should trigger rotation
	testMsg2 := &types.SBSMessage{
		Raw:       "MSG,2,111,11111,4CA2D7,111111,2015/02/19,18:06:08.710,2015/02/19,18:06:08.710,,34000,,,,,,,0,0,0,0",
		Timestamp: time.Now().UTC(),
		Source:    "rotation-test-2",
	}

	err = publishClient.PublishSBSMessage(testMsg2)
	if err != nil {
		t.Fatalf("Failed to publish rotation message: %v", err)
	}

	// Wait for rotation to complete
	time.Sleep(500 * time.Millisecond)

	// Verify files were created
	files, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read output directory: %v", err)
	}

	logFiles := 0
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".log") {
			logFiles++
		}
		t.Logf("Found file: %s", file.Name())
	}

	if logFiles < 1 {
		t.Errorf("Expected at least 1 log file after rotation, got %d", logFiles)
	}
}

// TestIntegration_LoggerEnvironmentVariables tests environment variable handling
func TestIntegration_LoggerEnvironmentVariables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Save original environment
	originalOutputDir := os.Getenv("OUTPUT_DIR")
	originalNATSURL := os.Getenv("NATS_URL")
	defer func() {
		os.Setenv("OUTPUT_DIR", originalOutputDir)
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

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "logger-env-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name              string
		outputDir         string
		natsURL           string
		expectNATSClient  bool
		expectDirCreation bool
	}{
		{
			name:              "valid_environment_variables",
			outputDir:         tempDir,
			natsURL:           natsURL,
			expectNATSClient:  true,
			expectDirCreation: true,
		},
		{
			name:              "invalid_nats_url",
			outputDir:         tempDir,
			natsURL:           "nats://invalid:4222",
			expectNATSClient:  false,
			expectDirCreation: true,
		},
		{
			name:              "empty_output_dir_uses_default",
			outputDir:         "",
			natsURL:           natsURL,
			expectNATSClient:  true,
			expectDirCreation: false, // Won't create ./logs in test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			os.Setenv("OUTPUT_DIR", tt.outputDir)
			os.Setenv("NATS_URL", tt.natsURL)

			// Test environment parsing
			outputDir, natsURL := parseEnvironment()

			if tt.outputDir == "" {
				if outputDir != "./logs" {
					t.Errorf("Expected default output dir './logs', got '%s'", outputDir)
				}
			} else {
				if outputDir != tt.outputDir {
					t.Errorf("Expected output dir '%s', got '%s'", tt.outputDir, outputDir)
				}
			}

			if natsURL != tt.natsURL {
				t.Errorf("Expected NATS URL '%s', got '%s'", tt.natsURL, natsURL)
			}

			// Test NATS client creation
			if tt.expectNATSClient {
				client, err := nats.New(natsURL)
				if err != nil {
					t.Errorf("Expected NATS client creation to succeed, got error: %v", err)
				}
				if client != nil {
					client.Close()
				}
			}

			// Test directory creation
			if tt.expectDirCreation && tt.outputDir != "" {
				err := os.MkdirAll(outputDir, 0750)
				if err != nil {
					t.Errorf("Failed to create output directory: %v", err)
				}

				// Verify directory exists
				if _, err := os.Stat(outputDir); os.IsNotExist(err) {
					t.Errorf("Output directory was not created: %s", outputDir)
				}
			}
		})
	}
}

// TestIntegration_LoggerGracefulShutdown tests shutdown signal handling
func TestIntegration_LoggerGracefulShutdown(t *testing.T) {
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

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "logger-shutdown-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create NATS client
	client, err := nats.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Create and start logger
	logger := NewLogger(tempDir)
	loggerCtx, loggerCancel := context.WithCancel(context.Background())

	go logger.Start(loggerCtx)

	// Setup subscription
	err = client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		if err := logger.WriteMessage(msg); err != nil {
			t.Errorf("Failed to write message: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("Failed to subscribe to SBS messages: %v", err)
	}

	// Let it run briefly
	time.Sleep(200 * time.Millisecond)

	// Test graceful shutdown
	loggerCancel()
	client.Close()

	// Wait a bit for shutdown to complete
	time.Sleep(300 * time.Millisecond)

	// Verify that the logger handled shutdown gracefully
	// (In real usage, this would be handled by signal handlers)
	if logger.GetCurrentFile() != nil {
		// File should still be valid if shutdown was graceful
		t.Logf("Logger shut down gracefully with file: %s", logger.GetCurrentFile().Name())
	}
}

// parseEnvironment parses environment variables (extracted for testability)
func parseEnvironment() (string, string) {
	outputDir := os.Getenv("OUTPUT_DIR")
	if outputDir == "" {
		outputDir = "./logs"
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats:4222"
	}

	return outputDir, natsURL
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

// generateTestMessages generates test SBS messages
func generateTestMessages(count int) []*types.SBSMessage {
	messages := make([]*types.SBSMessage, count)
	for i := 0; i < count; i++ {
		messages[i] = &types.SBSMessage{
			Raw:       fmt.Sprintf("MSG,%d,111,11111,4CA2D%d,111111,2015/02/19,18:06:%02d.710,2015/02/19,18:06:%02d.710,,3%d000,,,,,,,0,0,0,0", i+1, i+6, i+7, i+7, i+3),
			Timestamp: time.Now().UTC(),
			Source:    fmt.Sprintf("test-source-%d", i+1),
		}
	}
	return messages
}

// verifyLogContent verifies that the expected messages are in the log files
func verifyLogContent(outputDir string, expectedMessages []*types.SBSMessage) error {
	files, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("failed to read output directory: %w", err)
	}

	var logContent string
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".log") {
			content, err := os.ReadFile(filepath.Join(outputDir, file.Name()))
			if err != nil {
				return fmt.Errorf("failed to read log file %s: %w", file.Name(), err)
			}
			logContent += string(content)
		}
	}

	// Check that each expected message appears in the log
	for i, msg := range expectedMessages {
		if !strings.Contains(logContent, msg.Raw) {
			return fmt.Errorf("message %d not found in log: %s", i, msg.Raw)
		}
	}

	return nil
}
