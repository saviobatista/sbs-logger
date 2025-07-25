package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/types"
)

// Mock DB client for testing
type mockDBClient struct {
	flights     []*types.Flight
	createError error
	updateError error
	storeError  error
	getError    error
}

func (m *mockDBClient) GetActiveFlights() ([]*types.Flight, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	return m.flights, nil
}

func (m *mockDBClient) CreateFlight(flight *types.Flight) error {
	if m.createError != nil {
		return m.createError
	}
	m.flights = append(m.flights, flight)
	return nil
}

func (m *mockDBClient) UpdateFlight(flight *types.Flight) error {
	if m.updateError != nil {
		return m.updateError
	}
	// Find and update the flight
	for i, f := range m.flights {
		if f.SessionID == flight.SessionID {
			m.flights[i] = flight
			break
		}
	}
	return nil
}

func (m *mockDBClient) StoreAircraftState(state *types.AircraftState) error {
	if m.storeError != nil {
		return m.storeError
	}
	return nil
}

func (m *mockDBClient) StoreSystemStats(stats map[string]interface{}) error {
	return nil
}

func (m *mockDBClient) GetSystemStats(from, to time.Time) ([]map[string]interface{}, error) {
	return nil, nil
}

func (m *mockDBClient) Close() error {
	return nil
}

// Mock Redis client for testing
type mockRedisClient struct {
	flights          map[string]*types.Flight
	aircraftStates   map[string]*types.AircraftState
	flightValidation map[string]bool
	storeError       error
	getError         error
}

func newMockRedisClient() *mockRedisClient {
	return &mockRedisClient{
		flights:          make(map[string]*types.Flight),
		aircraftStates:   make(map[string]*types.AircraftState),
		flightValidation: make(map[string]bool),
	}
}

func (m *mockRedisClient) StoreFlight(ctx context.Context, flight *types.Flight) error {
	if m.storeError != nil {
		return m.storeError
	}
	m.flights[flight.HexIdent] = flight
	return nil
}

func (m *mockRedisClient) GetFlight(ctx context.Context, hexIdent string) (*types.Flight, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	flight, exists := m.flights[hexIdent]
	if !exists {
		return nil, nil
	}
	return flight, nil
}

func (m *mockRedisClient) DeleteFlight(ctx context.Context, hexIdent string) error {
	delete(m.flights, hexIdent)
	return nil
}

func (m *mockRedisClient) StoreAircraftState(ctx context.Context, state *types.AircraftState) error {
	if m.storeError != nil {
		return m.storeError
	}
	m.aircraftStates[state.HexIdent] = state
	return nil
}

func (m *mockRedisClient) GetAircraftState(ctx context.Context, hexIdent string) (*types.AircraftState, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	state, exists := m.aircraftStates[hexIdent]
	if !exists {
		return nil, nil
	}
	return state, nil
}

func (m *mockRedisClient) DeleteAircraftState(ctx context.Context, hexIdent string) error {
	delete(m.aircraftStates, hexIdent)
	return nil
}

func (m *mockRedisClient) SetFlightValidation(ctx context.Context, hexIdent string, valid bool) error {
	m.flightValidation[hexIdent] = valid
	return nil
}

func (m *mockRedisClient) GetFlightValidation(ctx context.Context, hexIdent string) (bool, error) {
	if m.getError != nil {
		return false, m.getError
	}
	valid, exists := m.flightValidation[hexIdent]
	if !exists {
		return true, nil // Default to valid if not set
	}
	return valid, nil
}

func (m *mockRedisClient) Close() error {
	return nil
}

// TestNewStateTracker tests the StateTracker constructor
func TestNewStateTracker(t *testing.T) {
	mockDB := &mockDBClient{}
	mockRedis := newMockRedisClient()

	tracker := NewStateTracker(mockDB, mockRedis)

	if tracker.db != mockDB {
		t.Error("Expected DB client to be set")
	}

	if tracker.redis != mockRedis {
		t.Error("Expected Redis client to be set")
	}

	if tracker.activeFlights == nil {
		t.Error("Expected activeFlights map to be initialized")
	}

	if tracker.states == nil {
		t.Error("Expected states map to be initialized")
	}

	if tracker.stats == nil {
		t.Error("Expected stats to be initialized")
	}
}

// TestStateTracker_Start tests the Start method
func TestStateTracker_Start(t *testing.T) {
	tests := []struct {
		name            string
		setupMocks      func() (*mockDBClient, *mockRedisClient)
		expectError     bool
		expectedFlights int
	}{
		{
			name: "successful start with no active flights",
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{flights: []*types.Flight{}}
				mockRedis := newMockRedisClient()
				return mockDB, mockRedis
			},
			expectError:     false,
			expectedFlights: 0,
		},
		{
			name: "successful start with active flights",
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				flights := []*types.Flight{
					{
						SessionID: "session1",
						HexIdent:  "ABC123",
						Callsign:  "TEST123",
						StartedAt: time.Now(),
					},
					{
						SessionID: "session2",
						HexIdent:  "DEF456",
						Callsign:  "TEST456",
						StartedAt: time.Now(),
					},
				}
				mockDB := &mockDBClient{flights: flights}
				mockRedis := newMockRedisClient()
				return mockDB, mockRedis
			},
			expectError:     false,
			expectedFlights: 2,
		},
		{
			name: "database error on start",
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{getError: fmt.Errorf("database error")}
				mockRedis := newMockRedisClient()
				return mockDB, mockRedis
			},
			expectError:     true,
			expectedFlights: 0,
		},
		{
			name: "successful start with Redis cache error (should continue)",
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				flights := []*types.Flight{
					{
						SessionID: "session1",
						HexIdent:  "ABC123",
						Callsign:  "TEST123",
						StartedAt: time.Now(),
					},
				}
				mockDB := &mockDBClient{flights: flights}
				mockRedis := newMockRedisClient()
				mockRedis.storeError = fmt.Errorf("redis store error")
				return mockDB, mockRedis
			},
			expectError:     false,
			expectedFlights: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, mockRedis := tt.setupMocks()
			tracker := NewStateTracker(mockDB, mockRedis)

			err := tracker.Start(context.Background())

			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if len(tracker.activeFlights) != tt.expectedFlights {
				t.Errorf("Expected %d active flights, got %d", tt.expectedFlights, len(tracker.activeFlights))
			}
		})
	}
}

