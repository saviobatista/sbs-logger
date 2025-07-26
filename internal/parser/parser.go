package parser

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/saviobatista/sbs-logger/internal/types"
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
	MsgTypeStatus          MessageType = 10 // STA messages
	MsgTypeAircraft        MessageType = 11 // AIR messages
	MsgTypeID              MessageType = 12 // ID messages
)

// ParseMessage parses a raw SBS message into an aircraft state
func ParseMessage(raw string, timestamp time.Time) (*types.AircraftState, error) {
	// Split message into fields
	fields := strings.Split(strings.TrimSpace(raw), ",")

	// Check message type prefix
	var msgTypeIndex int
	var messageType string

	if len(fields) > 0 {
		messageType = fields[0]
	}

	switch messageType {
	case "MSG":
		// Standard SBS format: MSG,type,transmission_type,session_id,aircraft_id,hex_ident,flight_id,...
		if len(fields) < 22 {
			return nil, fmt.Errorf("invalid SBS message format: expected at least 22 fields, got %d (raw: %q)", len(fields), raw)
		}
		msgTypeIndex = 1 // Message type is at index 1 after "MSG"

	case "STA", "AIR", "ID":
		// Status/Aircraft/ID format: TYPE,,transmission_type,session_id,aircraft_id,hex_ident,flight_id,...
		if len(fields) < 10 {
			return nil, fmt.Errorf("invalid %s message format: expected at least 10 fields, got %d (raw: %q)", messageType, len(fields), raw)
		}
		// For these message types, we'll treat them as status messages
		// and extract basic info without requiring full SBS format
		state := &types.AircraftState{
			MsgType:   getMessageTypeFromPrefix(messageType),
			Timestamp: timestamp,
		}

		// Extract hex identifier if available
		if len(fields) > 4 {
			state.HexIdent = fields[4]
		}

		// Extract callsign if available
		if len(fields) > 9 {
			state.Callsign = fields[9]
		}

		return state, nil

	default:
		return nil, fmt.Errorf("unknown message type prefix: %s (raw: %q)", messageType, raw)
	}

	// Parse message type
	msgType, err := strconv.Atoi(fields[msgTypeIndex])
	if err != nil {
		return nil, fmt.Errorf("invalid message type: %w (field value: %q)", err, fields[msgTypeIndex])
	}

	// Create state
	state := &types.AircraftState{
		MsgType:   msgType,
		Timestamp: timestamp,
	}

	// Parse fields based on message type
	if err := parseMessageFields(state, fields, msgTypeIndex); err != nil {
		return nil, err
	}

	return state, nil
}

// parseMessageFields parses the message fields based on message type
func parseMessageFields(state *types.AircraftState, fields []string, msgTypeIndex int) error {
	// Set hex identifier for all message types that have it
	if state.MsgType != int(MsgTypeSelectionChange) && state.MsgType != int(MsgTypeNewAircraft) {
		state.HexIdent = fields[4+msgTypeIndex]
	}

	switch MessageType(state.MsgType) {
	case MsgTypeSelectionChange, MsgTypeNewAircraft:
		// These messages don't contain state information
		return nil

	case MsgTypeNewID:
		// Only hex identifier is set above

	case MsgTypeNewCallSign:
		state.Callsign = fields[10+msgTypeIndex]

	case MsgTypeNewAltitude:
		parseAltitude(state, fields, msgTypeIndex)

	case MsgTypeNewGroundSpeed:
		parseGroundSpeed(state, fields, msgTypeIndex)

	case MsgTypeNewTrack:
		parseTrack(state, fields, msgTypeIndex)

	case MsgTypeNewLatLon:
		parseLatLon(state, fields, msgTypeIndex)

	case MsgTypeNewGround:
		parseOnGround(state, fields, msgTypeIndex)

	case MsgTypeStatus, MsgTypeAircraft, MsgTypeID:
		// These are status/info messages that don't contain state information
		// but we can extract some basic info if available
		if len(fields) > 10+msgTypeIndex {
			state.Callsign = fields[10+msgTypeIndex]
		}
		return nil

	default:
		return fmt.Errorf("unknown message type: %d (raw message: %q)", state.MsgType, strings.Join(fields, ","))
	}

	return nil
}

// parseAltitude parses altitude field
func parseAltitude(state *types.AircraftState, fields []string, msgTypeIndex int) {
	if alt, err := strconv.Atoi(fields[11+msgTypeIndex]); err == nil {
		state.Altitude = alt
	}
}

// parseGroundSpeed parses ground speed field
func parseGroundSpeed(state *types.AircraftState, fields []string, msgTypeIndex int) {
	if speed, err := strconv.ParseFloat(fields[12+msgTypeIndex], 64); err == nil {
		state.GroundSpeed = speed
	}
}

// parseTrack parses track field
func parseTrack(state *types.AircraftState, fields []string, msgTypeIndex int) {
	if track, err := strconv.ParseFloat(fields[13+msgTypeIndex], 64); err == nil {
		state.Track = track
	}
}

// parseLatLon parses latitude, longitude, and related fields
func parseLatLon(state *types.AircraftState, fields []string, msgTypeIndex int) {
	if lat, err := strconv.ParseFloat(fields[14+msgTypeIndex], 64); err == nil {
		state.Latitude = lat
	}
	if lon, err := strconv.ParseFloat(fields[15+msgTypeIndex], 64); err == nil {
		state.Longitude = lon
	}
	parseAltitude(state, fields, msgTypeIndex)
	parseGroundSpeed(state, fields, msgTypeIndex)
	parseTrack(state, fields, msgTypeIndex)

	if vr, err := strconv.Atoi(fields[16+msgTypeIndex]); err == nil {
		state.VerticalRate = vr
	}
	if squawk, err := strconv.Atoi(fields[17+msgTypeIndex]); err == nil {
		state.Squawk = fmt.Sprintf("%04d", squawk)
	}
	parseOnGround(state, fields, msgTypeIndex)
}

// parseOnGround parses on ground field
func parseOnGround(state *types.AircraftState, fields []string, msgTypeIndex int) {
	if len(fields) > 21+msgTypeIndex {
		if onGround, err := strconv.Atoi(fields[21+msgTypeIndex]); err == nil {
			state.OnGround = onGround == 1
		}
	}
}

// getMessageTypeFromPrefix converts message prefix to message type
func getMessageTypeFromPrefix(prefix string) int {
	switch prefix {
	case "STA":
		return int(MsgTypeStatus)
	case "AIR":
		return int(MsgTypeAircraft)
	case "ID":
		return int(MsgTypeID)
	default:
		return 0
	}
}
