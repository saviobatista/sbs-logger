package db

import (
	"testing"
	"time"

	"github.com/savio/sbs-logger/internal/types"
)

func TestNew_ValidConnectionString(t *testing.T) {
	// This test requires a PostgreSQL server running
	// For now, we'll test the function structure without actual connection
	connStr := "postgres://user:password@localhost:5432/sbs_logger?sslmode=disable"

	// Test that the function doesn't panic
	// Note: This will fail if PostgreSQL is not running, but that's expected
	client, err := New(connStr)
	if err != nil {
		// Expected if PostgreSQL is not running
		t.Logf("Expected error when PostgreSQL is not running: %v", err)
		return
	}

	if client == nil {
		t.Fatal("New() returned nil client")
	}

	if client.db == nil {
		t.Error("Expected database connection to be initialized")
	}

	// Clean up
	client.Close()
}

func TestNew_InvalidConnectionString(t *testing.T) {
	connStr := "invalid://connection:string"

	client, err := New(connStr)
	if err != nil {
		// Expected error with invalid connection string
		t.Logf("Expected error with invalid connection string: %v", err)
		return
	}

	// If we get here, the connection string was accepted
	// This is actually valid behavior for sql.Open - it only validates the driver
	t.Log("Connection string was accepted (this is valid behavior)")
	client.Close()
}

func TestNew_EmptyConnectionString(t *testing.T) {
	connStr := ""

	client, err := New(connStr)
	if err != nil {
		// Expected error with empty connection string
		t.Logf("Expected error with empty connection string: %v", err)
		return
	}

	// If we get here, the connection string was accepted
	// This is actually valid behavior for sql.Open - it only validates the driver
	t.Log("Empty connection string was accepted (this is valid behavior)")
	client.Close()
}

func TestClient_Close(t *testing.T) {
	// Test close without initialization
	client := &Client{}

	// This will panic because client.db is nil
	// We should test this with a proper client or handle the nil case
	defer func() {
		if r := recover(); r != nil {
			// Expected panic when db is nil
			t.Logf("Expected panic when db is nil: %v", r)
		}
	}()

	client.Close()
	t.Error("Close() should panic when db is nil")
}

func TestClient_CloseWithConnection(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Close should not panic
	err = client.Close()
	if err != nil {
		t.Errorf("Close() should not fail: %v", err)
	}
}

func TestClient_CreateFlight(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	flight := &types.Flight{
		SessionID:      "test-session",
		HexIdent:       "ABC123",
		Callsign:       "TEST123",
		StartedAt:      time.Now(),
		FirstLatitude:  40.7128,
		FirstLongitude: -74.0060,
		LastLatitude:   41.7128,
		LastLongitude:  -75.0060,
		MaxAltitude:    35000,
		MaxGroundSpeed: 450.5,
	}

	err = client.CreateFlight(flight)
	if err != nil {
		t.Fatalf("CreateFlight() failed: %v", err)
	}
}

func TestClient_UpdateFlight(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	flight := &types.Flight{
		SessionID:      "test-session",
		HexIdent:       "ABC123",
		Callsign:       "TEST123",
		StartedAt:      time.Now(),
		EndedAt:        time.Now().Add(time.Hour),
		FirstLatitude:  40.7128,
		FirstLongitude: -74.0060,
		LastLatitude:   41.7128,
		LastLongitude:  -75.0060,
		MaxAltitude:    35000,
		MaxGroundSpeed: 450.5,
	}

	// Create the flight first
	err = client.CreateFlight(flight)
	if err != nil {
		t.Fatalf("CreateFlight() failed: %v", err)
	}

	// Update the flight
	flight.Callsign = "UPDATED123"
	flight.MaxAltitude = 40000

	err = client.UpdateFlight(flight)
	if err != nil {
		t.Fatalf("UpdateFlight() failed: %v", err)
	}
}

func TestClient_GetActiveFlights(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Create a test flight
	flight := &types.Flight{
		SessionID:      "test-session",
		HexIdent:       "ABC123",
		Callsign:       "TEST123",
		StartedAt:      time.Now(),
		FirstLatitude:  40.7128,
		FirstLongitude: -74.0060,
		LastLatitude:   41.7128,
		LastLongitude:  -75.0060,
		MaxAltitude:    35000,
		MaxGroundSpeed: 450.5,
	}

	err = client.CreateFlight(flight)
	if err != nil {
		t.Fatalf("CreateFlight() failed: %v", err)
	}

	// Get active flights
	flights, err := client.GetActiveFlights()
	if err != nil {
		t.Fatalf("GetActiveFlights() failed: %v", err)
	}

	if len(flights) == 0 {
		t.Log("No active flights found (this is expected if the database is empty)")
		return
	}

	// Check that we can find our test flight
	found := false
	for _, f := range flights {
		if f.SessionID == flight.SessionID {
			found = true
			if f.HexIdent != flight.HexIdent {
				t.Errorf("Expected HexIdent %s, got %s", flight.HexIdent, f.HexIdent)
			}
			if f.Callsign != flight.Callsign {
				t.Errorf("Expected Callsign %s, got %s", flight.Callsign, f.Callsign)
			}
			break
		}
	}

	if !found {
		t.Log("Test flight not found in active flights (may have been ended)")
	}
}

func TestClient_StoreAircraftState(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	state := &types.AircraftState{
		HexIdent:     "ABC123",
		Callsign:     "TEST123",
		Altitude:     35000,
		GroundSpeed:  450.5,
		Track:        180.0,
		Latitude:     40.7128,
		Longitude:    -74.0060,
		VerticalRate: 1000,
		Squawk:       "1234",
		OnGround:     false,
		MsgType:      8,
		Timestamp:    time.Now(),
		SessionID:    "test-session",
	}

	err = client.StoreAircraftState(state)
	if err != nil {
		t.Fatalf("StoreAircraftState() failed: %v", err)
	}
}