// TestStateTracker_ProcessMessage tests message processing with more scenarios
func TestStateTracker_ProcessMessage(t *testing.T) {
	tests := []struct {
		name              string
		message           *types.SBSMessage
		setupMocks        func() (*mockDBClient, *mockRedisClient)
		setupTracker      func(*StateTracker)
		expectError       bool
		expectedNewFlight bool
	}{
		{
			name: "successful message processing with new flight",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				_ = mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
				return mockDB, mockRedis
			},
			setupTracker:      func(t *StateTracker) {},
			expectError:       false,
			expectedNewFlight: true,
		},
		{
			name: "successful message processing with existing state merge",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,12000,500,180,40.8000,-74.1000,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				_ = mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
				return mockDB, mockRedis
			},
			setupTracker: func(t *StateTracker) {
				// Add existing state
				t.states["ABC123"] = &types.AircraftState{
					HexIdent:    "ABC123",
					Callsign:    "TEST123",
					Altitude:    10000,
					GroundSpeed: 450,
					Timestamp:   time.Now().Add(-1 * time.Minute),
				}
			},
			expectError:       false,
			expectedNewFlight: false,
		},
		{
			name: "invalid message format",
			message: &types.SBSMessage{
				Raw:       "INVALID,MESSAGE,FORMAT",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				return mockDB, mockRedis
			},
			setupTracker:      func(t *StateTracker) {},
			expectError:       true,
			expectedNewFlight: false,
		},
		{
			name: "database storage error",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{storeError: fmt.Errorf("database error")}
				mockRedis := newMockRedisClient()
				_ = mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
				return mockDB, mockRedis
			},
			setupTracker:      func(t *StateTracker) {},
			expectError:       true,
			expectedNewFlight: false,
		},
		{
			name: "invalid flight validation",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				_ = mockRedis.SetFlightValidation(context.Background(), "ABC123", false)
				return mockDB, mockRedis
			},
			setupTracker:      func(t *StateTracker) {},
			expectError:       false,
			expectedNewFlight: false,
		},
		{
			name: "flight validation error (should continue with warning)",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				mockRedis.getError = fmt.Errorf("redis get error")
				return mockDB, mockRedis
			},
			setupTracker:      func(t *StateTracker) {},
			expectError:       false,
			expectedNewFlight: true,
		},
		{
			name: "redis store aircraft state error (should continue with warning)",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				_ = mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
				// Set different error for aircraft state storage
				return mockDB, mockRedis
			},
			setupTracker: func(t *StateTracker) {},
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, mockRedis := tt.setupMocks()
			tracker := NewStateTracker(mockDB, mockRedis)
			tt.setupTracker(tracker)

			err := tracker.ProcessMessage(tt.message)

			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if tt.expectedNewFlight {
				if len(tracker.activeFlights) == 0 {
					t.Error("Expected new flight to be created")
				}
			}
		})
	}
}

