package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSBSMessage_JSON(t *testing.T) {
	msg := SBSMessage{
		Raw:       "MSG,8,111,11111,111111,ABC123,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
		Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		Source:    "test-source",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal SBSMessage: %v", err)
	}

	var unmarshaled SBSMessage
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal SBSMessage: %v", err)
	}

	if msg.Raw != unmarshaled.Raw {
		t.Errorf("Raw mismatch: got %v, want %v", unmarshaled.Raw, msg.Raw)
	}
	if !msg.Timestamp.Equal(unmarshaled.Timestamp) {
		t.Errorf("Timestamp mismatch: got %v, want %v", unmarshaled.Timestamp, msg.Timestamp)
	}
	if msg.Source != unmarshaled.Source {
		t.Errorf("Source mismatch: got %v, want %v", unmarshaled.Source, msg.Source)
	}
}

func TestAircraftState_JSON(t *testing.T) {
	state := AircraftState{
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
		Timestamp:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		SessionID:    "session-123",
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Failed to marshal AircraftState: %v", err)
	}

	var unmarshaled AircraftState
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal AircraftState: %v", err)
	}

	if state.HexIdent != unmarshaled.HexIdent {
		t.Errorf("HexIdent mismatch: got %v, want %v", unmarshaled.HexIdent, state.HexIdent)
	}
	if state.Callsign != unmarshaled.Callsign {
		t.Errorf("Callsign mismatch: got %v, want %v", unmarshaled.Callsign, state.Callsign)
	}
	if state.Altitude != unmarshaled.Altitude {
		t.Errorf("Altitude mismatch: got %v, want %v", unmarshaled.Altitude, state.Altitude)
	}
	if state.GroundSpeed != unmarshaled.GroundSpeed {
		t.Errorf("GroundSpeed mismatch: got %v, want %v", unmarshaled.GroundSpeed, state.GroundSpeed)
	}
	if state.Track != unmarshaled.Track {
		t.Errorf("Track mismatch: got %v, want %v", unmarshaled.Track, state.Track)
	}
	if state.Latitude != unmarshaled.Latitude {
		t.Errorf("Latitude mismatch: got %v, want %v", unmarshaled.Latitude, state.Latitude)
	}
	if state.Longitude != unmarshaled.Longitude {
		t.Errorf("Longitude mismatch: got %v, want %v", unmarshaled.Longitude, state.Longitude)
	}
	if state.VerticalRate != unmarshaled.VerticalRate {
		t.Errorf("VerticalRate mismatch: got %v, want %v", unmarshaled.VerticalRate, state.VerticalRate)
	}
	if state.Squawk != unmarshaled.Squawk {
		t.Errorf("Squawk mismatch: got %v, want %v", unmarshaled.Squawk, state.Squawk)
	}
	if state.OnGround != unmarshaled.OnGround {
		t.Errorf("OnGround mismatch: got %v, want %v", unmarshaled.OnGround, state.OnGround)
	}
	if state.MsgType != unmarshaled.MsgType {
		t.Errorf("MsgType mismatch: got %v, want %v", unmarshaled.MsgType, state.MsgType)
	}
	if !state.Timestamp.Equal(unmarshaled.Timestamp) {
		t.Errorf("Timestamp mismatch: got %v, want %v", unmarshaled.Timestamp, state.Timestamp)
	}
	if state.SessionID != unmarshaled.SessionID {
		t.Errorf("SessionID mismatch: got %v, want %v", unmarshaled.SessionID, state.SessionID)
	}
}

