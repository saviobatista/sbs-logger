package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/types"
)

// TestParseEnvironment tests environment variable parsing
func TestParseEnvironment(t *testing.T) {
	// Save original environment
	originalOutputDir := os.Getenv("OUTPUT_DIR")
	originalNATSURL := os.Getenv("NATS_URL")
	defer func() {
		os.Setenv("OUTPUT_DIR", originalOutputDir)
		os.Setenv("NATS_URL", originalNATSURL)
	}()

	tests := []struct {
		name              string
		outputDir         string
		natsURL           string
		expectedOutputDir string
		expectedNATSURL   string
	}{
		{
			name:              "default values",
			outputDir:         "",
			natsURL:           "",
			expectedOutputDir: "./logs",
			expectedNATSURL:   "nats://nats:4222",
		},
		{
			name:              "custom values",
			outputDir:         "/tmp/custom-logs",
			natsURL:           "nats://custom:4222",
			expectedOutputDir: "/tmp/custom-logs",
			expectedNATSURL:   "nats://custom:4222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			os.Setenv("OUTPUT_DIR", tt.outputDir)
			os.Setenv("NATS_URL", tt.natsURL)

			outputDir, natsURL := parseEnvironment()

			if outputDir != tt.expectedOutputDir {
				t.Errorf("Expected output dir %q, got %q", tt.expectedOutputDir, outputDir)
			}

			if natsURL != tt.expectedNATSURL {
				t.Errorf("Expected NATS URL %q, got %q", tt.expectedNATSURL, natsURL)
			}
		})
	}
}

// TestNewLogger tests the logger constructor
func TestNewLogger(t *testing.T) {
	outputDir := "/tmp/test-logs"
	logger := NewLogger(outputDir)

	if logger.outputDir != outputDir {
		t.Errorf("Expected output dir %q, got %q", outputDir, logger.outputDir)
	}

	if logger.rotationChan == nil {
		t.Error("Expected rotation channel to be initialized")
	}

	if logger.GetCurrentFile() != nil {
		t.Error("Expected current file to be nil initially")
	}

	if logger.GetCurrentDate() != "" {
		t.Error("Expected current date to be empty initially")
	}
}

// TestLogger_Start tests logger initialization with error scenarios
func TestLogger_Start(t *testing.T) {
	tests := []struct {
		name              string
		setupOutputDir    func() (string, func())
		expectFileCreated bool
	}{
		{
			name: "successful start",
			setupOutputDir: func() (string, func()) {
				tempDir, err := os.MkdirTemp("", "logger-test")
				if err != nil {
					panic(err)
				}
				return tempDir, func() { _ = os.RemoveAll(tempDir) }
			},
			expectFileCreated: true,
		},
		{
			name: "start with invalid output directory",
			setupOutputDir: func() (string, func()) {
				// Use a path that can't be created (file exists with same name)
				tempDir, err := os.MkdirTemp("", "logger-test")
				if err != nil {
					panic(err)
				}
				// Create a file with the same name as the directory we'll try to create
				invalidDir := filepath.Join(tempDir, "blocked")
				_ = os.WriteFile(invalidDir, []byte("blocking file"), 0600)
				// Return the blocked path as the output directory
				return invalidDir, func() { _ = os.RemoveAll(tempDir) }
			},
			expectFileCreated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir, cleanup := tt.setupOutputDir()
			defer cleanup()

			logger := NewLogger(outputDir)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			// Start logger in goroutine
			go logger.Start(ctx)

			// Wait for context to complete
			<-ctx.Done()

			// Check results
			if tt.expectFileCreated {
				if logger.GetCurrentFile() == nil {
					t.Error("Expected current file to be set after successful start")
				} else {
					_ = logger.GetCurrentFile().Close()
				}
				if logger.GetCurrentDate() == "" {
					t.Error("Expected current date to be set after successful start")
				}
			} else {
				if logger.GetCurrentFile() != nil {
					t.Error("Expected current file to be nil after failed start")
					_ = logger.GetCurrentFile().Close()
				}
			}
		})
	}
}

// TestLogger_RotateFile tests file rotation with error scenarios
func TestLogger_RotateFile(t *testing.T) {
	tests := []struct {
		name        string
		setupDir    func() (string, func())
		expectError bool
	}{
		{
			name: "successful rotation",
			setupDir: func() (string, func()) {
				tempDir, err := os.MkdirTemp("", "logger-test")
				if err != nil {
					panic(err)
				}
				return tempDir, func() { os.RemoveAll(tempDir) }
			},
			expectError: false,
		},
		{
			name: "rotation with invalid directory",
			setupDir: func() (string, func()) {
				// Return a directory that doesn't exist and can't be created
				return "/invalid/path/that/doesnt/exist", func() {}
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir, cleanup := tt.setupDir()
			defer cleanup()

			logger := NewLogger(outputDir)

			err := logger.rotateFile()

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if !tt.expectError {
				if logger.GetCurrentFile() == nil {
					t.Error("Expected current file to be set")
				} else {
					logger.GetCurrentFile().Close()
				}
				if logger.GetCurrentDate() == "" {
					t.Error("Expected current date to be set")
				}

				// Verify file exists
				expectedDate := time.Now().UTC().Format("2006-01-02")
				expectedPath := filepath.Join(outputDir, fmt.Sprintf("sbs_%s.log", expectedDate))
				if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
					t.Errorf("Expected log file to exist at %s", expectedPath)
				}
			}
		})
	}
}