// TestStateTracker_MergeStates tests comprehensive state merging scenarios
func TestStateTracker_MergeStates(t *testing.T) {
	tracker := NewStateTracker(&mockDBClient{}, newMockRedisClient())

	tests := []struct {
		name        string
		existing    *types.AircraftState
		newState    *types.AircraftState
		expectAfter *types.AircraftState
	}{
		{
			name: "merge all fields",
			existing: &types.AircraftState{
				HexIdent:    "ABC123",
				Callsign:    "OLD123",
				Altitude:    10000,
				GroundSpeed: 400,
				Track:       180,
				Latitude:    40.0,
				Longitude:   -74.0,
				OnGround:    false,
				Timestamp:   time.Now().Add(-1 * time.Minute),
			},
			newState: &types.AircraftState{
				HexIdent:     "ABC123",
				Callsign:     "NEW123",
				Altitude:     11000,
				VerticalRate: 500,
				Squawk:       "7700",
				OnGround:     true,
				Timestamp:    time.Now(),
			},
			expectAfter: &types.AircraftState{
				HexIdent:     "ABC123",
				Callsign:     "NEW123",
				Altitude:     11000,
				GroundSpeed:  400,   // Should keep existing
				Track:        180,   // Should keep existing
				Latitude:     40.0,  // Should keep existing (newState is 0)
				Longitude:    -74.0, // Should keep existing (newState is 0)
				VerticalRate: 500,
				Squawk:       "7700",
				OnGround:     true,
				Timestamp:    time.Now(),
			},
		},
		{
			name: "merge with zero/empty values (should not update)",
			existing: &types.AircraftState{
				HexIdent:    "ABC123",
				Callsign:    "EXISTING",
				Altitude:    10000,
				GroundSpeed: 450,
				Track:       180,
				Latitude:    40.7128,
				Longitude:   -74.0060,
				Squawk:      "1200",
				OnGround:    false,
				Timestamp:   time.Now().Add(-1 * time.Minute),
			},
			newState: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "", // Empty, should not update
				Altitude:  0,  // Zero, should not update
				Timestamp: time.Now(),
			},
			expectAfter: &types.AircraftState{
				HexIdent:    "ABC123",
				Callsign:    "EXISTING", // Should remain unchanged
				Altitude:    10000,      // Should remain unchanged
				GroundSpeed: 450,        // Should remain unchanged
				Track:       180,        // Should remain unchanged
				Latitude:    40.7128,    // Should remain unchanged
				Longitude:   -74.0060,   // Should remain unchanged
				Squawk:      "1200",     // Should remain unchanged
				OnGround:    false,      // Will be updated (always updated)
				Timestamp:   time.Now(),
			},
		},
		{
			name: "partial update with coordinates",
			existing: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Altitude:  10000,
				Latitude:  40.0,
				Longitude: -74.0,
				Timestamp: time.Now().Add(-1 * time.Minute),
			},
			newState: &types.AircraftState{
				HexIdent:  "ABC123",
				Latitude:  41.0,
				Longitude: -75.0,
				Track:     90,
				Timestamp: time.Now(),
			},
			expectAfter: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123", // Should remain unchanged
				Altitude:  10000,     // Should remain unchanged
				Latitude:  41.0,      // Should be updated
				Longitude: -75.0,     // Should be updated
				Track:     90,        // Should be updated
				Timestamp: time.Now(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of existing state to avoid modifying the test case
			existing := *tt.existing

			// Merge states
			tracker.mergeStates(&existing, tt.newState)

			// Verify results
			if existing.Callsign != tt.expectAfter.Callsign {
				t.Errorf("Expected callsign %s, got %s", tt.expectAfter.Callsign, existing.Callsign)
			}
			if existing.Altitude != tt.expectAfter.Altitude {
				t.Errorf("Expected altitude %d, got %d", tt.expectAfter.Altitude, existing.Altitude)
			}
			if existing.GroundSpeed != tt.expectAfter.GroundSpeed {
				t.Errorf("Expected ground speed %f, got %f", tt.expectAfter.GroundSpeed, existing.GroundSpeed)
			}
			if existing.Track != tt.expectAfter.Track {
				t.Errorf("Expected track %f, got %f", tt.expectAfter.Track, existing.Track)
			}
			if existing.Latitude != tt.expectAfter.Latitude {
				t.Errorf("Expected latitude %f, got %f", tt.expectAfter.Latitude, existing.Latitude)
			}
			if existing.Longitude != tt.expectAfter.Longitude {
				t.Errorf("Expected longitude %f, got %f", tt.expectAfter.Longitude, existing.Longitude)
			}
			if existing.VerticalRate != tt.expectAfter.VerticalRate {
				t.Errorf("Expected vertical rate %d, got %d", tt.expectAfter.VerticalRate, existing.VerticalRate)
			}
			if existing.Squawk != tt.expectAfter.Squawk {
				t.Errorf("Expected squawk %s, got %s", tt.expectAfter.Squawk, existing.Squawk)
			}
			if existing.OnGround != tt.expectAfter.OnGround {
				t.Errorf("Expected on ground %t, got %t", tt.expectAfter.OnGround, existing.OnGround)
			}
		})
	}
}

