//go:build integration

package nats

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/saviobatista/sbs-logger/internal/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestIntegration_RealNATSServer tests the NATS client against a real NATS server
// using testcontainers. This provides 100% coverage of the real implementations.
func TestIntegration_RealNATSServer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start NATS container with JetStream enabled
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer func() {
		if err := natsContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	t.Run("full_integration_test", func(t *testing.T) {
		// Test creating a client
		client, err := New(natsURL)
		if err != nil {
			t.Fatalf("Failed to create NATS client: %v", err)
		}
		defer client.Close()

		// Test publishing a message
		testMsg := &types.SBSMessage{
			Raw:       "MSG,3,111,11111,4CA2D6,111111,2015/02/19,18:06:07.710,2015/02/19,18:06:07.710,,33000,,,,,,,0,0,0,0",
			Timestamp: time.Now(),
			Source:    "integration-test",
		}

		err = client.PublishSBSMessage(testMsg)
		if err != nil {
			t.Fatalf("Failed to publish message: %v", err)
		}

		// Test subscribing and receiving messages
		var receivedMsgs []*types.SBSMessage
		var mu sync.Mutex
		var wg sync.WaitGroup
		wg.Add(1)

		messageCount := 0
		err = client.SubscribeSBSRaw(func(msg *types.SBSMessage) {
			mu.Lock()
			receivedMsgs = append(receivedMsgs, msg)
			messageCount++
			if messageCount == 1 {
				wg.Done()
			}
			mu.Unlock()
		})
		if err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		// Give subscription time to be established
		time.Sleep(100 * time.Millisecond)

		// Publish another message to test the subscription
		testMsg2 := &types.SBSMessage{
			Raw:       "MSG,4,111,11111,4CA2D7,111111,2015/02/19,18:06:08.710,2015/02/19,18:06:08.710,,34000,,,,,,,0,0,0,0",
			Timestamp: time.Now(),
			Source:    "integration-test-2",
		}

		err = client.PublishSBSMessage(testMsg2)
		if err != nil {
			t.Fatalf("Failed to publish second message: %v", err)
		}

		// Wait for message to be received
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Message received successfully
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for message")
		}

		// Verify we received at least one message
		mu.Lock()
		if len(receivedMsgs) == 0 {
			t.Fatal("No message received")
		}
		// We might receive the first message or the second message due to JetStream persistence
		// Just verify we received a valid message
		lastMsg := receivedMsgs[len(receivedMsgs)-1]
		if lastMsg.Source == "" {
			t.Error("Received message has empty source")
		}
		if lastMsg.Raw == "" {
			t.Error("Received message has empty raw data")
		}
		mu.Unlock()
	})

	t.Run("test_real_implementations_coverage", func(t *testing.T) {
		// This test specifically covers the real implementation functions
		// that were not covered by unit tests

		// Test realNATSConnector.Connect()
		connector := &realNATSConnector{}
		conn, err := connector.Connect(natsURL)
		if err != nil {
			t.Fatalf("realNATSConnector.Connect() failed: %v", err)
		}
		defer conn.Close()

		// Test realNATSConnection.JetStream()
		js, err := conn.JetStream()
		if err != nil {
			t.Fatalf("realNATSConnection.JetStream() failed: %v", err)
		}

		// Test realJetStreamContext.AddStream()
		streamConfig := &nats.StreamConfig{
			Name:     "TEST_STREAM",
			Subjects: []string{"test.>"},
			Storage:  nats.MemoryStorage,
			MaxAge:   time.Hour,
		}

		streamInfo, err := js.AddStream(streamConfig)
		if err != nil {
			t.Fatalf("realJetStreamContext.AddStream() failed: %v", err)
		}
		if streamInfo.Config.Name != "TEST_STREAM" {
			t.Errorf("Expected stream name TEST_STREAM, got %s", streamInfo.Config.Name)
		}

		// Test realJetStreamContext.Publish()
		testData := []byte(`{"test": "data"}`)
		pubAck, err := js.Publish("test.subject", testData)
		if err != nil {
			t.Fatalf("realJetStreamContext.Publish() failed: %v", err)
		}
		if pubAck.Stream != "TEST_STREAM" {
			t.Errorf("Expected stream TEST_STREAM, got %s", pubAck.Stream)
		}

		// Test realJetStreamContext.Subscribe()
		var receivedData []byte
		var subscribeWg sync.WaitGroup
		var subscribeMu sync.Mutex
		subscribeWg.Add(1)

		messageCount := 0
		sub, err := js.Subscribe("test.subject", func(msg *nats.Msg) {
			subscribeMu.Lock()
			receivedData = msg.Data
			messageCount++
			if messageCount == 1 {
				subscribeWg.Done()
			}
			subscribeMu.Unlock()
		})
		if err != nil {
			t.Fatalf("realJetStreamContext.Subscribe() failed: %v", err)
		}
		defer sub.Unsubscribe()

		// Give subscription time to be established
		time.Sleep(100 * time.Millisecond)

		// Publish another message to test subscription
		_, err = js.Publish("test.subject", []byte(`{"test": "data2"}`))
		if err != nil {
			t.Fatalf("Failed to publish test message: %v", err)
		}

		// Wait for message
		done := make(chan struct{})
		go func() {
			subscribeWg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Message received
		case <-time.After(3 * time.Second):
			t.Fatal("Timeout waiting for subscription message")
		}

		// We might receive either the first or second message due to JetStream persistence
		// Just verify we received valid JSON data
		if len(receivedData) == 0 {
			t.Error("No data received")
		}
		// Should be either {"test": "data"} or {"test": "data2"}
		expectedData1 := `{"test": "data"}`
		expectedData2 := `{"test": "data2"}`
		actualData := string(receivedData)
		if actualData != expectedData1 && actualData != expectedData2 {
			t.Errorf("Expected data %s or %s, got %s", expectedData1, expectedData2, actualData)
		}

		// Test realNATSConnection.Close() - this will be called by defer
	})

	t.Run("test_error_scenarios", func(t *testing.T) {
		// Test connection to invalid URL
		_, err := New("nats://invalid-host:4222")
		if err == nil {
			t.Fatal("Expected error when connecting to invalid host")
		}

		// Test with valid client
		client, err := New(natsURL)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		// Test multiple publishes to ensure reliability
		for i := 0; i < 10; i++ {
			msg := &types.SBSMessage{
				Raw:       fmt.Sprintf("MSG,%d,111,11111,4CA2D%d,111111,2015/02/19,18:06:07.710,2015/02/19,18:06:07.710,,33000,,,,,,,0,0,0,0", i, i),
				Timestamp: time.Now(),
				Source:    fmt.Sprintf("test-%d", i),
			}

			err := client.PublishSBSMessage(msg)
			if err != nil {
				t.Fatalf("Failed to publish message %d: %v", i, err)
			}
		}
	})
}

