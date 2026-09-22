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
			raw:       "MSG,8,111,11111,ABC123,111111,111111,111111,111111,111111,111111,35000,450,180,40.7128,-74.0060,0,1234,0,0,0,0",
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
			raw:       "MSG,4,111,11111,ABC123,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111",
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
			raw:       "MSG,99,111,11111,ABC123,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111",
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

// Real lines from the lab's receiver (dump1090 SBS output, 22 columns).
func TestParseMessage_RealSBS(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		raw  string
		want types.AircraftState
	}{
		{
			raw:  "MSG,1,333,0,E49AE9,100,2026/09/22,14:06:29.364,2026/09/22,14:06:29.364,GLO1742 ,,,,,,,,,,,0",
			want: types.AircraftState{HexIdent: "E49AE9", Callsign: "GLO1742", MsgType: 1},
		},
		{
			raw:  "MSG,3,333,0,E491D1,100,2026/09/22,14:06:29.394,2026/09/22,14:06:29.394,,7800,,,-22.81143,-46.96381,,,0,0,0,0",
			want: types.AircraftState{HexIdent: "E491D1", Altitude: 7800, Latitude: -22.81143, Longitude: -46.96381, MsgType: 3},
		},
		{
			raw:  "MSG,4,333,0,E492A5,100,2026/09/22,14:06:29.408,2026/09/22,14:06:29.408,,,412,187.3,,,-1088,,,,,0",
			want: types.AircraftState{HexIdent: "E492A5", GroundSpeed: 412, Track: 187.3, VerticalRate: -1088, MsgType: 4},
		},
		{
			raw:  "MSG,6,333,0,E492A5,100,2026/09/22,14:06:29.408,2026/09/22,14:06:29.408,,18650,,,,,,2201,0,0,0,0",
			want: types.AircraftState{HexIdent: "E492A5", Altitude: 18650, Squawk: "2201", MsgType: 6},
		},
		{
			raw:  "MSG,8,333,0,E492A5,100,2026/09/22,14:06:29.417,2026/09/22,14:06:29.417,,,,,,,,,,,,1",
			want: types.AircraftState{HexIdent: "E492A5", OnGround: true, MsgType: 8},
		},
	}
	for _, c := range cases {
		got, err := ParseMessage(c.raw, now)
		if err != nil {
			t.Fatalf("%q: %v", c.raw, err)
		}
		c.want.Timestamp = got.Timestamp
		if *got != c.want {
			t.Errorf("%q\n got %+v\nwant %+v", c.raw, *got, c.want)
		}
	}
}

// AIR/ID lines have ten columns and no callsign; column 9 is a time.
func TestParseMessage_AIRHasNoCallsign(t *testing.T) {
	got, err := ParseMessage("AIR,,333,0,E4A391,100,2026/09/22,14:25:37.169,2026/09/22,14:25:37.169", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got.HexIdent != "E4A391" || got.Callsign != "" {
		t.Errorf("got %+v", *got)
	}
}