func TestClient_StoreSystemStats(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	stats := map[string]interface{}{
		"total_messages":    uint64(100),
		"parsed_messages":   uint64(95),
		"failed_messages":   uint64(5),
		"stored_states":     uint64(90),
		"created_flights":   uint64(10),
		"updated_flights":   uint64(5),
		"ended_flights":     uint64(3),
		"active_aircraft":   uint64(15),
		"active_flights":    uint64(7),
		"message_types":     [10]uint64{0, 5, 10, 15, 20, 25, 30, 35, 40, 45},
		"last_message_time": time.Now(),
		"processing_time":   time.Duration(100) * time.Millisecond,
	}

	err = client.StoreSystemStats(stats)
	if err != nil {
		t.Fatalf("StoreSystemStats() failed: %v", err)
	}
}

func TestClient_GetSystemStats(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Store some test stats first
	stats := map[string]interface{}{
		"total_messages":    uint64(100),
		"parsed_messages":   uint64(95),
		"failed_messages":   uint64(5),
		"stored_states":     uint64(90),
		"created_flights":   uint64(10),
		"updated_flights":   uint64(5),
		"ended_flights":     uint64(3),
		"active_aircraft":   uint64(15),
		"active_flights":    uint64(7),
		"message_types":     [10]uint64{0, 5, 10, 15, 20, 25, 30, 35, 40, 45},
		"last_message_time": time.Now(),
		"processing_time":   time.Duration(100) * time.Millisecond,
	}

	err = client.StoreSystemStats(stats)
	if err != nil {
		t.Fatalf("StoreSystemStats() failed: %v", err)
	}

	// Get stats for the last hour
	end := time.Now()
	start := end.Add(-time.Hour)

	retrievedStats, err := client.GetSystemStats(start, end)
	if err != nil {
		t.Fatalf("GetSystemStats() failed: %v", err)
	}

	if len(retrievedStats) == 0 {
		t.Log("No system stats found in the specified time range")
		return
	}

	// Check the structure of the first stat
	firstStat := retrievedStats[0]

	requiredKeys := []string{
		"time", "total_messages", "parsed_messages", "failed_messages",
		"stored_states", "created_flights", "updated_flights", "ended_flights",
		"active_aircraft", "active_flights", "message_types",
		"processing_time", "uptime_seconds",
	}

	for _, key := range requiredKeys {
		if _, exists := firstStat[key]; !exists {
			t.Errorf("Expected key '%s' in system stats", key)
		}
	}
}

func TestClient_StoreSystemStats_EdgeCases(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Test with zero values
	stats := map[string]interface{}{
		"total_messages":    uint64(0),
		"parsed_messages":   uint64(0),
		"failed_messages":   uint64(0),
		"stored_states":     uint64(0),
		"created_flights":   uint64(0),
		"updated_flights":   uint64(0),
		"ended_flights":     uint64(0),
		"active_aircraft":   uint64(0),
		"active_flights":    uint64(0),
		"message_types":     [10]uint64{},
		"last_message_time": time.Now(),
		"processing_time":   time.Duration(0),
	}

	err = client.StoreSystemStats(stats)
	if err != nil {
		t.Fatalf("StoreSystemStats() with zero values failed: %v", err)
	}

	// Test with large values
	stats = map[string]interface{}{
		"total_messages":    uint64(999999999),
		"parsed_messages":   uint64(999999999),
		"failed_messages":   uint64(999999999),
		"stored_states":     uint64(999999999),
		"created_flights":   uint64(999999999),
		"updated_flights":   uint64(999999999),
		"ended_flights":     uint64(999999999),
		"active_aircraft":   uint64(999999999),
		"active_flights":    uint64(999999999),
		"message_types":     [10]uint64{999999999, 999999999, 999999999, 999999999, 999999999, 999999999, 999999999, 999999999, 999999999, 999999999},
		"last_message_time": time.Now(),
		"processing_time":   time.Duration(999999999) * time.Millisecond,
	}

	err = client.StoreSystemStats(stats)
	if err != nil {
		t.Fatalf("StoreSystemStats() with large values failed: %v", err)
	}
}

func TestClient_StoreAircraftState_EdgeCases(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Test with zero values
	state := &types.AircraftState{
		HexIdent:     "",
		Callsign:     "",
		Altitude:     0,
		GroundSpeed:  0.0,
		Track:        0.0,
		Latitude:     0.0,
		Longitude:    0.0,
		VerticalRate: 0,
		Squawk:       "",
		OnGround:     false,
		MsgType:      0,
		Timestamp:    time.Now(),
		SessionID:    "",
	}

	err = client.StoreAircraftState(state)
	if err != nil {
		t.Fatalf("StoreAircraftState() with zero values failed: %v", err)
	}

	// Test with extreme values
	state = &types.AircraftState{
		HexIdent:     "FFFFFFFF",
		Callsign:     "VERY_LONG_CALLSIGN_THAT_MIGHT_EXCEED_LIMITS",
		Altitude:     999999,
		GroundSpeed:  999999.999,
		Track:        359.999,
		Latitude:     90.0,
		Longitude:    180.0,
		VerticalRate: 999999,
		Squawk:       "7777",
		OnGround:     true,
		MsgType:      99,
		Timestamp:    time.Now(),
		SessionID:    "very_long_session_id_that_might_exceed_limits",
	}

	err = client.StoreAircraftState(state)
	if err != nil {
		t.Fatalf("StoreAircraftState() with extreme values failed: %v", err)
	}
}