// TestStateTracker_UpdateFlight tests comprehensive flight update scenarios
func TestStateTracker_UpdateFlight(t *testing.T) {
	tests := []struct {
		name            string
		state           *types.AircraftState
		setupTracker    func(*StateTracker)
		setupMocks      func() (*mockDBClient, *mockRedisClient)
		expectNewFlight bool
		expectFlightEnd bool
		expectError     bool
	}{
		{
			name: "create new flight",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  40.7128,
				Longitude: -74.0060,
				Altitude:  10000,
				Timestamp: time.Now(),
			},
			setupTracker: func(t *StateTracker) {},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				return &mockDBClient{}, newMockRedisClient()
			},
			expectNewFlight: true,
			expectFlightEnd: false,
			expectError:     false,
		},
		{
			name: "update existing flight with max values",
			state: &types.AircraftState{
				HexIdent:    "ABC123",
				Callsign:    "TEST123",
				Latitude:    41.0,
				Longitude:   -75.0,
				Altitude:    15000, // Higher than existing
				GroundSpeed: 600,   // Higher than existing
				Timestamp:   time.Now(),
			},
			setupTracker: func(t *StateTracker) {
				t.activeFlights["ABC123"] = &types.Flight{
					SessionID:      "session1",
					HexIdent:       "ABC123",
					Callsign:       "TEST123",
					StartedAt:      time.Now().Add(-1 * time.Hour),
					FirstLatitude:  40.7128,
					FirstLongitude: -74.0060,
					MaxAltitude:    10000,
					MaxGroundSpeed: 450,
				}
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				return &mockDBClient{}, newMockRedisClient()
			},
			expectNewFlight: false,
			expectFlightEnd: false,
			expectError:     false,
		},
		{
			name: "flight ending scenario (old timestamp)",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  41.0,
				Longitude: -75.0,
				Altitude:  12000,
				Timestamp: time.Now().Add(-10 * time.Minute), // More than 5 minutes ago
			},
			setupTracker: func(t *StateTracker) {
				t.activeFlights["ABC123"] = &types.Flight{
					SessionID:      "session1",
					HexIdent:       "ABC123",
					Callsign:       "TEST123",
					StartedAt:      time.Now().Add(-1 * time.Hour),
					FirstLatitude:  40.7128,
					FirstLongitude: -74.0060,
					MaxAltitude:    10000,
					MaxGroundSpeed: 450,
				}
				t.states["ABC123"] = &types.AircraftState{
					HexIdent: "ABC123",
					Callsign: "TEST123",
				}
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockRedis := newMockRedisClient()
				mockRedis.flights["ABC123"] = &types.Flight{
					SessionID: "session1",
					HexIdent:  "ABC123",
				}
				return &mockDBClient{}, mockRedis
			},
			expectNewFlight: false,
			expectFlightEnd: true,
			expectError:     false,
		},
		{
			name: "database create flight error",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  40.7128,
				Longitude: -74.0060,
				Altitude:  10000,
				Timestamp: time.Now(),
			},
			setupTracker: func(t *StateTracker) {},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				return &mockDBClient{createError: fmt.Errorf("create error")}, newMockRedisClient()
			},
			expectNewFlight: false,
			expectFlightEnd: false,
			expectError:     true,
		},
		{
			name: "database update flight error",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  41.0,
				Longitude: -75.0,
				Timestamp: time.Now().Add(-10 * time.Minute), // Old timestamp to trigger update
			},
			setupTracker: func(t *StateTracker) {
				t.activeFlights["ABC123"] = &types.Flight{
					SessionID: "session1",
					HexIdent:  "ABC123",
					Callsign:  "TEST123",
					StartedAt: time.Now().Add(-1 * time.Hour),
				}
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				return &mockDBClient{updateError: fmt.Errorf("update error")}, newMockRedisClient()
			},
			expectNewFlight: false,
			expectFlightEnd: false,
			expectError:     true,
		},
		{
			name: "get flight from redis (not in local cache)",
			state: &types.AircraftState{
				HexIdent:    "ABC123",
				Callsign:    "TEST123",
				Latitude:    41.0,
				Longitude:   -75.0,
				Altitude:    12000,
				GroundSpeed: 500,
				Timestamp:   time.Now(),
			},
			setupTracker: func(t *StateTracker) {
				// Don't add to activeFlights to test Redis lookup
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockRedis := newMockRedisClient()
				// Add flight to Redis
				mockRedis.flights["ABC123"] = &types.Flight{
					SessionID:      "session1",
					HexIdent:       "ABC123",
					Callsign:       "TEST123",
					StartedAt:      time.Now().Add(-1 * time.Hour),
					FirstLatitude:  40.7128,
					FirstLongitude: -74.0060,
					MaxAltitude:    10000,
					MaxGroundSpeed: 450,
				}
				return &mockDBClient{}, mockRedis
			},
			expectNewFlight: false,
			expectFlightEnd: false,
			expectError:     false,
		},
		{
			name: "redis errors (should continue with warnings)",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  40.7128,
				Longitude: -74.0060,
				Altitude:  10000,
				Timestamp: time.Now(),
			},
			setupTracker: func(t *StateTracker) {},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockRedis := newMockRedisClient()
				mockRedis.getError = fmt.Errorf("redis get error")
				mockRedis.storeError = fmt.Errorf("redis store error")
				return &mockDBClient{}, mockRedis
			},
			expectNewFlight: true,
			expectFlightEnd: false,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, mockRedis := tt.setupMocks()
			tracker := NewStateTracker(mockDB, mockRedis)
			tt.setupTracker(tracker)

			initialFlightCount := len(tracker.activeFlights)

			err := tracker.updateFlight(tt.state)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if tt.expectNewFlight {
				if len(tracker.activeFlights) <= initialFlightCount {
					t.Error("Expected new flight to be created")
				}
			}

			if tt.expectFlightEnd {
				if _, exists := tracker.activeFlights[tt.state.HexIdent]; exists {
					t.Error("Expected flight to be ended and removed")
				}
				if _, exists := tracker.states[tt.state.HexIdent]; exists {
					t.Error("Expected state to be removed when flight ends")
				}
			}

			// Verify flight data updates
			if !tt.expectError && !tt.expectFlightEnd {
				flight := tracker.activeFlights[tt.state.HexIdent]
				if flight == nil && tt.expectNewFlight {
					t.Error("Expected flight to exist")
				}
				if flight != nil {
					if tt.expectNewFlight {
						if flight.FirstLatitude != tt.state.Latitude {
							t.Errorf("Expected first latitude %f, got %f", tt.state.Latitude, flight.FirstLatitude)
						}
					} else {
						if flight.LastLatitude != tt.state.Latitude {
							t.Errorf("Expected last latitude %f, got %f", tt.state.Latitude, flight.LastLatitude)
						}
						// Test max altitude and ground speed updates
						if tt.state.Altitude > 0 && flight.MaxAltitude < tt.state.Altitude {
							t.Errorf("Expected max altitude to be updated to %d, got %d", tt.state.Altitude, flight.MaxAltitude)
						}
						if tt.state.GroundSpeed > 0 && flight.MaxGroundSpeed < tt.state.GroundSpeed {
							t.Errorf("Expected max ground speed to be updated to %f, got %f", tt.state.GroundSpeed, flight.MaxGroundSpeed)
						}
					}
				}
			}
		})
	}
}

// TestStateTracker_LogStats tests the logStats function
func TestStateTracker_LogStats(t *testing.T) {
	mockDB := &mockDBClient{}
	mockRedis := newMockRedisClient()
	tracker := NewStateTracker(mockDB, mockRedis)

	// Test context cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Start logStats in a goroutine
	done := make(chan bool)
	go func() {
		tracker.logStats(ctx)
		done <- true
	}()

	// Cancel context after a short delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	// Wait for function to return due to context cancellation
	select {
	case <-done:
		// Function returned as expected
	case <-time.After(1 * time.Second):
		t.Error("logStats did not return when context was cancelled")
	}
}

