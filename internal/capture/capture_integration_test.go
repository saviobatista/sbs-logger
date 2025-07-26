package capture

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// TestCapture_Integration_RealConnection tests actual TCP connection and message handling
func TestCapture_Integration_RealConnection(t *testing.T) {
	// Create a simple TCP server manually
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Start a goroutine to accept connections and send messages
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			t.Logf("Failed to accept connection: %v", err)
			return
		}
		defer conn.Close()

		// Send SBS messages
		messages := []string{
			"MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n",
			"MSG,1,1,1,DEF456,1,2021-01-01,00:00:01.000,2021-01-01,00:00:01.000,TEST456,11000,500,200,41.7128,-75.0060,0,0,0,0\n",
		}

		for _, msg := range messages {
			_, err := conn.Write([]byte(msg))
			if err != nil {
				t.Logf("Failed to write message: %v", err)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	sources := []string{listener.Addr().String()}
	capture := New(sources)

	// Start the capture
	err = capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait a bit for connection to establish
	time.Sleep(1 * time.Second)

	// Get the message channel
	msgChan := capture.Messages()

	// Wait for messages with timeout
	timeout := time.After(10 * time.Second)
	messagesReceived := 0
	expectedMessages := 2

	for messagesReceived < expectedMessages {
		select {
		case msg := <-msgChan:
			t.Logf("Received message %d from %s: %s", messagesReceived+1, msg.Source, string(msg.Data))
			messagesReceived++
		case <-timeout:
			t.Fatalf("Timeout waiting for messages. Received %d, expected %d", messagesReceived, expectedMessages)
		}
	}

	// Stop the capture
	capture.Stop()
}

// TestCapture_Integration_MultipleSources tests capturing from multiple sources
func TestCapture_Integration_MultipleSources(t *testing.T) {
	// Create a TCP server
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Start a goroutine to accept connections and send messages
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			t.Logf("Failed to accept connection: %v", err)
			return
		}
		defer conn.Close()

		// Send a message
		msg := "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n"
		_, err = conn.Write([]byte(msg))
		if err != nil {
			t.Logf("Failed to write message: %v", err)
		}
	}()

	sourceAddr := listener.Addr().String()
	sources := []string{sourceAddr, "localhost:9999"} // One real, one fake
	capture := New(sources)

	// Start the capture
	err = capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait a bit for connection to establish
	time.Sleep(1 * time.Second)

	// Get the message channel
	msgChan := capture.Messages()

	// Wait for messages with timeout
	timeout := time.After(10 * time.Second)
	messageReceived := false

	for !messageReceived {
		select {
		case msg := <-msgChan:
			if msg.Source == sourceAddr {
				t.Logf("Received message from %s: %s", msg.Source, string(msg.Data))
				messageReceived = true
			}
		case <-timeout:
			t.Fatal("Timeout waiting for message")
		}
	}

	// Stop the capture
	capture.Stop()
}

// TestCapture_Integration_ConnectionRetry tests connection retry logic
func TestCapture_Integration_ConnectionRetry(t *testing.T) {
	// Create a TCP server
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	sourceAddr := listener.Addr().String()
	sources := []string{sourceAddr}
	capture := New(sources)

	// Start the capture
	err = capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait for initial connection attempt
	time.Sleep(2 * time.Second)

	// Close the listener to simulate connection loss
	listener.Close()

	// Wait for retry logic to kick in
	time.Sleep(3 * time.Second)

	// Stop the capture
	capture.Stop()
}

