package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/types"
)

// UNIT TESTS WITH MOCKS (Fast - no external dependencies)

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
	for i, f := range m.flights {
		if f.SessionID == flight.SessionID {
			m.flights[i] = flight
			break
		}
	}
	return nil
}

func (m *mockDBClient) StoreAircraftState(state *types.AircraftState) error {
	return m.storeError
}

func (m *mockDBClient) Close() error { return nil }

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
		return true, nil
	}
	return valid, nil
}

func (m *mockRedisClient) Close() error { return nil }

// Unit Tests

func TestStateTracker_New(t *testing.T) {
	mockDB := &mockDBClient{}
	mockRedis := newMockRedisClient()

	tracker := NewStateTracker(mockDB, mockRedis)

	if tracker.db != mockDB || tracker.redis != mockRedis {
		t.Error("Expected clients to be set correctly")
	}
	if tracker.activeFlights == nil || tracker.states == nil || tracker.stats == nil {
		t.Error("Expected maps and stats to be initialized")
	}
}

func TestStateTracker_Start(t *testing.T) {
	tests := []struct {
		name        string
		mockDB      *mockDBClient
		expectError bool
	}{
		{
			name:        "successful start",
			mockDB:      &mockDBClient{flights: []*types.Flight{}},
			expectError: false,
		},
		{
			name:        "database error",
			mockDB:      &mockDBClient{getError: fmt.Errorf("db error")},
			expectError: true,
		},
		{
			name: "start with existing flights",
			mockDB: &mockDBClient{flights: []*types.Flight{
				{SessionID: "session1", HexIdent: "ABC123", Callsign: "TEST123"},
			}},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewStateTracker(tt.mockDB, newMockRedisClient())
			err := tracker.Start(context.Background())

			if (err != nil) != tt.expectError {
				t.Errorf("Start() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestStateTracker_ProcessMessage(t *testing.T) {
	tests := []struct {
		name        string
		message     *types.SBSMessage
		setupMocks  func() (*mockDBClient, *mockRedisClient)
		expectError bool
	}{
		{
			name: "valid message processing",
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
			expectError: false,
		},
		{
			name: "invalid message format",
			message: &types.SBSMessage{
				Raw:       "INVALID,MESSAGE,FORMAT",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				return &mockDBClient{}, newMockRedisClient()
			},
			expectError: true,
		},
		{
			name: "database storage error",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{storeError: fmt.Errorf("db error")}
				mockRedis := newMockRedisClient()
				_ = mockRedis.SetFlightValidation(context.Background(), "ABC123", true)
				return mockDB, mockRedis
			},
			expectError: true,
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
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, mockRedis := tt.setupMocks()
			tracker := NewStateTracker(mockDB, mockRedis)

			err := tracker.ProcessMessage(tt.message)
			if (err != nil) != tt.expectError {
				t.Errorf("ProcessMessage() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestStateTracker_UpdateFlight(t *testing.T) {
	tests := []struct {
		name            string
		state           *types.AircraftState
		setupTracker    func(*StateTracker)
		mockDB          *mockDBClient
		expectNewFlight bool
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
			setupTracker:    func(t *StateTracker) {},
			mockDB:          &mockDBClient{},
			expectNewFlight: true,
			expectError:     false,
		},
		{
			name: "update existing flight",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  41.0,
				Longitude: -75.0,
				Altitude:  12000,
				Timestamp: time.Now(),
			},
			setupTracker: func(t *StateTracker) {
				t.activeFlights["ABC123"] = &types.Flight{
					SessionID: "session1",
					HexIdent:  "ABC123",
					Callsign:  "TEST123",
					StartedAt: time.Now().Add(-1 * time.Hour),
				}
			},
			mockDB:          &mockDBClient{},
			expectNewFlight: false,
			expectError:     false,
		},
		{
			name: "database create error",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  40.7128,
				Longitude: -74.0060,
				Timestamp: time.Now(),
			},
			setupTracker:    func(t *StateTracker) {},
			mockDB:          &mockDBClient{createError: fmt.Errorf("create error")},
			expectNewFlight: false,
			expectError:     true,
		},
		{
			name: "flight ending scenario",
			state: &types.AircraftState{
				HexIdent:  "ABC123",
				Callsign:  "TEST123",
				Latitude:  41.0,
				Longitude: -75.0,
				Timestamp: time.Now().Add(-10 * time.Minute), // Old timestamp
			},
			setupTracker: func(t *StateTracker) {
				t.activeFlights["ABC123"] = &types.Flight{
					SessionID: "session1",
					HexIdent:  "ABC123",
					StartedAt: time.Now().Add(-1 * time.Hour),
				}
				t.states["ABC123"] = &types.AircraftState{HexIdent: "ABC123"}
			},
			mockDB:          &mockDBClient{},
			expectNewFlight: false,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewStateTracker(tt.mockDB, newMockRedisClient())
			tt.setupTracker(tracker)

			initialCount := len(tracker.activeFlights)
			err := tracker.updateFlight(tt.state)

			if (err != nil) != tt.expectError {
				t.Errorf("updateFlight() error = %v, expectError %v", err, tt.expectError)
			}

			if tt.expectNewFlight && len(tracker.activeFlights) <= initialCount {
				t.Error("Expected new flight to be created")
			}
		})
	}
}

func TestStateTracker_MergeStates(t *testing.T) {
	tracker := NewStateTracker(&mockDBClient{}, newMockRedisClient())

	tests := []struct {
		name     string
		existing *types.AircraftState
		newState *types.AircraftState
		checkFn  func(*testing.T, *types.AircraftState)
	}{
		{
			name: "merge all fields",
			existing: &types.AircraftState{
				HexIdent:    "ABC123",
				Callsign:    "OLD123",
				Altitude:    10000,
				GroundSpeed: 400,
				Timestamp:   time.Now().Add(-1 * time.Minute),
			},
			newState: &types.AircraftState{
				HexIdent:     "ABC123",
				Callsign:     "NEW123",
				Altitude:     11000,
				Track:        90,
				VerticalRate: 500,
				Squawk:       "7700",
				OnGround:     true,
				Timestamp:    time.Now(),
			},
			checkFn: func(t *testing.T, existing *types.AircraftState) {
				if existing.Callsign != "NEW123" {
					t.Errorf("Expected callsign NEW123, got %s", existing.Callsign)
				}
				if existing.Altitude != 11000 {
					t.Errorf("Expected altitude 11000, got %d", existing.Altitude)
				}
				if existing.Track != 90 {
					t.Errorf("Expected track 90, got %f", existing.Track)
				}
				if existing.VerticalRate != 500 {
					t.Errorf("Expected vertical rate 500, got %d", existing.VerticalRate)
				}
				if existing.Squawk != "7700" {
					t.Errorf("Expected squawk 7700, got %s", existing.Squawk)
				}
				if !existing.OnGround {
					t.Error("Expected OnGround to be true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker.mergeStates(tt.existing, tt.newState)
			tt.checkFn(t, tt.existing)
		})
	}
}

func TestLogStats(t *testing.T) {
	tracker := NewStateTracker(&mockDBClient{}, newMockRedisClient())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan bool)
	go func() {
		tracker.logStats(ctx)
		done <- true
	}()

	select {
	case <-done:
		// Function returned as expected
	case <-time.After(200 * time.Millisecond):
		t.Error("logStats did not return when context was cancelled")
	}
}

func TestParseEnvironment(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected [3]string
	}{
		{
			name:    "default values",
			envVars: map[string]string{},
			expected: [3]string{
				"nats://nats:4222",
				"postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable",
				"redis:6379",
			},
		},
		{
			name: "custom values",
			envVars: map[string]string{
				"NATS_URL":    "nats://custom:4222",
				"DB_CONN_STR": "postgres://custom/db",
				"REDIS_ADDR":  "custom-redis:6379",
			},
			expected: [3]string{
				"nats://custom:4222",
				"postgres://custom/db",
				"custom-redis:6379",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Backup original environment
			original := map[string]string{
				"NATS_URL":    os.Getenv("NATS_URL"),
				"DB_CONN_STR": os.Getenv("DB_CONN_STR"),
				"REDIS_ADDR":  os.Getenv("REDIS_ADDR"),
			}
			defer func() {
				for k, v := range original {
					os.Setenv(k, v)
				}
			}()

			// Set test environment
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			natsURL, dbConnStr, redisAddr := parseEnvironment()
			result := [3]string{natsURL, dbConnStr, redisAddr}

			if result != tt.expected {
				t.Errorf("parseEnvironment() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
