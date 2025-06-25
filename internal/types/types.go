package types

import (
	"time"
)

// SBSMessage represents a raw SBS message
type SBSMessage struct {
	Raw       string    `json:"raw"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
}

// AircraftState represents the current state of an aircraft
type AircraftState struct {
	HexIdent     string    `json:"hex_ident"`
	Callsign     string    `json:"callsign"`
	Altitude     int       `json:"altitude"`
	GroundSpeed  float64   `json:"groundspeed"`
	Track        float64   `json:"track"`
	Latitude     float64   `json:"latitude"`
	Longitude    float64   `json:"longitude"`
	VerticalRate int       `json:"vertical_rate"`
	Squawk       string    `json:"squawk"`
	OnGround     bool      `json:"on_ground"`
	MsgType      int       `json:"msg_type"`
	Timestamp    time.Time `json:"timestamp"`
	SessionID    string    `json:"session_id"`
}

// Flight represents a complete flight session
type Flight struct {
	SessionID      string    `json:"session_id"`
	HexIdent       string    `json:"hex_ident"`
	Callsign       string    `json:"callsign"`
	StartedAt      time.Time `json:"started_at"`
	EndedAt        time.Time `json:"ended_at"`
	FirstLatitude  float64   `json:"first_latitude"`
	FirstLongitude float64   `json:"first_longitude"`
	LastLatitude   float64   `json:"last_latitude"`
	LastLongitude  float64   `json:"last_longitude"`
	MaxAltitude    int       `json:"max_altitude"`
	MaxGroundSpeed float64   `json:"max_ground_speed"`
}