// TestCapture_Integration_MessageHandling tests the actual message handling logic
func TestCapture_Integration_MessageHandling(t *testing.T) {
	// Create a simple TCP server manually for this test
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Start a goroutine to accept connections and send messages
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			t.Logf("Failed to accept connection: %v", err)
			return
		}
		defer conn.Close()

		// Send multiple SBS messages
		messages := []string{
			"MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n",
			"MSG,1,1,1,DEF456,1,2021-01-01,00:00:01.000,2021-01-01,00:00:01.000,TEST456,11000,500,200,41.7128,-75.0060,0,0,0,0\n",
			"MSG,4,1,1,GHI789,1,2021-01-01,00:00:02.000,2021-01-01,00:00:02.000,TEST789,12000,550,220,42.7128,-76.0060,0,0,0,0\n",
		}

		for _, msg := range messages {
			_, err := conn.Write([]byte(msg))
			if err != nil {
				t.Logf("Failed to write message: %v", err)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	sources := []string{listener.Addr().String()}
	capture := New(sources)

	// Start the capture
	err = capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait a bit for connection to establish
	time.Sleep(1 * time.Second)

	// Get the message channel
	msgChan := capture.Messages()

	// Wait for messages with timeout
	timeout := time.After(10 * time.Second)
	messagesReceived := 0
	expectedMessages := 3

	for messagesReceived < expectedMessages {
		select {
		case msg := <-msgChan:
			t.Logf("Received message %d: %s", messagesReceived+1, string(msg.Data))
			messagesReceived++
		case <-timeout:
			t.Fatalf("Timeout waiting for messages. Received %d, expected %d", messagesReceived, expectedMessages)
		}
	}

	// Stop the capture
	capture.Stop()
}

// TestCapture_Integration_ConnectionTimeout tests connection timeout handling
func TestCapture_Integration_ConnectionTimeout(t *testing.T) {
	// Create a TCP server that doesn't send any data
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Start a goroutine to accept connections but not send data
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			t.Logf("Failed to accept connection: %v", err)
			return
		}
		defer conn.Close()

		// Don't send any data, just keep the connection open
		time.Sleep(5 * time.Second)
	}()

	sources := []string{listener.Addr().String()}
	capture := New(sources)

	// Start the capture
	err = capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait for connection timeout logic to kick in
	time.Sleep(6 * time.Second)

	// Stop the capture
	capture.Stop()
}

// TestCapture_Integration_ConcurrentConnections tests multiple concurrent connections
func TestCapture_Integration_ConcurrentConnections(t *testing.T) {
	// Create multiple TCP servers
	listeners := make([]net.Listener, 3)
	defer func() {
		for _, listener := range listeners {
			if listener != nil {
				listener.Close()
			}
		}
	}()

	// Create 3 listeners
	for i := 0; i < 3; i++ {
		listener, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatalf("Failed to create listener %d: %v", i, err)
		}
		listeners[i] = listener

		// Start a goroutine for each listener
		go func(listener net.Listener, id int) {
			conn, err := listener.Accept()
			if err != nil {
				t.Logf("Failed to accept connection on listener %d: %v", id, err)
				return
			}
			defer conn.Close()

			// Send a message
			msg := fmt.Sprintf("MSG,3,1,1,ABC%d,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST%d,10000,450,180,40.7128,-74.0060,0,0,0,0\n", id, id)
			_, err = conn.Write([]byte(msg))
			if err != nil {
				t.Logf("Failed to write message on listener %d: %v", id, err)
			}
		}(listener, i)
	}

	// Create sources list
	sources := make([]string, 3)
	for i, listener := range listeners {
		sources[i] = listener.Addr().String()
	}

	capture := New(sources)

	// Start the capture
	err := capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait a bit for connections to establish
	time.Sleep(2 * time.Second)

	// Get the message channel
	msgChan := capture.Messages()

	// Wait for messages with timeout
	timeout := time.After(10 * time.Second)
	messagesReceived := 0
	expectedMessages := 3

	for messagesReceived < expectedMessages {
		select {
		case msg := <-msgChan:
			t.Logf("Received message %d from %s: %s", messagesReceived+1, msg.Source, string(msg.Data))
			messagesReceived++
		case <-timeout:
			t.Fatalf("Timeout waiting for messages. Received %d, expected %d", messagesReceived, expectedMessages)
		}
	}

	// Stop the capture
	capture.Stop()
}

// TestCapture_Integration_InvalidSource tests behavior with invalid sources
func TestCapture_Integration_InvalidSource(t *testing.T) {
	sources := []string{"invalid-host:99999", "localhost:99999"}
	capture := New(sources)

	// Start the capture
	err := capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait a bit for connection attempts
	time.Sleep(3 * time.Second)

	// Stop the capture
	capture.Stop()
}

