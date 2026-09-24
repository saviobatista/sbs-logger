package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/saviobatista/sbs-logger/internal/metrics"
	"github.com/saviobatista/sbs-logger/internal/nats"
	"github.com/saviobatista/sbs-logger/internal/types"
)

func main() {
	if err := runLogger(); err != nil {
		log.Printf("Logger failed: %v", err)
		os.Exit(1)
	}
}

// runLogger contains the main application logic and can be tested
func runLogger() error {
	// Load configuration
	outputDir, natsURL := parseEnvironment()

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create NATS client
	client, err := nats.New(natsURL)
	if err != nil {
		return fmt.Errorf("failed to create NATS client: %w", err)
	}
	// Note: client.Close() will be called in the shutdown handler

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	// Note: cancel() will be called in the shutdown handler

	// Start the logger
	logger := NewLogger(outputDir)
	logger.Start(ctx)

	// Prometheus metrics (default :9102, METRICS_ADDR overrides, empty disables)
	logger.metrics.Serve(ctx, metrics.Addr(metricsPort))

	// Consume SBS messages through the durable "sbs-logger" consumer: a
	// restart resumes after the last message written instead of replaying
	// the whole stream (duplicates) or skipping what arrived meanwhile.
	consumerErr := make(chan error, 1)
	go func() {
		consumerErr <- client.Consume(ctx, nats.ConsumeOptions{Durable: nats.ConsumerLogger, Batch: 500}, logger.WriteBatch)
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	var runErr error
	select {
	case <-sigChan:
		log.Println("Shutting down...")
	case err := <-consumerErr:
		runErr = fmt.Errorf("consumer stopped unexpectedly: %v", err)
	}

	cancel() // stop fetching; the batch in progress is written and acknowledged
	select {
	case <-consumerErr:
	case <-time.After(10 * time.Second):
	}
	client.Close()
	logger.Close()

	return runErr
}

// parseEnvironment extracts environment variables with defaults
func parseEnvironment() (string, string) {
	outputDir := os.Getenv("OUTPUT_DIR")
	if outputDir == "" {
		outputDir = "./logs" // Default output directory
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats:4222" // Default to Docker service name
	}

	return outputDir, natsURL
}

// metricsPort is the default port of the /metrics endpoint.
const metricsPort = "9102"

// Logger handles writing messages to log files
type Logger struct {
	outputDir    string
	currentFile  *os.File
	currentDate  string
	rotationChan chan struct{}
	mu           sync.RWMutex

	metrics     *metrics.Registry
	messages    metrics.Counter
	bytes       metrics.Counter
	writeErrors metrics.Counter
	lastIngest  atomic.Int64 // ingest time (Unix ns) of the latest message written
}

// NewLogger creates a new logger instance
func NewLogger(outputDir string) *Logger {
	reg := metrics.NewRegistry()
	l := &Logger{
		outputDir:    outputDir,
		rotationChan: make(chan struct{}, 1),
		metrics:      reg,
		messages:     reg.Counter("sbs_logger_messages_written_total", "SBS messages written to the log files."),
		bytes:        reg.Counter("sbs_logger_bytes_written_total", "Bytes written to the log files."),
		writeErrors:  reg.Counter("sbs_logger_write_errors_total", "Failed writes to the log files."),
	}
	reg.GaugeFunc("sbs_logger_lag_seconds", "Wall clock minus the ingest time of the latest message written.", func() float64 {
		last := l.lastIngest.Load()
		if last == 0 {
			return 0
		}
		return time.Since(time.Unix(0, last)).Seconds()
	})
	return l
}

// Start initializes the logger and starts the rotation timer
func (l *Logger) Start(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Initialize the current file
	if err := l.rotateFile(); err != nil {
		log.Printf("Failed to create initial log file: %v", err)
		return
	}

	// Start rotation timer
	go l.rotationTimer(ctx)
}

// Close closes the current file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.currentFile != nil {
		if err := l.currentFile.Close(); err != nil {
			log.Printf("Failed to close log file: %v", err)
		}
		l.currentFile = nil
	}
}

// formatLine returns the message as one line terminated by "\n". The
// ingestor publishes the SBS message without its "\r\n" terminator; writing
// it as is concatenated every message of the day into a single line.
func formatLine(raw string) string {
	raw = strings.TrimRight(raw, "\r\n")
	if raw == "" {
		return ""
	}
	return raw + "\n"
}

