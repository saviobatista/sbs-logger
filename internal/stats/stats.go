package stats

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/savio/sbs-logger/internal/db"
)

// Stats tracks message processing statistics
type Stats struct {
	// Message counts
	TotalMessages     uint64
	ParsedMessages    uint64
	FailedMessages    uint64
	StoredStates      uint64
	CreatedFlights    uint64
	UpdatedFlights    uint64
	EndedFlights      uint64

	// Message type counts
	MessageTypeCounts [10]uint64 // Index corresponds to message type

	// Timing
	LastMessageTime time.Time
	ProcessingTime  time.Duration

	// Active tracking
	ActiveAircraft uint64
	ActiveFlights  uint64

	// Database client for persistence
	db *db.Client

	mu sync.RWMutex
}

// New creates a new Stats instance
func New() *Stats {
	return &Stats{
		LastMessageTime: time.Now(),
	}
}

// SetDB sets the database client for persistence
func (s *Stats) SetDB(db *db.Client) {
	s.mu.Lock()
	s.db = db
	s.mu.Unlock()
}

// Persist stores the current statistics in the database
func (s *Stats) Persist() error {
	s.mu.RLock()
	if s.db == nil {
		s.mu.RUnlock()
		return fmt.Errorf("database client not set")
	}
	s.mu.RUnlock()

	stats := s.GetStats()
	return s.db.StoreSystemStats(stats)
}

// IncrementTotalMessages increments the total messages counter
func (s *Stats) IncrementTotalMessages() {
	atomic.AddUint64(&s.TotalMessages, 1)
}

// IncrementParsedMessages increments the parsed messages counter
func (s *Stats) IncrementParsedMessages() {
	atomic.AddUint64(&s.ParsedMessages, 1)
}

// IncrementFailedMessages increments the failed messages counter
func (s *Stats) IncrementFailedMessages() {
	atomic.AddUint64(&s.FailedMessages, 1)
}

// IncrementStoredStates increments the stored states counter
func (s *Stats) IncrementStoredStates() {
	atomic.AddUint64(&s.StoredStates, 1)
}

// IncrementMessageType increments the counter for a specific message type
func (s *Stats) IncrementMessageType(msgType int) {
	if msgType >= 0 && msgType < len(s.MessageTypeCounts) {
		atomic.AddUint64(&s.MessageTypeCounts[msgType], 1)
	}
}

// IncrementCreatedFlights increments the created flights counter
func (s *Stats) IncrementCreatedFlights() {
	atomic.AddUint64(&s.CreatedFlights, 1)
}

// IncrementUpdatedFlights increments the updated flights counter
func (s *Stats) IncrementUpdatedFlights() {
	atomic.AddUint64(&s.UpdatedFlights, 1)
}

// IncrementEndedFlights increments the ended flights counter
func (s *Stats) IncrementEndedFlights() {
	atomic.AddUint64(&s.EndedFlights, 1)
}

// SetActiveAircraft sets the number of active aircraft
func (s *Stats) SetActiveAircraft(count uint64) {
	atomic.StoreUint64(&s.ActiveAircraft, count)
}

// SetActiveFlights sets the number of active flights
func (s *Stats) SetActiveFlights(count uint64) {
	atomic.StoreUint64(&s.ActiveFlights, count)
}

// UpdateLastMessageTime updates the last message time
func (s *Stats) UpdateLastMessageTime() {
	s.mu.Lock()
	s.LastMessageTime = time.Now()
	s.mu.Unlock()
}

// AddProcessingTime adds to the total processing time
func (s *Stats) AddProcessingTime(duration time.Duration) {
	s.mu.Lock()
	s.ProcessingTime += duration
	s.mu.Unlock()
}

// GetStats returns a copy of the current statistics
func (s *Stats) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"total_messages":     atomic.LoadUint64(&s.TotalMessages),
		"parsed_messages":    atomic.LoadUint64(&s.ParsedMessages),
		"failed_messages":    atomic.LoadUint64(&s.FailedMessages),
		"stored_states":      atomic.LoadUint64(&s.StoredStates),
		"created_flights":    atomic.LoadUint64(&s.CreatedFlights),
		"updated_flights":    atomic.LoadUint64(&s.UpdatedFlights),
		"ended_flights":      atomic.LoadUint64(&s.EndedFlights),
		"active_aircraft":    atomic.LoadUint64(&s.ActiveAircraft),
		"active_flights":     atomic.LoadUint64(&s.ActiveFlights),
		"message_types":      s.MessageTypeCounts,
		"last_message_time":  s.LastMessageTime,
		"processing_time":    s.ProcessingTime,
		"uptime":            time.Since(s.LastMessageTime),
	}
}

// String returns a string representation of the statistics
func (s *Stats) String() string {
	stats := s.GetStats()
	return fmt.Sprintf(
		"Total Messages: %d\n"+
			"Parsed Messages: %d\n"+
			"Failed Messages: %d\n"+
			"Stored States: %d\n"+
			"Created Flights: %d\n"+
			"Updated Flights: %d\n"+
			"Ended Flights: %d\n"+
			"Active Aircraft: %d\n"+
			"Active Flights: %d\n"+
			"Last Message Time: %s\n"+
			"Processing Time: %s\n"+
			"Uptime: %s",
		stats["total_messages"],
		stats["parsed_messages"],
		stats["failed_messages"],
		stats["stored_states"],
		stats["created_flights"],
		stats["updated_flights"],
		stats["ended_flights"],
		stats["active_aircraft"],
		stats["active_flights"],
		stats["last_message_time"],
		stats["processing_time"],
		stats["uptime"],
	)
}

// StartPersistence starts periodic persistence of statistics
func (s *Stats) StartPersistence(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final persistence before shutdown
			if err := s.Persist(); err != nil {
				fmt.Printf("Failed to persist final statistics: %v\n", err)
			}
			return
		case <-ticker.C:
			if err := s.Persist(); err != nil {
				fmt.Printf("Failed to persist statistics: %v\n", err)
			}
		}
	}
} 