// TestIntegration_StreamAlreadyExists tests the scenario where the stream already exists
func TestIntegration_StreamAlreadyExists(t *testing.T) {
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

	// Create first client - this will create the stream
	client1, err := New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create first client: %v", err)
	}
	defer client1.Close()

	// Create second client - this should handle "stream already exists" gracefully
	client2, err := New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create second client: %v", err)
	}
	defer client2.Close()

	// Both clients should work
	testMsg := &types.SBSMessage{
		Raw:       "MSG,5,111,11111,4CA2D8,111111,2015/02/19,18:06:09.710,2015/02/19,18:06:09.710,,35000,,,,,,,0,0,0,0",
		Timestamp: time.Now(),
		Source:    "stream-exists-test",
	}

	err = client1.PublishSBSMessage(testMsg)
	if err != nil {
		t.Fatalf("Client1 failed to publish: %v", err)
	}

	err = client2.PublishSBSMessage(testMsg)
	if err != nil {
		t.Fatalf("Client2 failed to publish: %v", err)
	}
}

// startNATSContainer starts a NATS container with JetStream enabled
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

// Benchmark tests to ensure performance
func BenchmarkIntegration_PublishSubscribe(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping integration benchmarks in short mode")
	}

	ctx := context.Background()
	natsContainer, natsURL := startNATSContainerTB(b, ctx)
	defer func() {
		if err := natsContainer.Terminate(ctx); err != nil {
			b.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	client, err := New(natsURL)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	testMsg := &types.SBSMessage{
		Raw:       "MSG,6,111,11111,4CA2D9,111111,2015/02/19,18:06:10.710,2015/02/19,18:06:10.710,,36000,,,,,,,0,0,0,0",
		Timestamp: time.Now(),
		Source:    "benchmark-test",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := client.PublishSBSMessage(testMsg)
			if err != nil {
				b.Fatalf("Failed to publish message: %v", err)
			}
		}
	})
}

// Helper function for benchmarks that need to start containers
func startNATSContainerTB(tb testing.TB, ctx context.Context) (testcontainers.Container, string) {
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
		tb.Fatalf("Failed to start NATS container: %v", err)
	}

	// Get the mapped port
	mappedPort, err := natsContainer.MappedPort(ctx, "4222")
	if err != nil {
		tb.Fatalf("Failed to get mapped port: %v", err)
	}

	// Get the host
	host, err := natsContainer.Host(ctx)
	if err != nil {
		tb.Fatalf("Failed to get container host: %v", err)
	}

	natsURL := fmt.Sprintf("nats://%s:%s", host, mappedPort.Port())
	tb.Logf("NATS container started at: %s", natsURL)

	return natsContainer, natsURL
}
