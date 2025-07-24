package stats

import (
	"context"
	"testing"
	"time"

	"github.com/savio/sbs-logger/internal/db"
)

func TestNew(t *testing.T) {
	stats := New()

	if stats == nil {
		t.Fatal("New() returned nil")
	}

	if stats.TotalMessages != 0 {
		t.Errorf("Expected TotalMessages to be 0, got %d", stats.TotalMessages)
	}

	if stats.ParsedMessages != 0 {
		t.Errorf("Expected ParsedMessages to be 0, got %d", stats.ParsedMessages)
	}

	if stats.FailedMessages != 0 {
		t.Errorf("Expected FailedMessages to be 0, got %d", stats.FailedMessages)
	}

	if time.Since(stats.LastMessageTime) > 5*time.Second {
		t.Error("LastMessageTime should be recent")
	}
}

func TestIncrementTotalMessages(t *testing.T) {
	stats := New()

	initial := stats.TotalMessages
	stats.IncrementTotalMessages()

	if stats.TotalMessages != initial+1 {
		t.Errorf("Expected TotalMessages to be %d, got %d", initial+1, stats.TotalMessages)
	}

	// Test multiple increments
	stats.IncrementTotalMessages()
	stats.IncrementTotalMessages()

	if stats.TotalMessages != initial+3 {
		t.Errorf("Expected TotalMessages to be %d, got %d", initial+3, stats.TotalMessages)
	}
}

func TestIncrementParsedMessages(t *testing.T) {
	stats := New()

	initial := stats.ParsedMessages
	stats.IncrementParsedMessages()

	if stats.ParsedMessages != initial+1 {
		t.Errorf("Expected ParsedMessages to be %d, got %d", initial+1, stats.ParsedMessages)
	}
}

func TestIncrementFailedMessages(t *testing.T) {
	stats := New()

	initial := stats.FailedMessages
	stats.IncrementFailedMessages()

	if stats.FailedMessages != initial+1 {
		t.Errorf("Expected FailedMessages to be %d, got %d", initial+1, stats.FailedMessages)
	}
}

func TestIncrementStoredStates(t *testing.T) {
	stats := New()

	initial := stats.StoredStates
	stats.IncrementStoredStates()

	if stats.StoredStates != initial+1 {
		t.Errorf("Expected StoredStates to be %d, got %d", initial+1, stats.StoredStates)
	}
}

func TestIncrementMessageType(t *testing.T) {
	stats := New()

	// Test valid message types
	stats.IncrementMessageType(0)
	stats.IncrementMessageType(5)
	stats.IncrementMessageType(9)

	if stats.MessageTypeCounts[0] != 1 {
		t.Errorf("Expected MessageTypeCounts[0] to be 1, got %d", stats.MessageTypeCounts[0])
	}

	if stats.MessageTypeCounts[5] != 1 {
		t.Errorf("Expected MessageTypeCounts[5] to be 1, got %d", stats.MessageTypeCounts[5])
	}

	if stats.MessageTypeCounts[9] != 1 {
		t.Errorf("Expected MessageTypeCounts[9] to be 1, got %d", stats.MessageTypeCounts[9])
	}

	// Test invalid message types (should not panic)
	stats.IncrementMessageType(-1)
	stats.IncrementMessageType(10)
	stats.IncrementMessageType(100)
}

func TestIncrementCreatedFlights(t *testing.T) {
	stats := New()

	initial := stats.CreatedFlights
	stats.IncrementCreatedFlights()

	if stats.CreatedFlights != initial+1 {
		t.Errorf("Expected CreatedFlights to be %d, got %d", initial+1, stats.CreatedFlights)
	}
}

func TestIncrementUpdatedFlights(t *testing.T) {
	stats := New()

	initial := stats.UpdatedFlights
	stats.IncrementUpdatedFlights()

	if stats.UpdatedFlights != initial+1 {
		t.Errorf("Expected UpdatedFlights to be %d, got %d", initial+1, stats.UpdatedFlights)
	}
}

func TestIncrementEndedFlights(t *testing.T) {
	stats := New()

	initial := stats.EndedFlights
	stats.IncrementEndedFlights()

	if stats.EndedFlights != initial+1 {
		t.Errorf("Expected EndedFlights to be %d, got %d", initial+1, stats.EndedFlights)
	}
}

func TestSetActiveAircraft(t *testing.T) {
	stats := New()

	stats.SetActiveAircraft(42)

	if stats.ActiveAircraft != 42 {
		t.Errorf("Expected ActiveAircraft to be 42, got %d", stats.ActiveAircraft)
	}

	stats.SetActiveAircraft(100)

	if stats.ActiveAircraft != 100 {
		t.Errorf("Expected ActiveAircraft to be 100, got %d", stats.ActiveAircraft)
	}
}

func TestSetActiveFlights(t *testing.T) {
	stats := New()

	stats.SetActiveFlights(15)

	if stats.ActiveFlights != 15 {
		t.Errorf("Expected ActiveFlights to be 15, got %d", stats.ActiveFlights)
	}

	stats.SetActiveFlights(25)

	if stats.ActiveFlights != 25 {
		t.Errorf("Expected ActiveFlights to be 25, got %d", stats.ActiveFlights)
	}
}

func TestUpdateLastMessageTime(t *testing.T) {
	stats := New()

	oldTime := stats.LastMessageTime
	time.Sleep(10 * time.Millisecond) // Ensure time difference

	stats.UpdateLastMessageTime()

	if !stats.LastMessageTime.After(oldTime) {
		t.Error("LastMessageTime should be updated to a later time")
	}
}