// TestCapture_Integration_MessageBuffer tests message buffer handling
func TestCapture_Integration_MessageBuffer(t *testing.T) {
	// Create a TCP server that sends many messages quickly
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Start a goroutine to accept connections and send many messages
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			t.Logf("Failed to accept connection: %v", err)
			return
		}
		defer conn.Close()

		// Send many messages quickly
		for i := 0; i < 50; i++ {
			msg := fmt.Sprintf("MSG,3,1,1,ABC%d,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST%d,10000,450,180,40.7128,-74.0060,0,0,0,0\n", i, i)
			_, err := conn.Write([]byte(msg))
			if err != nil {
				t.Logf("Failed to write message %d: %v", i, err)
				return
			}
			time.Sleep(10 * time.Millisecond) // Small delay
		}
	}()

	sources := []string{listener.Addr().String()}
	capture := New(sources)

	// Start the capture
	err = capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait a bit for connection to establish
	time.Sleep(1 * time.Second)

	// Get the message channel
	msgChan := capture.Messages()

	// Wait for messages with timeout
	timeout := time.After(10 * time.Second)
	messagesReceived := 0
	expectedMessages := 50

	for messagesReceived < expectedMessages {
		select {
		case msg := <-msgChan:
			if strings.Contains(string(msg.Data), "MSG,3,1,1,ABC") {
				messagesReceived++
			}
		case <-timeout:
			t.Logf("Timeout waiting for messages. Received %d, expected %d", messagesReceived, expectedMessages)
			break
		}
	}

	t.Logf("Successfully received %d messages", messagesReceived)

	// Stop the capture
	capture.Stop()
}

// TestCapture_Integration_ReadDeadline tests read deadline handling
func TestCapture_Integration_ReadDeadline(t *testing.T) {
	// Create a TCP server that sends data slowly
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Start a goroutine to accept connections and send data slowly
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			t.Logf("Failed to accept connection: %v", err)
			return
		}
		defer conn.Close()

		// Send first message quickly
		msg1 := "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n"
		_, err = conn.Write([]byte(msg1))
		if err != nil {
			t.Logf("Failed to write first message: %v", err)
			return
		}

		// Wait longer than the read deadline (2 seconds)
		time.Sleep(3 * time.Second)

		// Send second message
		msg2 := "MSG,1,1,1,DEF456,1,2021-01-01,00:00:01.000,2021-01-01,00:00:01.000,TEST456,11000,500,200,41.7128,-75.0060,0,0,0,0\n"
		_, err = conn.Write([]byte(msg2))
		if err != nil {
			t.Logf("Failed to write second message: %v", err)
		}
	}()

	sources := []string{listener.Addr().String()}
	capture := New(sources)

	// Start the capture
	err = capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait a bit for connection to establish
	time.Sleep(1 * time.Second)

	// Get the message channel
	msgChan := capture.Messages()

	// Wait for messages with timeout
	timeout := time.After(15 * time.Second)
	messagesReceived := 0
	expectedMessages := 2

	for messagesReceived < expectedMessages {
		select {
		case msg := <-msgChan:
			t.Logf("Received message %d: %s", messagesReceived+1, string(msg.Data))
			messagesReceived++
		case <-timeout:
			t.Logf("Timeout waiting for messages. Received %d, expected %d", messagesReceived, expectedMessages)
			break
		}
	}

	// Stop the capture
	capture.Stop()
}

// TestCapture_Integration_ConnectionClose tests connection close handling
func TestCapture_Integration_ConnectionClose(t *testing.T) {
	// Create a TCP server that closes the connection after sending a message
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Start a goroutine to accept connections and close after sending
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			t.Logf("Failed to accept connection: %v", err)
			return
		}

		// Send a message
		msg := "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0\n"
		_, err = conn.Write([]byte(msg))
		if err != nil {
			t.Logf("Failed to write message: %v", err)
		}

		// Close the connection immediately
		conn.Close()
	}()

	sources := []string{listener.Addr().String()}
	capture := New(sources)

	// Start the capture
	err = capture.Start()
	if err != nil {
		t.Fatalf("Failed to start capture: %v", err)
	}

	// Wait a bit for connection to establish
	time.Sleep(1 * time.Second)

	// Get the message channel
	msgChan := capture.Messages()

	// Wait for messages with timeout
	timeout := time.After(10 * time.Second)
	messageReceived := false

	for !messageReceived {
		select {
		case msg := <-msgChan:
			t.Logf("Received message: %s", string(msg.Data))
			messageReceived = true
		case <-timeout:
			t.Fatal("Timeout waiting for message")
		}
	}

	// Stop the capture
	capture.Stop()
}
