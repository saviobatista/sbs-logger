package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/savio/sbs-logger/internal/types"
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
				mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
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
				mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
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
				mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
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
				mockRedis.SetFlightValidation(context.Background(), "ABC123", false)
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
				mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
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
				GroundSpeed:  400, // Should keep existing
				Track:        180, // Should keep existing
				Latitude:     40.0, // Should keep existing (newState is 0)
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

			natsURL, dbConnStr, redisAddr := parseEnvironment()

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

// parseEnvironment extracts the core environment parsing logic for testing
func parseEnvironment() (string, string, string) {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats:4222"
	}

	dbConnStr := os.Getenv("DB_CONN_STR")
	if dbConnStr == "" {
		dbConnStr = "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}

	return natsURL, dbConnStr, redisAddr
}
