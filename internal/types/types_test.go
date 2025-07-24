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

func TestSBSMessage_EmptyValues(t *testing.T) {
	msg := SBSMessage{}

	if msg.Raw != "" {
		t.Errorf("Expected empty Raw, got %v", msg.Raw)
	}
	if !msg.Timestamp.IsZero() {
		t.Errorf("Expected zero Timestamp, got %v", msg.Timestamp)
	}
	if msg.Source != "" {
		t.Errorf("Expected empty Source, got %v", msg.Source)
	}
}

func TestSBSMessage_JSONEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		msg     SBSMessage
		wantErr bool
	}{
		{
			name: "empty message",
			msg:  SBSMessage{},
		},
		{
			name: "message with special characters",
			msg: SBSMessage{
				Raw:       "MSG,8,111,11111,111111,ABC123,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
				Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Source:    "test-source-with-special-chars!@#$%",
			},
		},
		{
			name: "message with unicode characters",
			msg: SBSMessage{
				Raw:       "MSG,8,111,11111,111111,ABC123,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
				Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Source:    "test-source-unicode-🚁",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if tt.wantErr && err == nil {
				t.Error("Expected error, got nil")
				return
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !tt.wantErr {
				var unmarshaled SBSMessage
				err = json.Unmarshal(data, &unmarshaled)
				if err != nil {
					t.Errorf("Failed to unmarshal: %v", err)
				}

				if tt.msg.Raw != unmarshaled.Raw {
					t.Errorf("Raw mismatch: got %v, want %v", unmarshaled.Raw, tt.msg.Raw)
				}
				if !tt.msg.Timestamp.Equal(unmarshaled.Timestamp) {
					t.Errorf("Timestamp mismatch: got %v, want %v", unmarshaled.Timestamp, tt.msg.Timestamp)
				}
				if tt.msg.Source != unmarshaled.Source {
					t.Errorf("Source mismatch: got %v, want %v", unmarshaled.Source, tt.msg.Source)
				}
			}
		})
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

func TestAircraftState_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		state   AircraftState
		wantErr bool
	}{
		{
			name:  "empty state",
			state: AircraftState{},
		},
		{
			name: "negative values",
			state: AircraftState{
				HexIdent:     "ABC123",
				Callsign:     "TEST123",
				Altitude:     -1000,
				GroundSpeed:  -50.0,
				Track:        -180.0,
				Latitude:     -90.0,
				Longitude:    -180.0,
				VerticalRate: -2000,
				Squawk:       "1234",
				OnGround:     true,
				MsgType:      1,
				Timestamp:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				SessionID:    "session-123",
			},
		},
		{
			name: "extreme values",
			state: AircraftState{
				HexIdent:     "ABC123",
				Callsign:     "TEST123",
				Altitude:     999999,
				GroundSpeed:  999999.99,
				Track:        359.99,
				Latitude:     90.0,
				Longitude:    180.0,
				VerticalRate: 999999,
				Squawk:       "7777",
				OnGround:     false,
				MsgType:      99,
				Timestamp:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				SessionID:    "session-123",
			},
		},
		{
			name: "zero values",
			state: AircraftState{
				HexIdent:     "ABC123",
				Callsign:     "TEST123",
				Altitude:     0,
				GroundSpeed:  0.0,
				Track:        0.0,
				Latitude:     0.0,
				Longitude:    0.0,
				VerticalRate: 0,
				Squawk:       "0000",
				OnGround:     false,
				MsgType:      0,
				Timestamp:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				SessionID:    "session-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.state)
			if tt.wantErr && err == nil {
				t.Error("Expected error, got nil")
				return
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !tt.wantErr {
				var unmarshaled AircraftState
				err = json.Unmarshal(data, &unmarshaled)
				if err != nil {
					t.Errorf("Failed to unmarshal: %v", err)
				}

				// Verify all fields match
				if tt.state.HexIdent != unmarshaled.HexIdent {
					t.Errorf("HexIdent mismatch: got %v, want %v", unmarshaled.HexIdent, tt.state.HexIdent)
				}
				if tt.state.Altitude != unmarshaled.Altitude {
					t.Errorf("Altitude mismatch: got %v, want %v", unmarshaled.Altitude, tt.state.Altitude)
				}
				if tt.state.GroundSpeed != unmarshaled.GroundSpeed {
					t.Errorf("GroundSpeed mismatch: got %v, want %v", unmarshaled.GroundSpeed, tt.state.GroundSpeed)
				}
				if tt.state.Latitude != unmarshaled.Latitude {
					t.Errorf("Latitude mismatch: got %v, want %v", unmarshaled.Latitude, tt.state.Latitude)
				}
				if tt.state.Longitude != unmarshaled.Longitude {
					t.Errorf("Longitude mismatch: got %v, want %v", unmarshaled.Longitude, tt.state.Longitude)
				}
			}
		})
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

func TestFlight_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		flight  Flight
		wantErr bool
	}{
		{
			name:   "empty flight",
			flight: Flight{},
		},
		{
			name: "flight with same start and end time",
			flight: Flight{
				SessionID:      "session-123",
				HexIdent:       "ABC123",
				Callsign:       "TEST123",
				StartedAt:      time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				EndedAt:        time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				FirstLatitude:  40.7128,
				FirstLongitude: -74.0060,
				LastLatitude:   40.7128,
				LastLongitude:  -74.0060,
				MaxAltitude:    0,
				MaxGroundSpeed: 0.0,
			},
		},
		{
			name: "flight with extreme coordinates",
			flight: Flight{
				SessionID:      "session-123",
				HexIdent:       "ABC123",
				Callsign:       "TEST123",
				StartedAt:      time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC),
				EndedAt:        time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				FirstLatitude:  90.0,
				FirstLongitude: 180.0,
				LastLatitude:   -90.0,
				LastLongitude:  -180.0,
				MaxAltitude:    999999,
				MaxGroundSpeed: 999999.99,
			},
		},
		{
			name: "flight with negative values",
			flight: Flight{
				SessionID:      "session-123",
				HexIdent:       "ABC123",
				Callsign:       "TEST123",
				StartedAt:      time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC),
				EndedAt:        time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				FirstLatitude:  -90.0,
				FirstLongitude: -180.0,
				LastLatitude:   -45.0,
				LastLongitude:  -90.0,
				MaxAltitude:    -1000,
				MaxGroundSpeed: -50.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.flight)
			if tt.wantErr && err == nil {
				t.Error("Expected error, got nil")
				return
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !tt.wantErr {
				var unmarshaled Flight
				err = json.Unmarshal(data, &unmarshaled)
				if err != nil {
					t.Errorf("Failed to unmarshal: %v", err)
				}

				// Verify all fields match
				if tt.flight.SessionID != unmarshaled.SessionID {
					t.Errorf("SessionID mismatch: got %v, want %v", unmarshaled.SessionID, tt.flight.SessionID)
				}
				if tt.flight.HexIdent != unmarshaled.HexIdent {
					t.Errorf("HexIdent mismatch: got %v, want %v", unmarshaled.HexIdent, tt.flight.HexIdent)
				}
				if !tt.flight.StartedAt.Equal(unmarshaled.StartedAt) {
					t.Errorf("StartedAt mismatch: got %v, want %v", unmarshaled.StartedAt, tt.flight.StartedAt)
				}
				if !tt.flight.EndedAt.Equal(unmarshaled.EndedAt) {
					t.Errorf("EndedAt mismatch: got %v, want %v", unmarshaled.EndedAt, tt.flight.EndedAt)
				}
				if tt.flight.FirstLatitude != unmarshaled.FirstLatitude {
					t.Errorf("FirstLatitude mismatch: got %v, want %v", unmarshaled.FirstLatitude, tt.flight.FirstLatitude)
				}
				if tt.flight.FirstLongitude != unmarshaled.FirstLongitude {
					t.Errorf("FirstLongitude mismatch: got %v, want %v", unmarshaled.FirstLongitude, tt.flight.FirstLongitude)
				}
			}
		})
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

func TestFlight_ZeroValues(t *testing.T) {
	flight := Flight{}

	if flight.SessionID != "" {
		t.Errorf("Expected empty SessionID, got %v", flight.SessionID)
	}
	if flight.HexIdent != "" {
		t.Errorf("Expected empty HexIdent, got %v", flight.HexIdent)
	}
	if flight.Callsign != "" {
		t.Errorf("Expected empty Callsign, got %v", flight.Callsign)
	}
	if !flight.StartedAt.IsZero() {
		t.Errorf("Expected zero StartedAt, got %v", flight.StartedAt)
	}
	if !flight.EndedAt.IsZero() {
		t.Errorf("Expected zero EndedAt, got %v", flight.EndedAt)
	}
	if flight.FirstLatitude != 0 {
		t.Errorf("Expected 0 FirstLatitude, got %v", flight.FirstLatitude)
	}
	if flight.FirstLongitude != 0 {
		t.Errorf("Expected 0 FirstLongitude, got %v", flight.FirstLongitude)
	}
	if flight.LastLatitude != 0 {
		t.Errorf("Expected 0 LastLatitude, got %v", flight.LastLatitude)
	}
	if flight.LastLongitude != 0 {
		t.Errorf("Expected 0 LastLongitude, got %v", flight.LastLongitude)
	}
	if flight.MaxAltitude != 0 {
		t.Errorf("Expected 0 MaxAltitude, got %v", flight.MaxAltitude)
	}
	if flight.MaxGroundSpeed != 0 {
		t.Errorf("Expected 0 MaxGroundSpeed, got %v", flight.MaxGroundSpeed)
	}
}

func TestSBSMessage_ZeroValues(t *testing.T) {
	msg := SBSMessage{}

	if msg.Raw != "" {
		t.Errorf("Expected empty Raw, got %v", msg.Raw)
	}
	if !msg.Timestamp.IsZero() {
		t.Errorf("Expected zero Timestamp, got %v", msg.Timestamp)
	}
	if msg.Source != "" {
		t.Errorf("Expected empty Source, got %v", msg.Source)
	}
}

func TestJSONMarshaling_InvalidData(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{
			name:    "invalid JSON",
			data:    `{"invalid": json}`,
			wantErr: true,
		},
		{
			name:    "missing required fields",
			data:    `{}`,
			wantErr: false, // Should not error, just use zero values
		},
		{
			name:    "wrong field types",
			data:    `{"hex_ident": 123, "altitude": "not_a_number"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var state AircraftState
			err := json.Unmarshal([]byte(tt.data), &state)
			if tt.wantErr && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestStructFieldAccess(t *testing.T) {
	// Test that all struct fields can be accessed and modified
	state := AircraftState{}

	// Test field assignment
	state.HexIdent = "TEST123"
	state.Callsign = "FLIGHT123"
	state.Altitude = 30000
	state.GroundSpeed = 500.0
	state.Track = 90.0
	state.Latitude = 40.7128
	state.Longitude = -74.0060
	state.VerticalRate = 1500
	state.Squawk = "1234"
	state.OnGround = false
	state.MsgType = 8
	state.Timestamp = time.Now()
	state.SessionID = "session-456"

	// Test field retrieval
	if state.HexIdent != "TEST123" {
		t.Errorf("HexIdent not set correctly: got %v", state.HexIdent)
	}
	if state.Callsign != "FLIGHT123" {
		t.Errorf("Callsign not set correctly: got %v", state.Callsign)
	}
	if state.Altitude != 30000 {
		t.Errorf("Altitude not set correctly: got %v", state.Altitude)
	}
	if state.GroundSpeed != 500.0 {
		t.Errorf("GroundSpeed not set correctly: got %v", state.GroundSpeed)
	}
	if state.Track != 90.0 {
		t.Errorf("Track not set correctly: got %v", state.Track)
	}
	if state.Latitude != 40.7128 {
		t.Errorf("Latitude not set correctly: got %v", state.Latitude)
	}
	if state.Longitude != -74.0060 {
		t.Errorf("Longitude not set correctly: got %v", state.Longitude)
	}
	if state.VerticalRate != 1500 {
		t.Errorf("VerticalRate not set correctly: got %v", state.VerticalRate)
	}
	if state.Squawk != "1234" {
		t.Errorf("Squawk not set correctly: got %v", state.Squawk)
	}
	if state.OnGround != false {
		t.Errorf("OnGround not set correctly: got %v", state.OnGround)
	}
	if state.MsgType != 8 {
		t.Errorf("MsgType not set correctly: got %v", state.MsgType)
	}
	if state.SessionID != "session-456" {
		t.Errorf("SessionID not set correctly: got %v", state.SessionID)
	}
}
