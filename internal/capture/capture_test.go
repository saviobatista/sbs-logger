package capture

import (
	"net"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	sources := []string{"localhost:30003", "localhost:30004"}
	capture := New(sources)
	
	if capture == nil {
		t.Fatal("New() returned nil")
	}
	
	if len(capture.sources) != 2 {
		t.Errorf("Expected 2 sources, got %d", len(capture.sources))
	}
	
	if capture.sources[0] != "localhost:30003" {
		t.Errorf("Expected first source to be localhost:30003, got %s", capture.sources[0])
	}
	
	if capture.sources[1] != "localhost:30004" {
		t.Errorf("Expected second source to be localhost:30004, got %s", capture.sources[1])
	}
	
	if capture.conns == nil {
		t.Error("Expected conns map to be initialized")
	}
	
	if capture.msgChan == nil {
		t.Error("Expected msgChan to be initialized")
	}
	
	if capture.stopChan == nil {
		t.Error("Expected stopChan to be initialized")
	}
}

func TestNew_EmptySources(t *testing.T) {
	capture := New([]string{})
	
	if capture == nil {
		t.Fatal("New() returned nil")
	}
	
	if len(capture.sources) != 0 {
		t.Errorf("Expected 0 sources, got %d", len(capture.sources))
	}
}

func TestCapture_Start(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	err := capture.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	
	// Stop immediately to clean up
	capture.Stop()
}

func TestCapture_StartMultipleSources(t *testing.T) {
	sources := []string{"localhost:30003", "localhost:30004", "localhost:30005"}
	capture := New(sources)
	
	err := capture.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	
	// Stop immediately to clean up
	capture.Stop()
}

func TestCapture_Messages(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	msgChan := capture.Messages()
	if msgChan == nil {
		t.Fatal("Messages() returned nil channel")
	}
	
	// The channel should be the same as the internal one
	if msgChan != capture.msgChan {
		t.Error("Messages() should return the internal message channel")
	}
}

func TestCapture_Stop(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	// Start the capture
	err := capture.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	
	// Stop should not panic
	capture.Stop()
}

func TestCapture_StopWithoutStart(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	// Stop without starting should not panic
	capture.Stop()
}

func TestCapture_HandleConnectionError(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	// Test when connected becomes false
	connected := true
	disconnectTime := time.Time{}
	reconnectDelay := 5 * time.Second
	
	connected, disconnectTime = capture.handleConnectionError(connected, disconnectTime, reconnectDelay)
	
	if connected {
		t.Error("Expected connected to be false")
	}
	
	if disconnectTime.IsZero() {
		t.Error("Expected disconnectTime to be set")
	}
	
	// Test when already disconnected
	connected = false
	disconnectTime = time.Time{}
	
	connected, disconnectTime = capture.handleConnectionError(connected, disconnectTime, reconnectDelay)
	
	if connected {
		t.Error("Expected connected to remain false")
	}
	
	if !disconnectTime.IsZero() {
		t.Error("Expected disconnectTime to remain zero")
	}
}

func TestCapture_HandleSuccessfulConnection(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	// Test when not connected and no previous disconnect
	connected := false
	disconnectTime := time.Time{}
	source := "test-source"
	
	connected, disconnectTime = capture.handleSuccessfulConnection(connected, disconnectTime, source)
	
	if !connected {
		t.Error("Expected connected to be true")
	}
	
	if !disconnectTime.IsZero() {
		t.Error("Expected disconnectTime to remain zero")
	}
	
	// Test when not connected and with previous disconnect (short duration)
	connected = false
	disconnectTime = time.Now().Add(-500 * time.Millisecond) // 500ms ago
	
	connected, disconnectTime = capture.handleSuccessfulConnection(connected, disconnectTime, source)
	
	if !connected {
		t.Error("Expected connected to be true")
	}
	
	if !disconnectTime.IsZero() {
		t.Error("Expected disconnectTime to be reset")
	}
	
	// Test when not connected and with previous disconnect (long duration)
	connected = false
	disconnectTime = time.Now().Add(-15 * time.Second) // 15 seconds ago
	
	connected, disconnectTime = capture.handleSuccessfulConnection(connected, disconnectTime, source)
	
	if !connected {
		t.Error("Expected connected to be true")
	}
	
	if !disconnectTime.IsZero() {
		t.Error("Expected disconnectTime to be reset")
	}
	
	// Test when already connected
	connected = true
	disconnectTime = time.Time{}
	
	connected, disconnectTime = capture.handleSuccessfulConnection(connected, disconnectTime, source)
	
	if !connected {
		t.Error("Expected connected to remain true")
	}
	
	if !disconnectTime.IsZero() {
		t.Error("Expected disconnectTime to remain zero")
	}
}

func TestCapture_ConfigureTCPKeepalive(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	// Create a mock TCP connection
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()
	
	// Connect to the listener
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()
	
	// Test configureTCPKeepalive
	capture.configureTCPKeepalive(conn, "test-source")
	
	// The function should not panic and should complete successfully
}

func TestCapture_ConfigureTCPKeepaliveNonTCP(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	// Create a mock non-TCP connection using a custom type
	type mockConn struct {
		net.Conn
	}
	
	// Create a TCP listener to get a real connection, then wrap it
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()
	
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()
	
	// Wrap the connection to make it non-TCP for testing
	mockConnection := &mockConn{conn}
	
	// Test configureTCPKeepalive with non-TCP connection
	capture.configureTCPKeepalive(mockConnection, "test-source")
	
	// The function should not panic and should complete successfully
}

func TestMessage_Structure(t *testing.T) {
	// Test Message struct creation
	msg := Message{
		Source:    "test-source",
		Data:      []byte("test data"),
		Timestamp: time.Now(),
	}
	
	if msg.Source != "test-source" {
		t.Errorf("Expected Source to be 'test-source', got %s", msg.Source)
	}
	
	if string(msg.Data) != "test data" {
		t.Errorf("Expected Data to be 'test data', got %s", string(msg.Data))
	}
	
	if msg.Timestamp.IsZero() {
		t.Error("Expected Timestamp to be set")
	}
}

func TestCapture_ConcurrentAccess(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	// Test concurrent access to the capture
	done := make(chan bool, 2)
	
	go func() {
		capture.Start()
		done <- true
	}()
	
	go func() {
		time.Sleep(100 * time.Millisecond)
		capture.Stop()
		done <- true
	}()
	
	// Wait for both goroutines to complete
	<-done
	<-done
}

func TestCapture_MessageChannelBuffer(t *testing.T) {
	sources := []string{"localhost:30003"}
	capture := New(sources)
	
	// Test that the message channel has a reasonable buffer size
	// The buffer size is set to 1000 in the New function
	// We can't directly test sending to the receive-only channel, but we can test the internal channel
	msgChan := capture.msgChan
	
	// Try to send messages up to the buffer limit
	for i := 0; i < 1000; i++ {
		select {
		case msgChan <- Message{
			Source:    "test",
			Data:      []byte("test"),
			Timestamp: time.Now(),
		}:
			// Message sent successfully
		default:
			t.Errorf("Message channel buffer should be able to hold at least %d messages", i+1)
			return
		}
	}
	
	// Clean up
	capture.Stop()
}

func TestCapture_EmptySourcesStart(t *testing.T) {
	capture := New([]string{})
	
	err := capture.Start()
	if err != nil {
		t.Fatalf("Start() with empty sources should not fail: %v", err)
	}
	
	capture.Stop()
} 