// TestLogger_WriteMessage tests message writing with error scenarios
func TestLogger_WriteMessage(t *testing.T) {
	tests := []struct {
		name        string
		setupLogger func() (*Logger, func())
		message     *types.SBSMessage
		expectError bool
	}{
		{
			name: "successful write",
			setupLogger: func() (*Logger, func()) {
				tempDir, err := os.MkdirTemp("", "logger-test")
				if err != nil {
					panic(err)
				}
				logger := NewLogger(tempDir)
				err = logger.rotateFile()
				if err != nil {
					panic(err)
				}
				return logger, func() {
					if logger.GetCurrentFile() != nil {
						logger.GetCurrentFile().Close()
					}
					os.RemoveAll(tempDir)
				}
			},
			message: &types.SBSMessage{
				Raw:       "test message\n",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			expectError: false,
		},
		{
			name: "write with date rotation trigger",
			setupLogger: func() (*Logger, func()) {
				tempDir, err := os.MkdirTemp("", "logger-test")
				if err != nil {
					panic(err)
				}
				logger := NewLogger(tempDir)
				err = logger.rotateFile()
				if err != nil {
					panic(err)
				}
				// Set date to yesterday to trigger rotation
				logger.SetCurrentDateForTesting(time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"))
				return logger, func() {
					if logger.GetCurrentFile() != nil {
						logger.GetCurrentFile().Close()
					}
					os.RemoveAll(tempDir)
				}
			},
			message: &types.SBSMessage{
				Raw:       "test message\n",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, cleanup := tt.setupLogger()
			defer cleanup()

			err := logger.WriteMessage(tt.message)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// For the rotation trigger test, verify rotation was triggered
			if tt.name == "write with date rotation trigger" {
				select {
				case <-logger.rotationChan:
					// Rotation was triggered as expected
				case <-time.After(10 * time.Millisecond):
					t.Error("Expected rotation to be triggered, but channel was empty")
				}
			}
		})
	}
}

// TestCompressFile tests comprehensive compression scenarios
func TestCompressFile(t *testing.T) {
	tests := []struct {
		name          string
		setupFile     func() (string, func())
		expectError   bool
		errorContains string
	}{
		{
			name: "successful compression",
			setupFile: func() (string, func()) {
				tempDir, err := os.MkdirTemp("", "logger-test")
				if err != nil {
					panic(err)
				}
				testFile := filepath.Join(tempDir, "test.log")
				err = os.WriteFile(testFile, []byte("test content\n"), 0600)
				if err != nil {
					panic(err)
				}
				return testFile, func() { os.RemoveAll(tempDir) }
			},
			expectError: false,
		},
		{
			name: "compress non-existent file",
			setupFile: func() (string, func()) {
				return "/tmp/non-existent-file.log", func() {}
			},
			expectError:   true,
			errorContains: "failed to read file",
		},
		{
			name: "compress empty file",
			setupFile: func() (string, func()) {
				tempDir, err := os.MkdirTemp("", "logger-test")
				if err != nil {
					panic(err)
				}
				testFile := filepath.Join(tempDir, "empty.log")
				err = os.WriteFile(testFile, []byte(""), 0600)
				if err != nil {
					panic(err)
				}
				return testFile, func() { os.RemoveAll(tempDir) }
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile, cleanup := tt.setupFile()
			defer cleanup()

			err := compressFile(testFile)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
			if tt.expectError && tt.errorContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorContains, err)
				}
			}

			if !tt.expectError {
				// Verify original file is gone
				if _, err := os.Stat(testFile); !os.IsNotExist(err) {
					t.Error("Expected original file to be deleted after compression")
				}

				// Verify compressed file exists
				compressedFile := testFile + ".gz"
				if _, err := os.Stat(compressedFile); os.IsNotExist(err) {
					t.Error("Expected compressed file to exist")
				}

				// Verify compressed file has content (basic check)
				compressedContent, err := os.ReadFile(compressedFile)
				if err != nil {
					t.Errorf("Failed to read compressed file: %v", err)
				}

				if len(compressedContent) == 0 && tt.name != "compress empty file" {
					t.Error("Expected compressed file to have content")
				}
			}
		})
	}
}