// WriteMessage writes a message to the current log file
func (l *Logger) WriteMessage(msg *types.SBSMessage) error {
	return l.WriteBatch([]*types.SBSMessage{msg})
}

// WriteBatch writes the messages to the current log file, one line each, in
// a single write.
func (l *Logger) WriteBatch(msgs []*types.SBSMessage) error {
	var b strings.Builder
	var lines int
	var last time.Time
	for _, msg := range msgs {
		if line := formatLine(msg.Raw); line != "" {
			b.WriteString(line)
			lines++
		}
		if msg.Timestamp.After(last) {
			last = msg.Timestamp
		}
	}
	if b.Len() == 0 {
		return nil
	}

	l.mu.RLock()
	currentDate := l.currentDate
	l.mu.RUnlock()

	// Ask for a rotation when the day changed. Never block: the rotation
	// goroutine may be busy, and one request is enough.
	if currentDate != time.Now().UTC().Format("2006-01-02") {
		select {
		case l.rotationChan <- struct{}{}:
		default:
		}
	}

	// Write under the read lock so a rotation cannot close the file mid-write.
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.currentFile == nil {
		l.writeErrors.Inc()
		return fmt.Errorf("failed to write message: no open log file")
	}
	n, err := l.currentFile.WriteString(b.String())
	l.bytes.Add(float64(n))
	if err != nil {
		l.writeErrors.Inc()
		return fmt.Errorf("failed to write message: %w", err)
	}
	l.messages.Add(float64(lines))
	if !last.IsZero() {
		l.lastIngest.Store(last.UnixNano())
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

// rotateAndCompress closes the current file, opens the new day's file and
// compresses the previous day's log. Compression runs after the lock is
// released, so writes to the new file do not wait for it.
func (l *Logger) rotateAndCompress() error {
	l.mu.Lock()
	today := time.Now().UTC().Format("2006-01-02")
	if l.currentDate == today && l.currentFile != nil {
		l.mu.Unlock()
		return nil // already rotated
	}

	// Close current file
	if l.currentFile != nil {
		if err := l.currentFile.Close(); err != nil {
			l.mu.Unlock()
			return fmt.Errorf("failed to close current file: %w", err)
		}
		l.currentFile = nil
	}

	prevDate := l.currentDate
	err := l.rotateFile()
	l.mu.Unlock()

	// Compress previous day's log if it exists
	if prevDate != "" && prevDate != today {
		prevLogPath := filepath.Join(l.outputDir, fmt.Sprintf("sbs_%s.log", prevDate))
		if cerr := compressFile(prevLogPath); cerr != nil {
			log.Printf("Failed to compress previous log: %v", cerr)
		}
	}
	return err
}

// rotateFile creates a new log file for the current day
func (l *Logger) rotateFile() error {
	// Get current date
	currentDate := time.Now().UTC().Format("2006-01-02")
	logPath := filepath.Join(l.outputDir, fmt.Sprintf("sbs_%s.log", currentDate))

	// Create new file
	//nolint:gosec // logPath is controlled by application logic
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	l.currentFile = file
	l.currentDate = currentDate
	return nil
}

// compressFile gzips a log file into filePath+".gz" and removes the original.
// It used to copy the bytes into the .gz file uncompressed, so the "gzip"
// files were plain text that gzip refuses to read.
func compressFile(filePath string) error {
	//nolint:gosec // filePath is controlled by application logic
	src, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	defer func() { _ = src.Close() }()

	compressedPath := filePath + ".gz"
	tmpPath := compressedPath + ".tmp"
	//nolint:gosec // tmpPath is controlled by application logic
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("failed to create compressed file: %w", err)
	}
	zw := gzip.NewWriter(dst)
	zw.Name = filepath.Base(filePath)
	if _, err := io.Copy(zw, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write compressed data: %w", err)
	}
	if err := zw.Close(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write compressed data: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close compressed file: %w", err)
	}
	// Only a complete archive gets the final name, and only then is the
	// original removed.
	if err := os.Rename(tmpPath, compressedPath); err != nil {
		return fmt.Errorf("failed to rename compressed file: %w", err)
	}
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove original file: %w", err)
	}
	return nil
}

// GetCurrentFile returns the current file in a thread-safe manner
func (l *Logger) GetCurrentFile() *os.File {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.currentFile
}

// GetCurrentDate returns the current date in a thread-safe manner
func (l *Logger) GetCurrentDate() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.currentDate
}

// SetCurrentDateForTesting sets the current date for testing purposes
func (l *Logger) SetCurrentDateForTesting(date string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.currentDate = date
}