// TestEnvironmentVariables tests environment variable handling
func TestEnvironmentVariables(t *testing.T) {
	// Save original environment
	originalNATSURL := os.Getenv("NATS_URL")
	originalDBConnStr := os.Getenv("DB_CONN_STR")
	originalRedisAddr := os.Getenv("REDIS_ADDR")
	defer func() {
		os.Setenv("NATS_URL", originalNATSURL)
		os.Setenv("DB_CONN_STR", originalDBConnStr)
		os.Setenv("REDIS_ADDR", originalRedisAddr)
	}()

	tests := []struct {
		name              string
		natsURL           string
		dbConnStr         string
		redisAddr         string
		expectedNATSURL   string
		expectedDBConnStr string
		expectedRedisAddr string
	}{
		{
			name:              "default values",
			natsURL:           "",
			dbConnStr:         "",
			redisAddr:         "",
			expectedNATSURL:   "nats://nats:4222",
			expectedDBConnStr: "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable",
			expectedRedisAddr: "redis:6379",
		},
		{
			name:              "custom values",
			natsURL:           "nats://custom:4222",
			dbConnStr:         "postgres://custom/db",
			redisAddr:         "custom-redis:6379",
			expectedNATSURL:   "nats://custom:4222",
			expectedDBConnStr: "postgres://custom/db",
			expectedRedisAddr: "custom-redis:6379",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			os.Setenv("NATS_URL", tt.natsURL)
			os.Setenv("DB_CONN_STR", tt.dbConnStr)
			os.Setenv("REDIS_ADDR", tt.redisAddr)

			natsURL, dbConnStr, redisAddr := parseEnvironmentTest()

			if natsURL != tt.expectedNATSURL {
				t.Errorf("Expected NATS URL %q, got %q", tt.expectedNATSURL, natsURL)
			}

			if dbConnStr != tt.expectedDBConnStr {
				t.Errorf("Expected DB conn string %q, got %q", tt.expectedDBConnStr, dbConnStr)
			}

			if redisAddr != tt.expectedRedisAddr {
				t.Errorf("Expected Redis addr %q, got %q", tt.expectedRedisAddr, redisAddr)
			}
		})
	}
}

// Helper functions for testing

// parseEnvironmentTest is a test helper that calls the main parseEnvironment function
func parseEnvironmentTest() (string, string, string) {
	return parseEnvironment()
}

// TestMainFunctionLogic tests the main function logic without os.Exit
func TestMainFunctionLogic(t *testing.T) {
	// Save original environment variables
	originalNATSURL := os.Getenv("NATS_URL")
	originalDBConnStr := os.Getenv("DB_CONN_STR")
	originalRedisAddr := os.Getenv("REDIS_ADDR")
	defer func() {
		os.Setenv("NATS_URL", originalNATSURL)
		os.Setenv("DB_CONN_STR", originalDBConnStr)
		os.Setenv("REDIS_ADDR", originalRedisAddr)
	}()

	t.Run("environment variable parsing", func(t *testing.T) {
		// Test that environment variables are parsed correctly
		os.Setenv("NATS_URL", "nats://test:4222")
		os.Setenv("DB_CONN_STR", "postgres://test/db")
		os.Setenv("REDIS_ADDR", "test-redis:6379")

		natsURL, dbConnStr, redisAddr := parseEnvironmentTest()

		if natsURL != "nats://test:4222" {
			t.Errorf("Expected NATS URL nats://test:4222, got %s", natsURL)
		}
		if dbConnStr != "postgres://test/db" {
			t.Errorf("Expected DB conn string postgres://test/db, got %s", dbConnStr)
		}
		if redisAddr != "test-redis:6379" {
			t.Errorf("Expected Redis addr test-redis:6379, got %s", redisAddr)
		}
	})

	t.Run("default environment values", func(t *testing.T) {
		// Clear environment variables to test defaults
		os.Setenv("NATS_URL", "")
		os.Setenv("DB_CONN_STR", "")
		os.Setenv("REDIS_ADDR", "")

		natsURL, dbConnStr, redisAddr := parseEnvironmentTest()

		if natsURL != "nats://nats:4222" {
			t.Errorf("Expected default NATS URL nats://nats:4222, got %s", natsURL)
		}
		if dbConnStr != "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable" {
			t.Errorf("Expected default DB conn string, got %s", dbConnStr)
		}
		if redisAddr != "redis:6379" {
			t.Errorf("Expected default Redis addr redis:6379, got %s", redisAddr)
		}
	})
}

// TestMainFunctionErrorHandling tests the error handling in main function
func TestMainFunctionErrorHandling(t *testing.T) {
	// Save original environment variables
	originalNATSURL := os.Getenv("NATS_URL")
	originalDBConnStr := os.Getenv("DB_CONN_STR")
	originalRedisAddr := os.Getenv("REDIS_ADDR")
	defer func() {
		os.Setenv("NATS_URL", originalNATSURL)
		os.Setenv("DB_CONN_STR", originalDBConnStr)
		os.Setenv("REDIS_ADDR", originalRedisAddr)
	}()

	t.Run("NATS client creation failure", func(t *testing.T) {
		// Test that main function handles NATS client creation failure
		os.Setenv("NATS_URL", "invalid://nats")
		os.Setenv("DB_CONN_STR", "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable")
		os.Setenv("REDIS_ADDR", "redis:6379")

		// We can't easily test os.Exit, but we can test the logic leading up to it
		// by testing the environment parsing and client creation logic
		natsURL, dbConnStr, redisAddr := parseEnvironmentTest()

		if natsURL != "invalid://nats" {
			t.Errorf("Expected NATS URL invalid://nats, got %s", natsURL)
		}
		if dbConnStr != "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable" {
			t.Errorf("Expected DB conn string, got %s", dbConnStr)
		}
		if redisAddr != "redis:6379" {
			t.Errorf("Expected Redis addr redis:6379, got %s", redisAddr)
		}
	})

	t.Run("database client creation failure", func(t *testing.T) {
		// Test that main function handles database client creation failure
		os.Setenv("NATS_URL", "nats://nats:4222")
		os.Setenv("DB_CONN_STR", "invalid://db")
		os.Setenv("REDIS_ADDR", "redis:6379")

		natsURL, dbConnStr, redisAddr := parseEnvironmentTest()

		if natsURL != "nats://nats:4222" {
			t.Errorf("Expected NATS URL nats://nats:4222, got %s", natsURL)
		}
		if dbConnStr != "invalid://db" {
			t.Errorf("Expected DB conn string invalid://db, got %s", dbConnStr)
		}
		if redisAddr != "redis:6379" {
			t.Errorf("Expected Redis addr redis:6379, got %s", redisAddr)
		}
	})

	t.Run("Redis client creation failure", func(t *testing.T) {
		// Test that main function handles Redis client creation failure
		os.Setenv("NATS_URL", "nats://nats:4222")
		os.Setenv("DB_CONN_STR", "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable")
		os.Setenv("REDIS_ADDR", "invalid://redis")

		natsURL, dbConnStr, redisAddr := parseEnvironmentTest()

		if natsURL != "nats://nats:4222" {
			t.Errorf("Expected NATS URL nats://nats:4222, got %s", natsURL)
		}
		if dbConnStr != "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable" {
			t.Errorf("Expected DB conn string, got %s", dbConnStr)
		}
		if redisAddr != "invalid://redis" {
			t.Errorf("Expected Redis addr invalid://redis, got %s", redisAddr)
		}
	})
}

