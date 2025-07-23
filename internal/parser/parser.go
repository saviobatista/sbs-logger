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
	MsgTypeNewAircraft     MessageType = 2
	MsgTypeNewID           MessageType = 3
	MsgTypeNewCallSign     MessageType = 4
	MsgTypeNewAltitude     MessageType = 5
	MsgTypeNewGroundSpeed  MessageType = 6
	MsgTypeNewTrack        MessageType = 7
	MsgTypeNewLatLon       MessageType = 8
	MsgTypeNewGround       MessageType = 9
)

// ParseMessage parses a raw SBS message into an aircraft state
func ParseMessage(raw string, timestamp time.Time) (*types.AircraftState, error) {
	// Split message into fields
	fields := strings.Split(strings.TrimSpace(raw), ",")
	if len(fields) < 22 {
		return nil, fmt.Errorf("invalid message format: expected at least 22 fields, got %d", len(fields))
	}

	// Check if message starts with "MSG" (SBS format)
	msgTypeIndex := 0
	if len(fields) > 0 && fields[0] == "MSG" {
		// SBS format: MSG,type,transmission_type,session_id,aircraft_id,hex_ident,flight_id,...
		if len(fields) < 22 {
			return nil, fmt.Errorf("invalid SBS message format: expected at least 22 fields, got %d", len(fields))
		}
		msgTypeIndex = 1 // Message type is at index 1 after "MSG"
	}

	// Parse message type
	msgType, err := strconv.Atoi(fields[msgTypeIndex])
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
		state.HexIdent = fields[4+msgTypeIndex]

	case MsgTypeNewCallSign:
		state.HexIdent = fields[4+msgTypeIndex]
		state.Callsign = fields[10+msgTypeIndex]

	case MsgTypeNewAltitude:
		state.HexIdent = fields[4+msgTypeIndex]
		if alt, err := strconv.Atoi(fields[11+msgTypeIndex]); err == nil {
			state.Altitude = alt
		}

	case MsgTypeNewGroundSpeed:
		state.HexIdent = fields[4+msgTypeIndex]
		if speed, err := strconv.ParseFloat(fields[12+msgTypeIndex], 64); err == nil {
			state.GroundSpeed = speed
		}

	case MsgTypeNewTrack:
		state.HexIdent = fields[4+msgTypeIndex]
		if track, err := strconv.ParseFloat(fields[13+msgTypeIndex], 64); err == nil {
			state.Track = track
		}

	case MsgTypeNewLatLon:
		state.HexIdent = fields[4+msgTypeIndex]
		if lat, err := strconv.ParseFloat(fields[14+msgTypeIndex], 64); err == nil {
			state.Latitude = lat
		}
		if lon, err := strconv.ParseFloat(fields[15+msgTypeIndex], 64); err == nil {
			state.Longitude = lon
		}
		if alt, err := strconv.Atoi(fields[11+msgTypeIndex]); err == nil {
			state.Altitude = alt
		}
		if speed, err := strconv.ParseFloat(fields[12+msgTypeIndex], 64); err == nil {
			state.GroundSpeed = speed
		}
		if track, err := strconv.ParseFloat(fields[13+msgTypeIndex], 64); err == nil {
			state.Track = track
		}
		if vr, err := strconv.Atoi(fields[16+msgTypeIndex]); err == nil {
			state.VerticalRate = vr
		}
		if squawk, err := strconv.Atoi(fields[17+msgTypeIndex]); err == nil {
			state.Squawk = fmt.Sprintf("%04d", squawk)
		}
		// Check if we have enough fields for onGround
		if len(fields) > 21+msgTypeIndex {
			if onGround, err := strconv.Atoi(fields[21+msgTypeIndex]); err == nil {
				state.OnGround = onGround == 1
			}
		}

	case MsgTypeNewGround:
		state.HexIdent = fields[4+msgTypeIndex]
		// Check if we have enough fields for onGround
		if len(fields) > 21+msgTypeIndex {
			if onGround, err := strconv.Atoi(fields[21+msgTypeIndex]); err == nil {
				state.OnGround = onGround == 1
			}
		}

	default:
		return nil, fmt.Errorf("unknown message type: %d", msgType)
	}

	return state, nil
}