func TestAddProcessingTime(t *testing.T) {
	stats := New()

	initial := stats.ProcessingTime
	duration := 100 * time.Millisecond

	stats.AddProcessingTime(duration)

	if stats.ProcessingTime != initial+duration {
		t.Errorf("Expected ProcessingTime to be %v, got %v", initial+duration, stats.ProcessingTime)
	}

	stats.AddProcessingTime(duration)

	if stats.ProcessingTime != initial+duration+duration {
		t.Errorf("Expected ProcessingTime to be %v, got %v", initial+duration+duration, stats.ProcessingTime)
	}
}

func TestGetStats(t *testing.T) {
	stats := New()

	// Set some values
	stats.IncrementTotalMessages()
	stats.IncrementParsedMessages()
	stats.IncrementFailedMessages()
	stats.IncrementStoredStates()
	stats.IncrementCreatedFlights()
	stats.IncrementUpdatedFlights()
	stats.IncrementEndedFlights()
	stats.SetActiveAircraft(10)
	stats.SetActiveFlights(5)
	stats.IncrementMessageType(1)
	stats.AddProcessingTime(50 * time.Millisecond)

	statsMap := stats.GetStats()

	if statsMap["total_messages"] != uint64(1) {
		t.Errorf("Expected total_messages to be 1, got %v", statsMap["total_messages"])
	}

	if statsMap["parsed_messages"] != uint64(1) {
		t.Errorf("Expected parsed_messages to be 1, got %v", statsMap["parsed_messages"])
	}

	if statsMap["failed_messages"] != uint64(1) {
		t.Errorf("Expected failed_messages to be 1, got %v", statsMap["failed_messages"])
	}

	if statsMap["stored_states"] != uint64(1) {
		t.Errorf("Expected stored_states to be 1, got %v", statsMap["stored_states"])
	}

	if statsMap["created_flights"] != uint64(1) {
		t.Errorf("Expected created_flights to be 1, got %v", statsMap["created_flights"])
	}

	if statsMap["updated_flights"] != uint64(1) {
		t.Errorf("Expected updated_flights to be 1, got %v", statsMap["updated_flights"])
	}

	if statsMap["ended_flights"] != uint64(1) {
		t.Errorf("Expected ended_flights to be 1, got %v", statsMap["ended_flights"])
	}

	if statsMap["active_aircraft"] != uint64(10) {
		t.Errorf("Expected active_aircraft to be 10, got %v", statsMap["active_aircraft"])
	}

	if statsMap["active_flights"] != uint64(5) {
		t.Errorf("Expected active_flights to be 5, got %v", statsMap["active_flights"])
	}

	if statsMap["processing_time"] != 50*time.Millisecond {
		t.Errorf("Expected processing_time to be 50ms, got %v", statsMap["processing_time"])
	}

	// Check that uptime is present
	if _, exists := statsMap["uptime"]; !exists {
		t.Error("Expected uptime to be present in stats")
	}

	// Check that last_message_time is present
	if _, exists := statsMap["last_message_time"]; !exists {
		t.Error("Expected last_message_time to be present in stats")
	}

	// Check that message_types is present
	if _, exists := statsMap["message_types"]; !exists {
		t.Error("Expected message_types to be present in stats")
	}
}

func TestString(t *testing.T) {
	stats := New()

	stats.IncrementTotalMessages()
	stats.IncrementParsedMessages()
	stats.SetActiveAircraft(5)
	stats.SetActiveFlights(3)

	str := stats.String()

	if str == "" {
		t.Error("String() should not return empty string")
	}

	// Check that key statistics are present in the string
	if !contains(str, "Total Messages: 1") {
		t.Error("String should contain 'Total Messages: 1'")
	}

	if !contains(str, "Parsed Messages: 1") {
		t.Error("String should contain 'Parsed Messages: 1'")
	}

	if !contains(str, "Active Aircraft: 5") {
		t.Error("String should contain 'Active Aircraft: 5'")
	}

	if !contains(str, "Active Flights: 3") {
		t.Error("String should contain 'Active Flights: 3'")
	}
}

func TestSetDB(t *testing.T) {
	stats := New()

	if stats.db != nil {
		t.Error("Expected db to be nil initially")
	}

	// Create a mock DB client (we'll just use nil for this test)
	var dbClient *db.Client
	stats.SetDB(dbClient)

	// We can't easily test the private field, but we can test that it doesn't panic
	// and that Persist() returns an error when db is nil
	err := stats.Persist()
	if err == nil {
		t.Error("Persist() should return error when db is not set")
	}
}

func TestPersist_NoDB(t *testing.T) {
	stats := New()

	err := stats.Persist()
	if err == nil {
		t.Error("Persist() should return error when db is not set")
	}

	expectedError := "database client not set"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestStartPersistence_ContextCancellation(t *testing.T) {
	stats := New()

	ctx, cancel := context.WithCancel(context.Background())

	// Start persistence in a goroutine
	go func() {
		stats.StartPersistence(ctx, 100*time.Millisecond)
	}()

	// Cancel the context after a short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Give some time for the goroutine to finish
	time.Sleep(100 * time.Millisecond)
}

func TestStartPersistence_Ticker(t *testing.T) {
	stats := New()

	ctx, cancel := context.WithCancel(context.Background())

	// Start persistence in a goroutine with a very short interval
	go func() {
		stats.StartPersistence(ctx, 10*time.Millisecond)
	}()

	// Let it run for a few ticks
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Give some time for the goroutine to finish
	time.Sleep(50 * time.Millisecond)
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			func() bool {
				for i := 1; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}()))
}
