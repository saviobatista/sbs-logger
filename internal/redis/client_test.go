package redis

import (
	"context"
	"testing"
	"time"

	"github.com/savio/sbs-logger/internal/types"
)

func TestNew_ValidAddress(t *testing.T) {
	// This test requires a Redis server running on localhost:6379
	// For now, we'll test the function structure without actual connection
	addr := "localhost:6379"
	
	// Test that the function doesn't panic
	// Note: This will fail if Redis is not running, but that's expected
	client, err := New(addr)
	if err != nil {
		// Expected if Redis is not running
		t.Logf("Expected error when Redis is not running: %v", err)
		return
	}
	
	if client == nil {
		t.Fatal("New() returned nil client")
	}
	
	if client.client == nil {
		t.Error("Expected Redis client to be initialized")
	}
	
	// Clean up
	client.Close()
}

func TestNew_InvalidAddress(t *testing.T) {
	addr := "invalid:address:12345"
	
	client, err := New(addr)
	if err == nil {
		t.Error("New() should fail with invalid address")
		client.Close()
		return
	}
	
	if client != nil {
		t.Error("New() should return nil client on error")
	}
}

func TestClient_Close(t *testing.T) {
	// Test close without initialization
	client := &Client{}
	
	// This will panic because client.client is nil
	// We should test this with a proper client or handle the nil case
	defer func() {
		if r := recover(); r != nil {
			// Expected panic when client is nil
			t.Logf("Expected panic when client is nil: %v", r)
		}
	}()
	
	client.Close()
	t.Error("Close() should panic when client is nil")
}

func TestClient_StoreFlight(t *testing.T) {
	// This test requires Redis to be running
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}
	defer client.Close()
	
	ctx := context.Background()
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
	
	err = client.StoreFlight(ctx, flight)
	if err != nil {
		t.Fatalf("StoreFlight() failed: %v", err)
	}
	
	// Clean up
	client.DeleteFlight(ctx, flight.HexIdent)
}

func TestClient_GetFlight(t *testing.T) {
	// This test requires Redis to be running
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}
	defer client.Close()
	
	ctx := context.Background()
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
	
	// Store the flight first
	err = client.StoreFlight(ctx, flight)
	if err != nil {
		t.Fatalf("StoreFlight() failed: %v", err)
	}
	
	// Retrieve the flight
	retrievedFlight, err := client.GetFlight(ctx, flight.HexIdent)
	if err != nil {
		t.Fatalf("GetFlight() failed: %v", err)
	}
	
	if retrievedFlight == nil {
		t.Fatal("GetFlight() returned nil")
	}
	
	if retrievedFlight.HexIdent != flight.HexIdent {
		t.Errorf("Expected HexIdent %s, got %s", flight.HexIdent, retrievedFlight.HexIdent)
	}
	
	if retrievedFlight.Callsign != flight.Callsign {
		t.Errorf("Expected Callsign %s, got %s", flight.Callsign, retrievedFlight.Callsign)
	}
	
	// Test getting non-existent flight
	nonExistentFlight, err := client.GetFlight(ctx, "NONEXISTENT")
	if err != nil {
		t.Fatalf("GetFlight() should not fail for non-existent flight: %v", err)
	}
	
	if nonExistentFlight != nil {
		t.Error("GetFlight() should return nil for non-existent flight")
	}
	
	// Clean up
	client.DeleteFlight(ctx, flight.HexIdent)
}

func TestClient_DeleteFlight(t *testing.T) {
	// This test requires Redis to be running
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}
	defer client.Close()
	
	ctx := context.Background()
	flight := &types.Flight{
		SessionID: "test-session",
		HexIdent:  "ABC123",
		Callsign:  "TEST123",
		StartedAt: time.Now(),
		EndedAt:   time.Now().Add(time.Hour),
	}
	
	// Store the flight first
	err = client.StoreFlight(ctx, flight)
	if err != nil {
		t.Fatalf("StoreFlight() failed: %v", err)
	}
	
	// Delete the flight
	err = client.DeleteFlight(ctx, flight.HexIdent)
	if err != nil {
		t.Fatalf("DeleteFlight() failed: %v", err)
	}
	
	// Verify it's deleted
	retrievedFlight, err := client.GetFlight(ctx, flight.HexIdent)
	if err != nil {
		t.Fatalf("GetFlight() should not fail after deletion: %v", err)
	}
	
	if retrievedFlight != nil {
		t.Error("Flight should be deleted")
	}
}

func TestClient_StoreAircraftState(t *testing.T) {
	// This test requires Redis to be running
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}
	defer client.Close()
	
	ctx := context.Background()
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
	
	err = client.StoreAircraftState(ctx, state)
	if err != nil {
		t.Fatalf("StoreAircraftState() failed: %v", err)
	}
	
	// Clean up
	client.DeleteAircraftState(ctx, state.HexIdent)
}

