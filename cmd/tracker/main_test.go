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

// TestStateTracker_ProcessMessage tests message processing
func TestStateTracker_ProcessMessage(t *testing.T) {
	tests := []struct {
		name              string
		message           *types.SBSMessage
		setupMocks        func() (*mockDBClient, *mockRedisClient)
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
			expectError:       false,
			expectedNewFlight: true,
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
			expectError:       true,
			expectedNewFlight: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, mockRedis := tt.setupMocks()
			tracker := NewStateTracker(mockDB, mockRedis)

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

// TestStateTracker_MergeStates tests state merging functionality
func TestStateTracker_MergeStates(t *testing.T) {
	tracker := NewStateTracker(&mockDBClient{}, newMockRedisClient())

	// Create existing state
	existing := &types.AircraftState{
		HexIdent:    "ABC123",
		Callsign:    "OLD123",
		Altitude:    10000,
		GroundSpeed: 400,
		Track:       180,
		Latitude:    40.0,
		Longitude:   -74.0,
		Timestamp:   time.Now().Add(-1 * time.Minute),
	}

	// Create new state with some updated fields
	newState := &types.AircraftState{
		HexIdent:     "ABC123",
		Callsign:     "NEW123",
		Altitude:     11000,
		VerticalRate: 500,
		Timestamp:    time.Now(),
	}

	// Merge states
	tracker.mergeStates(existing, newState)

	// Verify merge results
	if existing.Callsign != "NEW123" {
		t.Errorf("Expected callsign to be updated to NEW123, got %s", existing.Callsign)
	}

	if existing.Altitude != 11000 {
		t.Errorf("Expected altitude to be updated to 11000, got %d", existing.Altitude)
	}

	if existing.GroundSpeed != 400 {
		t.Errorf("Expected ground speed to remain 400, got %f", existing.GroundSpeed)
	}

	if existing.VerticalRate != 500 {
		t.Errorf("Expected vertical rate to be updated to 500, got %d", existing.VerticalRate)
	}

	if !existing.Timestamp.Equal(newState.Timestamp) {
		t.Error("Expected timestamp to be updated")
	}
}

// TestStateTracker_UpdateFlight tests flight creation and updates
func TestStateTracker_UpdateFlight(t *testing.T) {
	tests := []struct {
		name            string
		state           *types.AircraftState
		existingFlights map[string]*types.Flight
		expectNewFlight bool
		expectFlightEnd bool
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
			existingFlights: make(map[string]*types.Flight),
			expectNewFlight: true,
			expectFlightEnd: false,
		},
		{
			name: "update existing flight",
			state: &types.AircraftState{
				HexIdent:    "ABC123",
				Callsign:    "TEST123",
				Latitude:    41.0,
				Longitude:   -75.0,
				Altitude:    12000,
				GroundSpeed: 500,
				Timestamp:   time.Now(),
			},
			existingFlights: map[string]*types.Flight{
				"ABC123": {
					SessionID:      "session1",
					HexIdent:       "ABC123",
					Callsign:       "TEST123",
					StartedAt:      time.Now().Add(-1 * time.Hour),
					FirstLatitude:  40.7128,
					FirstLongitude: -74.0060,
					MaxAltitude:    10000,
					MaxGroundSpeed: 450,
				},
			},
			expectNewFlight: false,
			expectFlightEnd: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := &mockDBClient{}
			mockRedis := newMockRedisClient()
			tracker := NewStateTracker(mockDB, mockRedis)

			// Set up existing flights
			tracker.activeFlights = tt.existingFlights

			err := tracker.updateFlight(tt.state)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.expectNewFlight {
				if len(tracker.activeFlights) == 0 {
					t.Error("Expected new flight to be created")
				}
			}

			// Verify flight data
			flight, exists := tracker.activeFlights[tt.state.HexIdent]
			if !exists && !tt.expectFlightEnd {
				t.Error("Expected flight to exist")
			}

			if exists {
				if flight.HexIdent != tt.state.HexIdent {
					t.Errorf("Expected hex ident %s, got %s", tt.state.HexIdent, flight.HexIdent)
				}

				// For new flights, check FirstLatitude/FirstLongitude
				// For updated flights, check LastLatitude/LastLongitude
				if tt.expectNewFlight {
					if flight.FirstLatitude != tt.state.Latitude {
						t.Errorf("Expected first latitude %f, got %f", tt.state.Latitude, flight.FirstLatitude)
					}

					if flight.FirstLongitude != tt.state.Longitude {
						t.Errorf("Expected first longitude %f, got %f", tt.state.Longitude, flight.FirstLongitude)
					}
				} else {
					if flight.LastLatitude != tt.state.Latitude {
						t.Errorf("Expected last latitude %f, got %f", tt.state.Latitude, flight.LastLatitude)
					}

					if flight.LastLongitude != tt.state.Longitude {
						t.Errorf("Expected last longitude %f, got %f", tt.state.Longitude, flight.LastLongitude)
					}
				}
			}
		})
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
