package capture

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// Message represents a captured SBS message
type Message struct {
	Source    string
	Data      []byte
	Timestamp time.Time
}

// Capture represents a network capture instance
type Capture struct {
	sources  []string
	conns    map[string]net.Conn
	msgChan  chan Message
	wg       sync.WaitGroup
	stopChan chan struct{}
	mu       sync.Mutex
}

// New creates a new Capture instance
func New(sources []string) *Capture {
	return &Capture{
		sources:  sources,
		conns:    make(map[string]net.Conn),
		msgChan:  make(chan Message, 1000), // Buffer size of 1000 messages
		stopChan: make(chan struct{}),
	}
}

// Start begins listening for SBS messages from all sources
func (c *Capture) Start() error {
	for _, source := range c.sources {
		c.wg.Add(1)
		go c.connectToSource(source)
	}
	return nil
}

// handleConnectionError handles connection errors and returns updated state
func (c *Capture) handleConnectionError(connected bool, disconnectTime time.Time, reconnectDelay time.Duration) (bool, time.Time) {
	if connected {
		if disconnectTime.IsZero() {
			disconnectTime = time.Now()
		}
		connected = false
	}
	if !connected && !disconnectTime.IsZero() {
		// Only sleep and retry if we are not connected
		time.Sleep(reconnectDelay)
	}
	return connected, disconnectTime
}

// configureTCPKeepalive configures TCP keepalive settings
func (c *Capture) configureTCPKeepalive(conn net.Conn, source string) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if err := tcpConn.SetKeepAlive(true); err != nil {
			fmt.Printf("Warning: failed to set keepalive for %s: %v\n", source, err)
		}
		if err := tcpConn.SetKeepAlivePeriod(2 * time.Second); err != nil {
			fmt.Printf("Warning: failed to set keepalive period for %s: %v\n", source, err)
		}
		if err := tcpConn.SetNoDelay(true); err != nil {
			fmt.Printf("Warning: failed to set no delay for %s: %v\n", source, err)
		}
	}
}

// handleSuccessfulConnection handles successful connection logic
func (c *Capture) handleSuccessfulConnection(connected bool, disconnectTime time.Time, source string) (bool, time.Time) {
	if !connected {
		if !disconnectTime.IsZero() {
			duration := time.Since(disconnectTime)
			if duration >= 100*time.Millisecond && duration < 10*time.Second {
				fmt.Printf("A connection hiccup of %.1f seconds happened.\n", duration.Seconds())
			} else if duration >= 10*time.Second {
				fmt.Printf("Connection to %s reestablished after %.1f minutes\n", source, duration.Minutes())
			}
			disconnectTime = time.Time{} // Reset disconnect time
		} else {
			fmt.Printf("Successfully connected to %s\n", source)
		}
		connected = true
	}
	return connected, disconnectTime
}

// Stop gracefully stops the capture
func (c *Capture) Stop() {
	close(c.stopChan)
	c.mu.Lock()
	for _, conn := range c.conns {
		conn.Close()
	}
	c.mu.Unlock()
	c.wg.Wait()
	close(c.msgChan)
}

// Messages returns the channel for receiving messages
func (c *Capture) Messages() <-chan Message {
	return c.msgChan
}

func (c *Capture) connectToSource(source string) {
	defer c.wg.Done()

	reconnectDelay := 5 * time.Second
	connected := false
	var disconnectTime time.Time
	firstConnection := true

	for {
		select {
		case <-c.stopChan:
			return
		default:
			if firstConnection {
				fmt.Printf("Attempting to connect to %s...\n", source)
				firstConnection = false
			}

			conn, err := net.Dial("tcp", source)
			if err != nil {
				connected, disconnectTime = c.handleConnectionError(connected, disconnectTime, reconnectDelay)
				continue
			}

			// Configure TCP keepalive
			c.configureTCPKeepalive(conn, source)

			connected, disconnectTime = c.handleSuccessfulConnection(connected, disconnectTime, source)

			c.mu.Lock()
			c.conns[source] = conn
			c.mu.Unlock()

			c.handleConnection(source, conn)

			// If we get here, the connection was closed
			c.mu.Lock()
			delete(c.conns, source)
			c.mu.Unlock()

			if connected {
				disconnectTime = time.Now()
				connected = false
			}
		}
	}
}

func (c *Capture) handleConnection(source string, conn net.Conn) {
	defer conn.Close()

	buffer := make([]byte, 1024)
	lastMessageTime := time.Now()

	for {
		select {
		case <-c.stopChan:
			return
		default:
			// Set a read deadline of 2 seconds
			if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				fmt.Printf("Warning: failed to set read deadline for %s: %v\n", source, err)
			}

			n, err := conn.Read(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Check if we've gone too long without a message
					if time.Since(lastMessageTime) > 3*time.Second {
						return
					}
					// Reset the deadline and continue
					if err := conn.SetReadDeadline(time.Time{}); err != nil {
						fmt.Printf("Warning: failed to reset read deadline for %s: %v\n", source, err)
					}
					continue
				}
				return
			}

			// Reset the read deadline
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				fmt.Printf("Warning: failed to reset read deadline for %s: %v\n", source, err)
			}

			// Update last message time
			lastMessageTime = time.Now()

			// Create a copy of the data to avoid buffer reuse issues
			data := make([]byte, n)
			copy(data, buffer[:n])

			// Send the message through the channel
			select {
			case c.msgChan <- Message{
				Source:    source,
				Data:      data,
				Timestamp: time.Now(),
			}:
			case <-c.stopChan:
				return
			}
		}
	}
}
