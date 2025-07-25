package parser

import (
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/testutils"
	"github.com/saviobatista/sbs-logger/internal/types"
)

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		timestamp time.Time
		wantErr   bool
		wantState *types.AircraftState
	}{
		{
			name:      "valid position message",
			raw:       "MSG,8,111,11111,111111,ABC123,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
			timestamp: time.Now().UTC(),
			wantErr:   false,
			wantState: &types.AircraftState{
				HexIdent:     "ABC123",
				Altitude:     35000,
				GroundSpeed:  450,
				Track:        180,
				Latitude:     40.7128,
				Longitude:    -74.0060,
				VerticalRate: 0,
				Squawk:       "1234",
				OnGround:     false,
				MsgType:      8,
			},
		},
		{
			name:      "valid callsign message",
			raw:       "MSG,4,111,11111,111111,ABC123,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111",
			timestamp: time.Now().UTC(),
			wantErr:   false,
			wantState: &types.AircraftState{
				HexIdent: "ABC123",
				MsgType:  4,
			},
		},
		{
			name:      "invalid message format",
			raw:       "MSG,8,111,11111",
			timestamp: time.Now().UTC(),
			wantErr:   true,
		},
		{
			name:      "unknown message type",
			raw:       "MSG,99,111,11111,111111,ABC123,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111",
			timestamp: time.Now().UTC(),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, err := ParseMessage(tt.raw, tt.timestamp)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseMessage() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseMessage() unexpected error: %v", err)
				return
			}

			if state == nil {
				t.Errorf("ParseMessage() returned nil state")
				return
			}

			if tt.wantState != nil {
				if state.HexIdent != tt.wantState.HexIdent {
					t.Errorf("ParseMessage() HexIdent = %v, want %v", state.HexIdent, tt.wantState.HexIdent)
				}
				if state.MsgType != tt.wantState.MsgType {
					t.Errorf("ParseMessage() MsgType = %v, want %v", state.MsgType, tt.wantState.MsgType)
				}
			}
		})
	}
}

func TestParseMessageWithMock(t *testing.T) {
	mockMsg := testutils.MockSBSMessage(8, "ABC123")
	state, err := ParseMessage(mockMsg.Raw, mockMsg.Timestamp)

	if err != nil {
		t.Errorf("ParseMessage() with mock failed: %v", err)
		return
	}

	if state == nil {
		t.Errorf("ParseMessage() with mock returned nil state")
		return
	}

	if state.HexIdent != "ABC123" {
		t.Errorf("ParseMessage() with mock HexIdent = %v, want ABC123", state.HexIdent)
	}
}