func TestClient_GetAircraftState(t *testing.T) {
	// This test requires Redis to be running
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}
	defer client.Close()
	
	ctx := context.Background()
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
	
	// Store the state first
	err = client.StoreAircraftState(ctx, state)
	if err != nil {
		t.Fatalf("StoreAircraftState() failed: %v", err)
	}
	
	// Retrieve the state
	retrievedState, err := client.GetAircraftState(ctx, state.HexIdent)
	if err != nil {
		t.Fatalf("GetAircraftState() failed: %v", err)
	}
	
	if retrievedState == nil {
		t.Fatal("GetAircraftState() returned nil")
	}
	
	if retrievedState.HexIdent != state.HexIdent {
		t.Errorf("Expected HexIdent %s, got %s", state.HexIdent, retrievedState.HexIdent)
	}
	
	if retrievedState.Callsign != state.Callsign {
		t.Errorf("Expected Callsign %s, got %s", state.Callsign, retrievedState.Callsign)
	}
	
	if retrievedState.Altitude != state.Altitude {
		t.Errorf("Expected Altitude %d, got %d", state.Altitude, retrievedState.Altitude)
	}
	
	// Test getting non-existent state
	nonExistentState, err := client.GetAircraftState(ctx, "NONEXISTENT")
	if err != nil {
		t.Fatalf("GetAircraftState() should not fail for non-existent state: %v", err)
	}
	
	if nonExistentState != nil {
		t.Error("GetAircraftState() should return nil for non-existent state")
	}
	
	// Clean up
	client.DeleteAircraftState(ctx, state.HexIdent)
}

func TestClient_DeleteAircraftState(t *testing.T) {
	// This test requires Redis to be running
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}
	defer client.Close()
	
	ctx := context.Background()
	state := &types.AircraftState{
		HexIdent:  "ABC123",
		Callsign:  "TEST123",
		Altitude:  35000,
		Timestamp: time.Now(),
	}
	
	// Store the state first
	err = client.StoreAircraftState(ctx, state)
	if err != nil {
		t.Fatalf("StoreAircraftState() failed: %v", err)
	}
	
	// Delete the state
	err = client.DeleteAircraftState(ctx, state.HexIdent)
	if err != nil {
		t.Fatalf("DeleteAircraftState() failed: %v", err)
	}
	
	// Verify it's deleted
	retrievedState, err := client.GetAircraftState(ctx, state.HexIdent)
	if err != nil {
		t.Fatalf("GetAircraftState() should not fail after deletion: %v", err)
	}
	
	if retrievedState != nil {
		t.Error("Aircraft state should be deleted")
	}
}

func TestClient_SetFlightValidation(t *testing.T) {
	// This test requires Redis to be running
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}
	defer client.Close()
	
	ctx := context.Background()
	hexIdent := "ABC123"
	
	// Test setting valid flight
	err = client.SetFlightValidation(ctx, hexIdent, true)
	if err != nil {
		t.Fatalf("SetFlightValidation(true) failed: %v", err)
	}
	
	// Test setting invalid flight
	err = client.SetFlightValidation(ctx, hexIdent, false)
	if err != nil {
		t.Fatalf("SetFlightValidation(false) failed: %v", err)
	}
	
	// Clean up
	client.client.Del(ctx, "validation:"+hexIdent)
}

func TestClient_GetFlightValidation(t *testing.T) {
	// This test requires Redis to be running
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}
	defer client.Close()
	
	ctx := context.Background()
	hexIdent := "ABC123"
	
	// Test getting non-existent validation
	valid, err := client.GetFlightValidation(ctx, hexIdent)
	if err != nil {
		t.Fatalf("GetFlightValidation() should not fail for non-existent validation: %v", err)
	}
	
	if valid {
		t.Error("Non-existent validation should return false")
	}
	
	// Test setting and getting valid flight
	err = client.SetFlightValidation(ctx, hexIdent, true)
	if err != nil {
		t.Fatalf("SetFlightValidation(true) failed: %v", err)
	}
	
	valid, err = client.GetFlightValidation(ctx, hexIdent)
	if err != nil {
		t.Fatalf("GetFlightValidation() failed: %v", err)
	}
	
	if !valid {
		t.Error("Validation should be true")
	}
	
	// Test setting and getting invalid flight
	err = client.SetFlightValidation(ctx, hexIdent, false)
	if err != nil {
		t.Fatalf("SetFlightValidation(false) failed: %v", err)
	}
	
	valid, err = client.GetFlightValidation(ctx, hexIdent)
	if err != nil {
		t.Fatalf("GetFlightValidation() failed: %v", err)
	}
	
	if valid {
		t.Error("Validation should be false")
	}
	
	// Clean up
	client.client.Del(ctx, "validation:"+hexIdent)
}

func TestClient_GetData_InvalidJSON(t *testing.T) {
	// This test requires Redis to be running
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}
	defer client.Close()
	
	ctx := context.Background()
	key := "test:invalid:json"
	
	// Store invalid JSON
	err = client.client.Set(ctx, key, "invalid json", time.Hour).Err()
	if err != nil {
		t.Fatalf("Failed to set invalid JSON: %v", err)
	}
	
	// Try to get it as a flight
	flight, err := client.GetFlight(ctx, "invalid")
	if err == nil {
		t.Error("GetFlight() should fail with invalid JSON")
	}
	
	if flight != nil {
		t.Error("GetFlight() should return nil with invalid JSON")
	}
	
	// Clean up
	client.client.Del(ctx, key)
} 