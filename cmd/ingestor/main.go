package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/saviobatista/sbs-logger/internal/metrics"
	"github.com/saviobatista/sbs-logger/internal/nats"
	"github.com/saviobatista/sbs-logger/internal/types"
)

// NATSClient interface for testability
type NATSClient interface {
	PublishSBSMessage(msg *types.SBSMessage) error
	Close()
}

// metricsPort is the default port of the /metrics endpoint.
const metricsPort = "9101"

// maxPendingLine bounds the bytes kept while waiting for a line terminator.
// An SBS line is under 200 bytes; a stream without terminators must not
// grow the buffer forever.
const maxPendingLine = 64 * 1024

// ingestMetrics are the ingestor's Prometheus series, labeled by source.
type ingestMetrics struct {
	messages      metrics.CounterVec
	bytes         metrics.CounterVec
	reconnects    metrics.CounterVec
	publishErrors metrics.CounterVec
	connected     metrics.GaugeVec
}

func newIngestMetrics(reg *metrics.Registry) *ingestMetrics {
	return &ingestMetrics{
		messages:      reg.CounterVec("sbs_ingestor_messages_total", "SBS messages read and published, per source.", "source"),
		bytes:         reg.CounterVec("sbs_ingestor_bytes_total", "Bytes read from the source, per source.", "source"),
		reconnects:    reg.CounterVec("sbs_ingestor_reconnects_total", "Connections to the source after the first one, per source.", "source"),
		publishErrors: reg.CounterVec("sbs_ingestor_publish_errors_total", "Messages that failed to publish to NATS, per source.", "source"),
		connected:     reg.GaugeVec("sbs_ingestor_connected", "1 while connected to the source.", "source"),
	}
}

// ingestorMetrics is used by connectAndIngest; main replaces it with the
// served registry.
var ingestorMetrics = newIngestMetrics(metrics.NewRegistry())

func main() {
	// Load configuration
	sources := os.Getenv("SOURCES")
	if sources == "" {
		log.Fatal("SOURCES environment variable is required")
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats:4222" // Default to Docker service name
	}

	// Create NATS client
	client, err := nats.New(natsURL)
	if err != nil {
		log.Printf("Failed to create NATS client: %v", err)
		os.Exit(1)
	}
	defer client.Close()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prometheus metrics (default :9101, METRICS_ADDR overrides, empty disables)
	reg := metrics.NewRegistry()
	ingestorMetrics = newIngestMetrics(reg)
	reg.Serve(ctx, metrics.Addr(metricsPort))

	// Start ingesting from each source
	sourceList := strings.Split(sources, ",")
	for _, source := range sourceList {
		source = strings.TrimSpace(source)
		go ingestSource(ctx, source, client)
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	cancel()
	time.Sleep(time.Second) // Give time for goroutines to clean up
}

func ingestSource(ctx context.Context, source string, client NATSClient) {
	connections := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if connections > 0 {
				ingestorMetrics.reconnects.With(source).Inc()
			}
			connections++
			if err := connectAndIngest(ctx, source, client); err != nil {
				log.Printf("Error from source %s: %v", source, err)
				time.Sleep(5 * time.Second) // Wait before retrying
			}
		}
	}
}

// splitLines returns the complete lines in buf and the bytes after the last
// line terminator, which may be the start of a line still being received.
// SBS/BaseStation lines end in "\r\n"; a bare "\n" is accepted too. The
// lines have the terminator and surrounding spaces removed: the NATS payload
// is the bare SBS message (the tracker parses it as is, the logger adds the
// "\n" when it writes the file).
func splitLines(buf []byte) (lines []string, rest []byte) {
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return lines, buf
		}
		if line := strings.TrimSpace(string(buf[:i])); line != "" {
			lines = append(lines, line)
		}
		buf = buf[i+1:]
	}
}

func connectAndIngest(ctx context.Context, source string, client NATSClient) error {
	// Create TCP connection
	conn, err := connectWithRetry(source)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing conn: %v\n", err)
		}
	}()

	log.Printf("Connected to source: %s", source)
	connected := ingestorMetrics.connected.With(source)
	connected.Set(1)
	defer connected.Set(0)

	buf := make([]byte, 4096)
	var pending []byte

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// Set read deadline
			if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
				return fmt.Errorf("failed to set read deadline: %w", err)
			}

			// Read data
			n, err := conn.Read(buf)
			if err != nil {
				return fmt.Errorf("read error: %w", err)
			}
			ingestorMetrics.bytes.With(source).Add(float64(n))

			pending = append(pending, buf[:n]...)
			var lines []string
			lines, pending = splitLines(pending)
			if len(pending) > maxPendingLine {
				log.Printf("Dropping %d bytes from %s without a line terminator", len(pending), source)
				pending = pending[:0]
			}
			// Keep the partial line in a buffer of its own, so the next
			// append does not grow the old backing array forever.
			pending = append([]byte(nil), pending...)

			for _, line := range lines {
				msg := &types.SBSMessage{
					Raw:       line,
					Timestamp: time.Now().UTC(),
					Source:    source,
				}
				if err := client.PublishSBSMessage(msg); err != nil {
					ingestorMetrics.publishErrors.With(source).Inc()
					log.Printf("Failed to publish message: %v", err)
					continue
				}
				ingestorMetrics.messages.With(source).Inc()
			}
		}
	}
}

func connectWithRetry(source string) (*net.TCPConn, error) {
	addr, err := net.ResolveTCPAddr("tcp", source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	for {
		conn, err := net.DialTCP("tcp", nil, addr)
		if err == nil {
			return conn, nil
		}

		log.Printf("Failed to connect to %s: %v. Retrying in 5 seconds...", source, err)
		time.Sleep(5 * time.Second)
	}
}