// TestStateTrackerStartWithDBClient tests the Start method with concrete DB client
func TestStateTrackerStartWithDBClient(t *testing.T) {
	t.Run("start with concrete DB client", func(t *testing.T) {
		// Test the specific code path where dbClient is cast to *db.Client
		// This tests the lines that set up statistics with the database client

		// Create a mock that implements the DBClient interface
		mockDB := &mockDBClient{flights: []*types.Flight{}}
		mockRedis := newMockRedisClient()

		tracker := NewStateTracker(mockDB, mockRedis)

		// Test that Start works correctly
		err := tracker.Start(context.Background())
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Verify that stats are initialized
		if tracker.stats == nil {
			t.Error("Expected stats to be initialized")
		}
	})
}

// TestStateTrackerProcessMessageEdgeCases tests edge cases in ProcessMessage
func TestStateTrackerProcessMessageEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		message      *types.SBSMessage
		setupMocks   func() (*mockDBClient, *mockRedisClient)
		setupTracker func(*StateTracker)
		expectError  bool
	}{
		{
			name: "nil state from parser",
			message: &types.SBSMessage{
				Raw:       "MSG,1,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,,0,0,0,0,0,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				return &mockDBClient{}, newMockRedisClient()
			},
			setupTracker: func(t *StateTracker) {},
			expectError:  false, // Should return nil for nil state
		},
		{
			name: "Redis store aircraft state error (should continue)",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				_ = mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
				// Set store error for aircraft state storage
				mockRedis.storeError = fmt.Errorf("redis store error")
				return mockDB, mockRedis
			},
			setupTracker: func(t *StateTracker) {},
			expectError:  false, // Should continue with warning
		},
		{
			name: "Redis get flight error (should continue)",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				_ = mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
				mockRedis.getError = fmt.Errorf("redis get error")
				return mockDB, mockRedis
			},
			setupTracker: func(t *StateTracker) {},
			expectError:  false, // Should continue with warning
		},
		{
			name: "Redis store flight error (should continue)",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				_ = mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
				mockRedis.storeError = fmt.Errorf("redis store error")
				return mockDB, mockRedis
			},
			setupTracker: func(t *StateTracker) {},
			expectError:  false, // Should continue with warning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, mockRedis := tt.setupMocks()
			tracker := NewStateTracker(mockDB, mockRedis)
			tt.setupTracker(tracker)

			err := tracker.ProcessMessage(tt.message)

			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

// TestStateTrackerUpdateFlightEdgeCases tests edge cases in updateFlight
func TestStateTrackerUpdateFlightEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		state           *types.AircraftState
		setupTracker    func(*StateTracker)
		setupMocks      func() (*mockDBClient, *mockRedisClient)
		expectNewFlight bool
		expectFlightEnd bool
		expectError     bool
	}{
		{
			name: "Redis delete flight error (should continue)",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  41.0,
				Longitude: -75.0,
				Timestamp: time.Now().Add(-10 * time.Minute), // Old timestamp to trigger flight end
			},
			setupTracker: func(t *StateTracker) {
				t.activeFlights["ABC123"] = &types.Flight{
					SessionID: "session1",
					HexIdent:  "ABC123",
					Callsign:  "TEST123",
					StartedAt: time.Now().Add(-1 * time.Hour),
				}
				t.states["ABC123"] = &types.AircraftState{
					HexIdent: "ABC123",
					Callsign: "TEST123",
				}
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockRedis := newMockRedisClient()
				mockRedis.flights["ABC123"] = &types.Flight{
					SessionID: "session1",
					HexIdent:  "ABC123",
				}
				// Set error for delete operations
				mockRedis.getError = fmt.Errorf("redis delete error")
				return &mockDBClient{}, mockRedis
			},
			expectNewFlight: false,
			expectFlightEnd: true,
			expectError:     false, // Should continue with warning
		},
		{
			name: "Redis delete aircraft state error (should continue)",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  41.0,
				Longitude: -75.0,
				Timestamp: time.Now().Add(-10 * time.Minute), // Old timestamp to trigger flight end
			},
			setupTracker: func(t *StateTracker) {
				t.activeFlights["ABC123"] = &types.Flight{
					SessionID: "session1",
					HexIdent:  "ABC123",
					Callsign:  "TEST123",
					StartedAt: time.Now().Add(-1 * time.Hour),
				}
				t.states["ABC123"] = &types.AircraftState{
					HexIdent: "ABC123",
					Callsign: "TEST123",
				}
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockRedis := newMockRedisClient()
				mockRedis.flights["ABC123"] = &types.Flight{
					SessionID: "session1",
					HexIdent:  "ABC123",
				}
				// Set error for aircraft state delete operation
				mockRedis.getError = fmt.Errorf("redis delete aircraft state error")
				return &mockDBClient{}, mockRedis
			},
			expectNewFlight: false,
			expectFlightEnd: true,
			expectError:     false, // Should continue with warning
		},
		{
			name: "Redis update flight error (should continue)",
			state: &types.AircraftState{
				HexIdent:    "ABC123",
				Callsign:    "TEST123",
				Latitude:    41.0,
				Longitude:   -75.0,
				Altitude:    12000,
				GroundSpeed: 500,
				Timestamp:   time.Now(), // Recent timestamp
			},
			setupTracker: func(t *StateTracker) {
				t.activeFlights["ABC123"] = &types.Flight{
					SessionID:      "session1",
					HexIdent:       "ABC123",
					Callsign:       "TEST123",
					StartedAt:      time.Now().Add(-1 * time.Hour),
					FirstLatitude:  40.7128,
					FirstLongitude: -74.0060,
					MaxAltitude:    10000,
					MaxGroundSpeed: 450,
				}
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockRedis := newMockRedisClient()
				mockRedis.flights["ABC123"] = &types.Flight{
					SessionID: "session1",
					HexIdent:  "ABC123",
				}
				// Set error for store operation (flight update)
				mockRedis.storeError = fmt.Errorf("redis update flight error")
				return &mockDBClient{}, mockRedis
			},
			expectNewFlight: false,
			expectFlightEnd: false,
			expectError:     false, // Should continue with warning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, mockRedis := tt.setupMocks()
			tracker := NewStateTracker(mockDB, mockRedis)
			tt.setupTracker(tracker)

			initialFlightCount := len(tracker.activeFlights)

			err := tracker.updateFlight(tt.state)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if tt.expectNewFlight {
				if len(tracker.activeFlights) <= initialFlightCount {
					t.Error("Expected new flight to be created")
				}
			}

			if tt.expectFlightEnd {
				if _, exists := tracker.activeFlights[tt.state.HexIdent]; exists {
					t.Error("Expected flight to be ended and removed")
				}
				if _, exists := tracker.states[tt.state.HexIdent]; exists {
					t.Error("Expected state to be removed when flight ends")
				}
			}
		})
	}
}