// TestLogger_ErrorScenarios tests various error scenarios
func TestLogger_ErrorScenarios(t *testing.T) {
	t.Run("write to closed file", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "logger-test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer func() { _ = os.RemoveAll(tempDir) }()

		logger := NewLogger(tempDir)
		err = logger.rotateFile()
		if err != nil {
			t.Fatalf("Failed to initialize logger: %v", err)
		}

		// Close the file to simulate error condition
		logger.GetCurrentFile().Close()

		// Try to write message (should fail)
		testMessage := &types.SBSMessage{
			Raw:       "test message\n",
			Timestamp: time.Now(),
			Source:    "test-source",
		}

		err = logger.WriteMessage(testMessage)
		if err == nil {
			t.Error("Expected error when writing to closed file")
		}
		if !strings.Contains(err.Error(), "failed to write message") {
			t.Errorf("Expected 'failed to write message' error, got: %v", err)
		}
	})
}

// TestRunLogger tests the main application logic
func TestRunLogger(t *testing.T) {
	// Save original environment
	originalOutputDir := os.Getenv("OUTPUT_DIR")
	originalNATSURL := os.Getenv("NATS_URL")
	defer func() {
		os.Setenv("OUTPUT_DIR", originalOutputDir)
		os.Setenv("NATS_URL", originalNATSURL)
	}()

	tests := []struct {
		name          string
		outputDir     string
		natsURL       string
		expectError   bool
		errorContains string
	}{
		{
			name:          "invalid output directory",
			outputDir:     "/invalid/path/that/doesnt/exist",
			natsURL:       "",
			expectError:   true,
			errorContains: "failed to create output directory",
		},
		{
			name:          "invalid NATS URL",
			outputDir:     "/tmp/test-logs",
			natsURL:       "invalid://url",
			expectError:   true,
			errorContains: "failed to create NATS client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			os.Setenv("OUTPUT_DIR", tt.outputDir)
			os.Setenv("NATS_URL", tt.natsURL)

			// Create a temporary directory for valid tests
			if tt.outputDir == "/tmp/test-logs" {
				tempDir, err := os.MkdirTemp("", "logger-test")
				if err != nil {
					t.Fatalf("Failed to create temp dir: %v", err)
				}
				defer os.RemoveAll(tempDir)
				os.Setenv("OUTPUT_DIR", tempDir)
			}

			// Run the logger
			err := runLogger()

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
			if tt.expectError && tt.errorContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorContains, err)
				}
			}
		})
	}
}

// TestRunLogger_Integration tests the logger with a real NATS server
func TestRunLogger_Integration(t *testing.T) {
	// This test requires a NATS server to be running
	// For now, we'll test the error case when NATS is not available
	t.Run("NATS server not available", func(t *testing.T) {
		// Save original environment
		originalOutputDir := os.Getenv("OUTPUT_DIR")
		originalNATSURL := os.Getenv("NATS_URL")
		defer func() {
			os.Setenv("OUTPUT_DIR", originalOutputDir)
			os.Setenv("NATS_URL", originalNATSURL)
		}()

		// Set up test environment
		tempDir, err := os.MkdirTemp("", "logger-test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		os.Setenv("OUTPUT_DIR", tempDir)
		os.Setenv("NATS_URL", "nats://localhost:4222") // Use localhost which should not be available

		// Run the logger
		err = runLogger()
		if err == nil {
			t.Error("Expected error when NATS server is not available")
		}
		if !strings.Contains(err.Error(), "failed to create NATS client") {
			t.Errorf("Expected NATS client error, got: %v", err)
		}
	})
}

// TestParseEnvironment_Integration tests the environment parsing function
func TestParseEnvironment_Integration(t *testing.T) {
	// Save original environment
	originalOutputDir := os.Getenv("OUTPUT_DIR")
	originalNATSURL := os.Getenv("NATS_URL")
	defer func() {
		os.Setenv("OUTPUT_DIR", originalOutputDir)
		os.Setenv("NATS_URL", originalNATSURL)
	}()

	tests := []struct {
		name              string
		outputDir         string
		natsURL           string
		expectedOutputDir string
		expectedNATSURL   string
	}{
		{
			name:              "default values",
			outputDir:         "",
			natsURL:           "",
			expectedOutputDir: "./logs",
			expectedNATSURL:   "nats://nats:4222",
		},
		{
			name:              "custom values",
			outputDir:         "/tmp/custom-logs",
			natsURL:           "nats://custom:4222",
			expectedOutputDir: "/tmp/custom-logs",
			expectedNATSURL:   "nats://custom:4222",
		},
		{
			name:              "partial custom values",
			outputDir:         "/tmp/partial-logs",
			natsURL:           "",
			expectedOutputDir: "/tmp/partial-logs",
			expectedNATSURL:   "nats://nats:4222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			os.Setenv("OUTPUT_DIR", tt.outputDir)
			os.Setenv("NATS_URL", tt.natsURL)

			outputDir, natsURL := parseEnvironment()

			if outputDir != tt.expectedOutputDir {
				t.Errorf("Expected output dir %q, got %q", tt.expectedOutputDir, outputDir)
			}

			if natsURL != tt.expectedNATSURL {
				t.Errorf("Expected NATS URL %q, got %q", tt.expectedNATSURL, natsURL)
			}
		})
	}
}

// Helper functions
