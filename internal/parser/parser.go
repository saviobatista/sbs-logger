package parser

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/savio/sbs-logger/internal/types"
)

// MessageType represents the type of SBS message
type MessageType int

const (
	// SBS message types
	MsgTypeSelectionChange MessageType = 1
	MsgTypeNewAircraft    MessageType = 2
	MsgTypeNewID          MessageType = 3
	MsgTypeNewCallSign    MessageType = 4
	MsgTypeNewAltitude    MessageType = 5
	MsgTypeNewGroundSpeed MessageType = 6
	MsgTypeNewTrack       MessageType = 7
	MsgTypeNewLatLon      MessageType = 8
	MsgTypeNewGround      MessageType = 9
)

// ParseMessage parses a raw SBS message into an aircraft state
func ParseMessage(raw string, timestamp time.Time) (*types.AircraftState, error) {
	// Split message into fields
	fields := strings.Split(strings.TrimSpace(raw), ",")
	if len(fields) < 22 {
		return nil, fmt.Errorf("invalid message format: expected at least 22 fields, got %d", len(fields))
	}

	// Parse message type
	msgType, err := strconv.Atoi(fields[0])
	if err != nil {
		return nil, fmt.Errorf("invalid message type: %w", err)
	}

	// Create state
	state := &types.AircraftState{
		MsgType:   msgType,
		Timestamp: timestamp,
	}

	// Parse fields based on message type
	switch MessageType(msgType) {
	case MsgTypeSelectionChange, MsgTypeNewAircraft:
		// These messages don't contain state information
		return nil, nil

	case MsgTypeNewID:
		state.HexIdent = fields[4]

	case MsgTypeNewCallSign:
		state.HexIdent = fields[4]
		state.Callsign = fields[10]

	case MsgTypeNewAltitude:
		state.HexIdent = fields[4]
		if alt, err := strconv.Atoi(fields[11]); err == nil {
			state.Altitude = alt
		}

	case MsgTypeNewGroundSpeed:
		state.HexIdent = fields[4]
		if speed, err := strconv.ParseFloat(fields[12], 64); err == nil {
			state.GroundSpeed = speed
		}

	case MsgTypeNewTrack:
		state.HexIdent = fields[4]
		if track, err := strconv.ParseFloat(fields[13], 64); err == nil {
			state.Track = track
		}

	case MsgTypeNewLatLon:
		state.HexIdent = fields[4]
		if lat, err := strconv.ParseFloat(fields[14], 64); err == nil {
			state.Latitude = lat
		}
		if lon, err := strconv.ParseFloat(fields[15], 64); err == nil {
			state.Longitude = lon
		}
		if alt, err := strconv.Atoi(fields[11]); err == nil {
			state.Altitude = alt
		}
		if speed, err := strconv.ParseFloat(fields[12], 64); err == nil {
			state.GroundSpeed = speed
		}
		if track, err := strconv.ParseFloat(fields[13], 64); err == nil {
			state.Track = track
		}
		if vr, err := strconv.Atoi(fields[16]); err == nil {
			state.VerticalRate = vr
		}
		if squawk, err := strconv.Atoi(fields[17]); err == nil {
			state.Squawk = fmt.Sprintf("%04d", squawk)
		}
		if onGround, err := strconv.Atoi(fields[21]); err == nil {
			state.OnGround = onGround == 1
		}

	case MsgTypeNewGround:
		state.HexIdent = fields[4]
		if onGround, err := strconv.Atoi(fields[21]); err == nil {
			state.OnGround = onGround == 1
		}

	default:
		return nil, fmt.Errorf("unknown message type: %d", msgType)
	}

	return state, nil
} 