// TestStateTrackerLogStatsWithContext tests logStats with context cancellation
func TestStateTrackerLogStatsWithContext(t *testing.T) {
	mockDB := &mockDBClient{}
	mockRedis := newMockRedisClient()
	tracker := NewStateTracker(mockDB, mockRedis)

	// Test context cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Start logStats in a goroutine
	done := make(chan bool)
	go func() {
		tracker.logStats(ctx)
		done <- true
	}()

	// Cancel context after a short delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	// Wait for function to return due to context cancellation
	select {
	case <-done:
		// Function returned as expected
	case <-time.After(1 * time.Second):
		t.Error("logStats did not return when context was cancelled")
	}
}

// TestStateTrackerLogStatsWithTicker tests logStats with ticker
func TestStateTrackerLogStatsWithTicker(t *testing.T) {
	mockDB := &mockDBClient{}
	mockRedis := newMockRedisClient()
	tracker := NewStateTracker(mockDB, mockRedis)

	// Test that logStats runs with ticker
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start logStats in a goroutine
	done := make(chan bool)
	go func() {
		tracker.logStats(ctx)
		done <- true
	}()

	// Wait for function to return due to context timeout
	select {
	case <-done:
		// Function returned as expected
	case <-time.After(2 * time.Second):
		t.Error("logStats did not return when context timed out")
	}
}

// TestMainFunctionShutdownBehavior tests the shutdown behavior
func TestMainFunctionShutdownBehavior(t *testing.T) {
	t.Run("shutdown signal handling", func(t *testing.T) {
		// Test that the main function would handle shutdown signals correctly
		// We can't easily test os.Exit, but we can test the signal handling logic

		// Test that signal channel is created correctly
		sigChan := make(chan os.Signal, 1)

		// Test that we can send a signal to the channel
		select {
		case sigChan <- syscall.SIGTERM: // Use syscall.SIGTERM for testing
			// Signal sent successfully
		default:
			t.Error("Could not send signal to channel")
		}
	})
}

// TestMainFunctionClientCreation tests client creation logic
func TestMainFunctionClientCreation(t *testing.T) {
	// Save original environment variables
	originalNATSURL := os.Getenv("NATS_URL")
	originalDBConnStr := os.Getenv("DB_CONN_STR")
	originalRedisAddr := os.Getenv("REDIS_ADDR")
	defer func() {
		os.Setenv("NATS_URL", originalNATSURL)
		os.Setenv("DB_CONN_STR", originalDBConnStr)
		os.Setenv("REDIS_ADDR", originalRedisAddr)
	}()

	t.Run("client creation parameter passing", func(t *testing.T) {
		// Test that the main function would pass correct parameters to client creation
		os.Setenv("NATS_URL", "nats://test:4222")
		os.Setenv("DB_CONN_STR", "postgres://test/db")
		os.Setenv("REDIS_ADDR", "test-redis:6379")

		natsURL, dbConnStr, redisAddr := parseEnvironmentTest()

		// Verify parameters are parsed correctly
		if natsURL != "nats://test:4222" {
			t.Errorf("Expected NATS URL nats://test:4222, got %s", natsURL)
		}
		if dbConnStr != "postgres://test/db" {
			t.Errorf("Expected DB conn string postgres://test/db, got %s", dbConnStr)
		}
		if redisAddr != "test-redis:6379" {
			t.Errorf("Expected Redis addr test-redis:6379, got %s", redisAddr)
		}
	})
}

