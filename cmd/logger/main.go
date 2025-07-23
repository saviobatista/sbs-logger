package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/savio/sbs-logger/internal/nats"
	"github.com/savio/sbs-logger/internal/types"
)

func main() {
	// Load configuration
	outputDir := os.Getenv("OUTPUT_DIR")
	if outputDir == "" {
		outputDir = "./logs" // Default output directory
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats:4222" // Default to Docker service name
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Create NATS client
	client, err := nats.New(natsURL)
	if err != nil {
		log.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the logger
	logger := NewLogger(outputDir)
	go logger.Start(ctx)

	// Subscribe to SBS messages
	if err := client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		if err := logger.WriteMessage(msg); err != nil {
			log.Printf("Failed to write message: %v", err)
		}
	}); err != nil {
		log.Fatalf("Failed to subscribe to SBS messages: %v", err)
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	cancel()
	time.Sleep(time.Second) // Give time for goroutines to clean up
}

// Logger handles writing messages to log files
type Logger struct {
	outputDir    string
	currentFile  *os.File
	currentDate  string
	rotationChan chan struct{}
}

// NewLogger creates a new logger instance
func NewLogger(outputDir string) *Logger {
	return &Logger{
		outputDir:    outputDir,
		rotationChan: make(chan struct{}, 1),
	}
}

// Start initializes the logger and starts the rotation timer
func (l *Logger) Start(ctx context.Context) {
	// Initialize the current file
	if err := l.rotateFile(); err != nil {
		log.Printf("Failed to create initial log file: %v", err)
		return
	}

	// Start rotation timer
	go l.rotationTimer(ctx)
}

// WriteMessage writes a message to the current log file
func (l *Logger) WriteMessage(msg *types.SBSMessage) error {
	// Check if we need to rotate
	if l.currentDate != time.Now().UTC().Format("2006-01-02") {
		l.rotationChan <- struct{}{}
	}

	// Write message to file
	if _, err := l.currentFile.WriteString(msg.Raw); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// rotationTimer handles daily log rotation
func (l *Logger) rotationTimer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.rotationChan:
			if err := l.rotateAndCompress(); err != nil {
				log.Printf("Failed to rotate logs: %v", err)
			}
		}
	}
}

// rotateAndCompress closes the current file, compresses the previous day's log,
// and creates a new log file
func (l *Logger) rotateAndCompress() error {
	// Close current file
	if l.currentFile != nil {
		if err := l.currentFile.Close(); err != nil {
			return fmt.Errorf("failed to close current file: %w", err)
		}
	}

	// Compress previous day's log if it exists
	if l.currentDate != "" {
		prevLogPath := filepath.Join(l.outputDir, fmt.Sprintf("sbs_%s.log", l.currentDate))
		if err := compressFile(prevLogPath); err != nil {
			log.Printf("Failed to compress previous log: %v", err)
		}
	}

	// Create new log file
	return l.rotateFile()
}

// rotateFile creates a new log file for the current day
func (l *Logger) rotateFile() error {
	// Get current date
	l.currentDate = time.Now().UTC().Format("2006-01-02")
	logPath := filepath.Join(l.outputDir, fmt.Sprintf("sbs_%s.log", l.currentDate))

	// Create new file
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	l.currentFile = file
	return nil
}

// compressFile compresses a log file using gzip
func compressFile(filePath string) error {
	// Read the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Create compressed file
	compressedPath := filePath + ".gz"
	compressedFile, err := os.Create(compressedPath)
	if err != nil {
		return fmt.Errorf("failed to create compressed file: %w", err)
	}
	defer compressedFile.Close()

	// Write compressed data
	if _, err := compressedFile.Write(data); err != nil {
		return fmt.Errorf("failed to write compressed data: %w", err)
	}

	// Remove original file
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove original file: %w", err)
	}

	return nil
}