func TestFlight_JSON(t *testing.T) {
	flight := Flight{
		SessionID:      "session-123",
		HexIdent:       "ABC123",
		Callsign:       "TEST123",
		StartedAt:      time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC),
		EndedAt:        time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		FirstLatitude:  40.7128,
		FirstLongitude: -74.0060,
		LastLatitude:   41.7128,
		LastLongitude:  -75.0060,
		MaxAltitude:    35000,
		MaxGroundSpeed: 450.5,
	}

	data, err := json.Marshal(flight)
	if err != nil {
		t.Fatalf("Failed to marshal Flight: %v", err)
	}

	var unmarshaled Flight
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal Flight: %v", err)
	}

	if flight.SessionID != unmarshaled.SessionID {
		t.Errorf("SessionID mismatch: got %v, want %v", unmarshaled.SessionID, flight.SessionID)
	}
	if flight.HexIdent != unmarshaled.HexIdent {
		t.Errorf("HexIdent mismatch: got %v, want %v", unmarshaled.HexIdent, flight.HexIdent)
	}
	if flight.Callsign != unmarshaled.Callsign {
		t.Errorf("Callsign mismatch: got %v, want %v", unmarshaled.Callsign, flight.Callsign)
	}
	if !flight.StartedAt.Equal(unmarshaled.StartedAt) {
		t.Errorf("StartedAt mismatch: got %v, want %v", unmarshaled.StartedAt, flight.StartedAt)
	}
	if !flight.EndedAt.Equal(unmarshaled.EndedAt) {
		t.Errorf("EndedAt mismatch: got %v, want %v", unmarshaled.EndedAt, flight.EndedAt)
	}
	if flight.FirstLatitude != unmarshaled.FirstLatitude {
		t.Errorf("FirstLatitude mismatch: got %v, want %v", unmarshaled.FirstLatitude, flight.FirstLatitude)
	}
	if flight.FirstLongitude != unmarshaled.FirstLongitude {
		t.Errorf("FirstLongitude mismatch: got %v, want %v", unmarshaled.FirstLongitude, flight.FirstLongitude)
	}
	if flight.LastLatitude != unmarshaled.LastLatitude {
		t.Errorf("LastLatitude mismatch: got %v, want %v", unmarshaled.LastLatitude, flight.LastLatitude)
	}
	if flight.LastLongitude != unmarshaled.LastLongitude {
		t.Errorf("LastLongitude mismatch: got %v, want %v", unmarshaled.LastLongitude, flight.LastLongitude)
	}
	if flight.MaxAltitude != unmarshaled.MaxAltitude {
		t.Errorf("MaxAltitude mismatch: got %v, want %v", unmarshaled.MaxAltitude, flight.MaxAltitude)
	}
	if flight.MaxGroundSpeed != unmarshaled.MaxGroundSpeed {
		t.Errorf("MaxGroundSpeed mismatch: got %v, want %v", unmarshaled.MaxGroundSpeed, flight.MaxGroundSpeed)
	}
}

func TestAircraftState_ZeroValues(t *testing.T) {
	state := AircraftState{}

	if state.HexIdent != "" {
		t.Errorf("Expected empty HexIdent, got %v", state.HexIdent)
	}
	if state.Callsign != "" {
		t.Errorf("Expected empty Callsign, got %v", state.Callsign)
	}
	if state.Altitude != 0 {
		t.Errorf("Expected 0 Altitude, got %v", state.Altitude)
	}
	if state.GroundSpeed != 0 {
		t.Errorf("Expected 0 GroundSpeed, got %v", state.GroundSpeed)
	}
	if state.Track != 0 {
		t.Errorf("Expected 0 Track, got %v", state.Track)
	}
	if state.Latitude != 0 {
		t.Errorf("Expected 0 Latitude, got %v", state.Latitude)
	}
	if state.Longitude != 0 {
		t.Errorf("Expected 0 Longitude, got %v", state.Longitude)
	}
	if state.VerticalRate != 0 {
		t.Errorf("Expected 0 VerticalRate, got %v", state.VerticalRate)
	}
	if state.Squawk != "" {
		t.Errorf("Expected empty Squawk, got %v", state.Squawk)
	}
	if state.OnGround != false {
		t.Errorf("Expected false OnGround, got %v", state.OnGround)
	}
	if state.MsgType != 0 {
		t.Errorf("Expected 0 MsgType, got %v", state.MsgType)
	}
	if !state.Timestamp.IsZero() {
		t.Errorf("Expected zero Timestamp, got %v", state.Timestamp)
	}
	if state.SessionID != "" {
		t.Errorf("Expected empty SessionID, got %v", state.SessionID)
	}
} 