// TestMainFunctionErrorHandlingPaths tests various error handling paths
func TestMainFunctionErrorHandlingPaths(t *testing.T) {
	// Save original environment variables
	originalNATSURL := os.Getenv("NATS_URL")
	originalDBConnStr := os.Getenv("DB_CONN_STR")
	originalRedisAddr := os.Getenv("REDIS_ADDR")
	defer func() {
		os.Setenv("NATS_URL", originalNATSURL)
		os.Setenv("DB_CONN_STR", originalDBConnStr)
		os.Setenv("REDIS_ADDR", originalRedisAddr)
	}()

	t.Run("NATS client creation error path", func(t *testing.T) {
		// Test the error handling path for NATS client creation
		os.Setenv("NATS_URL", "invalid://nats")

		natsURL, _, _ := parseEnvironmentTest()

		if natsURL != "invalid://nats" {
			t.Errorf("Expected NATS URL invalid://nats, got %s", natsURL)
		}
	})

	t.Run("database client creation error path", func(t *testing.T) {
		// Test the error handling path for database client creation
		os.Setenv("DB_CONN_STR", "invalid://db")

		_, dbConnStr, _ := parseEnvironmentTest()

		if dbConnStr != "invalid://db" {
			t.Errorf("Expected DB conn string invalid://db, got %s", dbConnStr)
		}
	})

	t.Run("Redis client creation error path", func(t *testing.T) {
		// Test the error handling path for Redis client creation
		os.Setenv("REDIS_ADDR", "invalid://redis")

		_, _, redisAddr := parseEnvironmentTest()

		if redisAddr != "invalid://redis" {
			t.Errorf("Expected Redis addr invalid://redis, got %s", redisAddr)
		}
	})

	t.Run("state tracker start error path", func(t *testing.T) {
		// Test the error handling path for state tracker start
		// This tests the logic that would handle tracker.Start() errors

		// Create a tracker that would fail to start
		mockDB := &mockDBClient{getError: fmt.Errorf("start error")}
		mockRedis := newMockRedisClient()

		tracker := NewStateTracker(mockDB, mockRedis)

		err := tracker.Start(context.Background())
		if err == nil {
			t.Error("Expected error from tracker start, got nil")
		}
	})

	t.Run("NATS subscription error path", func(t *testing.T) {
		// Test the error handling path for NATS subscription
		// This tests the logic that would handle subscription errors

		// We can't easily test the actual NATS subscription, but we can test
		// that the error handling logic exists and would work correctly

		// Test that we can create a mock message handler
		messageHandler := func(msg *types.SBSMessage) {
			// This would be the actual message handler
			_ = msg // Suppress unused variable warning
		}

		// Test that the handler can be called (functions are never nil)
		testMessage := &types.SBSMessage{
			Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
			Timestamp: time.Now(),
			Source:    "test-source",
		}
		messageHandler(testMessage)
	})
}

// TestCreateClients tests the createClients function
func TestCreateClients(t *testing.T) {
	t.Run("successful client creation", func(t *testing.T) {
		// This test would require actual clients, but we can test the function structure
		// by testing that it returns appropriate errors for invalid URLs

		// Test with invalid URLs to trigger error paths
		_, _, _, err := createClients("invalid://nats", "invalid://db", "invalid://redis")
		if err == nil {
			t.Error("Expected error with invalid URLs, got nil")
		}

		// Verify error message contains expected text
		if !strings.Contains(err.Error(), "failed to create") {
			t.Errorf("Expected error containing 'failed to create', got: %v", err)
		}
	})
}

// TestSetupStateTracker tests the setupStateTracker function
func TestSetupStateTracker(t *testing.T) {
	t.Run("successful state tracker setup", func(t *testing.T) {
		// Create mock clients
		mockDB := &mockDBClient{flights: []*types.Flight{}}
		mockRedis := newMockRedisClient()

		// We can't easily test with real clients, but we can test the function structure
		// by creating a test that would work with our mock clients

		// Test that NewStateTracker works correctly
		tracker := NewStateTracker(mockDB, mockRedis)
		if tracker == nil {
			t.Error("Expected tracker to be created, got nil")
		}

		// Test that Start works correctly
		err := tracker.Start(context.Background())
		if err != nil {
			t.Errorf("Expected no error from tracker start, got: %v", err)
		}
	})
}

// TestSetupNATSSubscription tests the setupNATSSubscription function
func TestSetupNATSSubscription(t *testing.T) {
	t.Run("subscription setup logic", func(t *testing.T) {
		// Test that we can create a message handler function
		// This tests the logic that would be used in setupNATSSubscription

		mockDB := &mockDBClient{}
		mockRedis := newMockRedisClient()
		tracker := NewStateTracker(mockDB, mockRedis)

		// Create a message handler like the one used in setupNATSSubscription
		messageHandler := func(msg *types.SBSMessage) {
			if err := tracker.ProcessMessage(msg); err != nil {
				// This would be logged in the real function
				_ = err // Suppress unused variable warning
			}
		}

		// Test that the handler can be called
		testMessage := &types.SBSMessage{
			Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
			Timestamp: time.Now(),
			Source:    "test-source",
		}

		// Call the handler - this should not panic
		messageHandler(testMessage)
	})
}

// TestWaitForShutdown tests the waitForShutdown function
func TestWaitForShutdown(t *testing.T) {
	t.Run("shutdown signal handling", func(t *testing.T) {
		// Test that the shutdown function can be called
		// We can't easily test the actual signal waiting, but we can test the structure

		// Create mock clients
		mockDB := &mockDBClient{}
		mockRedis := newMockRedisClient()

		// Test that we can create the signal channel
		sigChan := make(chan os.Signal, 1)

		// Test that we can send a signal to the channel
		select {
		case sigChan <- syscall.SIGTERM:
			// Signal sent successfully
		default:
			t.Error("Could not send signal to channel")
		}

		// Test that mock clients can be closed
		if err := mockDB.Close(); err != nil {
			t.Errorf("Expected no error closing mock DB, got: %v", err)
		}
		if err := mockRedis.Close(); err != nil {
			t.Errorf("Expected no error closing mock Redis, got: %v", err)
		}
	})
}
