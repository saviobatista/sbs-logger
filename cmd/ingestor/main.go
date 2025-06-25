package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/savio/sbs-logger/internal/nats"
	"github.com/savio/sbs-logger/internal/types"
)

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
		log.Fatalf("Failed to create NATS client: %v", err)
	}
	defer client.Close()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

func ingestSource(ctx context.Context, source string, client *nats.Client) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := connectAndIngest(ctx, source, client); err != nil {
				log.Printf("Error from source %s: %v", source, err)
				time.Sleep(5 * time.Second) // Wait before retrying
			}
		}
	}
}

func connectAndIngest(ctx context.Context, source string, client *nats.Client) error {
	// Create TCP connection
	conn, err := connectWithRetry(source)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	log.Printf("Connected to source: %s", source)

	// Create buffer for reading messages
	buf := make([]byte, 1024)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// Set read deadline
			if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
				return fmt.Errorf("failed to set read deadline: %w", err)
			}

			// Read message
			n, err := conn.Read(buf)
			if err != nil {
				return fmt.Errorf("read error: %w", err)
			}

			// Create and publish message
			msg := &types.SBSMessage{
				Raw:       string(buf[:n]),
				Timestamp: time.Now().UTC(),
				Source:    source,
			}

			if err := client.PublishSBSMessage(msg); err != nil {
				log.Printf("Failed to publish message: %v", err)
				continue
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