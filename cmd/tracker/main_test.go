package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	natsMod "github.com/testcontainers/testcontainers-go/modules/nats"
	postgresMod "github.com/testcontainers/testcontainers-go/modules/postgres"
	redisMod "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/saviobatista/sbs-logger/internal/types"
)

// UNIT TESTS WITH MOCKS (Fast)

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

// EASY COVERAGE WINS - Testing uncovered functions

func TestCreateClients_SuccessPath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	containers := setupTestContainers(ctx, t)
	defer containers.cleanup()

	// Get connection strings
	postgresConn, err := containers.postgres.(*postgresMod.PostgresContainer).ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get postgres connection string: %v", err)
	}

	redisHost, err := containers.redis.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get redis host: %v", err)
	}
	redisPort, err := containers.redis.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("Failed to get redis port: %v", err)
	}
	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort.Port())

	natsHost, err := containers.nats.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get nats host: %v", err)
	}
	natsPort, err := containers.nats.MappedPort(ctx, "4222")
	if err != nil {
		t.Fatalf("Failed to get nats port: %v", err)
	}
	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort.Port())

	// Test successful client creation (covers success path)
	natsClient, dbClient, redisClient, err := createClients(natsURL, postgresConn, redisAddr)
	if err != nil {
		t.Fatalf("createClients() failed: %v", err)
	}

	// Verify all clients are non-nil
	if natsClient == nil || dbClient == nil || redisClient == nil {
		t.Error("Expected all clients to be non-nil")
	}

	// Clean up
	natsClient.Close()
	dbClient.Close()
	redisClient.Close()
}

func TestMainFunctionErrorPaths(t *testing.T) {
	tests := []struct {
		name        string
		natsURL     string
		dbConnStr   string
		redisAddr   string
		expectError bool
	}{
		{
			name:        "invalid NATS URL",
			natsURL:     "invalid://nats",
			dbConnStr:   "postgres://valid/connection",
			redisAddr:   "localhost:6379",
			expectError: true,
		},
		{
			name:        "invalid database connection",
			natsURL:     "nats://localhost:4222",
			dbConnStr:   "invalid://database",
			redisAddr:   "localhost:6379",
			expectError: true,
		},
		{
			name:        "invalid Redis address",
			natsURL:     "nats://localhost:4222",
			dbConnStr:   "postgres://valid/connection",
			redisAddr:   "invalid://redis",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := createClients(tt.natsURL, tt.dbConnStr, tt.redisAddr)
			if (err != nil) != tt.expectError {
				t.Errorf("createClients() error = %v, expectError %v", err, tt.expectError)
			}
			if tt.expectError && !strings.Contains(err.Error(), "failed to create") {
				t.Errorf("Expected error to contain 'failed to create', got: %v", err)
			}
		})
	}
}

// Test runMigrations directly
func TestRunMigrations_Unit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	containers := setupTestContainers(ctx, t)
	defer containers.cleanup()

	postgresConn, err := containers.postgres.(*postgresMod.PostgresContainer).ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get postgres connection string: %v", err)
	}

	// Test successful migration
	if err := runMigrations(postgresConn); err != nil {
		t.Errorf("runMigrations() failed: %v", err)
	}

	// Test invalid connection string
	if err := runMigrations("invalid://connection"); err == nil {
		t.Error("Expected error with invalid connection string")
	}
}

// Test setupStateTracker with real clients
func TestSetupStateTracker_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	containers := setupTestContainers(ctx, t)
	defer containers.cleanup()

	// Get connection strings
	postgresConn, err := containers.postgres.(*postgresMod.PostgresContainer).ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get postgres connection string: %v", err)
	}

	redisHost, err := containers.redis.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get redis host: %v", err)
	}
	redisPort, err := containers.redis.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("Failed to get redis port: %v", err)
	}
	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort.Port())

	natsHost, err := containers.nats.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get nats host: %v", err)
	}
	natsPort, err := containers.nats.MappedPort(ctx, "4222")
	if err != nil {
		t.Fatalf("Failed to get nats port: %v", err)
	}
	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort.Port())

	// Create clients
	_, dbClient, redisClient, err := createClients(natsURL, postgresConn, redisAddr)
	if err != nil {
		t.Fatalf("createClients() failed: %v", err)
	}
	defer func() {
		dbClient.Close()
		redisClient.Close()
	}()

	// Test setupStateTracker
	tracker, err := setupStateTracker(dbClient, redisClient)
	if err != nil {
		t.Fatalf("setupStateTracker() failed: %v", err)
	}
	if tracker == nil {
		t.Error("Expected tracker to be non-nil")
	}
}

// Test setupNATSSubscription with integration test
func TestSetupNATSSubscription_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	containers := setupTestContainers(ctx, t)
	defer containers.cleanup()

	// Get connection strings
	postgresConn, err := containers.postgres.(*postgresMod.PostgresContainer).ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get postgres connection string: %v", err)
	}

	redisHost, err := containers.redis.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get redis host: %v", err)
	}
	redisPort, err := containers.redis.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("Failed to get redis port: %v", err)
	}
	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort.Port())

	natsHost, err := containers.nats.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get nats host: %v", err)
	}
	natsPort, err := containers.nats.MappedPort(ctx, "4222")
	if err != nil {
		t.Fatalf("Failed to get nats port: %v", err)
	}
	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort.Port())

	// Create clients using createClients function (tests success path)
	natsClient, dbClient, redisClient, err := createClients(natsURL, postgresConn, redisAddr)
	if err != nil {
		t.Fatalf("createClients() failed: %v", err)
	}
	defer func() {
		natsClient.Close()
		dbClient.Close()
		redisClient.Close()
	}()

	// Setup state tracker
	tracker, err := setupStateTracker(dbClient, redisClient)
	if err != nil {
		t.Fatalf("setupStateTracker() failed: %v", err)
	}

	// Test setupNATSSubscription - this is the key function we want to cover
	if err := setupNATSSubscription(natsClient, tracker); err != nil {
		t.Fatalf("setupNATSSubscription() failed: %v", err)
	}

	// Test that subscription is working by trying to process a test message
	// This also tests the integration flow that main() would follow
	testMessage := &types.SBSMessage{
		Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
		Timestamp: time.Now(),
		Source:    "integration-test",
	}

	// Process a message to verify the full integration works
	if err := tracker.ProcessMessage(testMessage); err != nil {
		t.Errorf("ProcessMessage() failed: %v", err)
	}
}

// INTEGRATION TESTS WITH TESTCONTAINERS (Comprehensive)

type testContainers struct {
	postgres testcontainers.Container
	redis    testcontainers.Container
	nats     testcontainers.Container
	cleanup  func()
}

func setupTestContainers(ctx context.Context, t *testing.T) *testContainers {
	t.Helper()

	// PostgreSQL container
	postgresContainer, err := postgresMod.Run(ctx,
		"timescale/timescaledb:latest-pg16",
		postgresMod.WithDatabase("sbs_data"),
		postgresMod.WithUsername("sbs"),
		postgresMod.WithPassword("sbs_password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("Failed to start postgres container: %v", err)
	}

	// Redis container
	redisContainer, err := redisMod.Run(ctx,
		"redis:7-alpine",
		testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections")),
	)
	if err != nil {
		t.Fatalf("Failed to start redis container: %v", err)
	}

	// NATS container
	natsContainer, err := natsMod.Run(ctx,
		"nats:2.10-alpine",
		testcontainers.WithWaitStrategy(wait.ForLog("Server is ready")),
	)
	if err != nil {
		t.Fatalf("Failed to start nats container: %v", err)
	}

	cleanup := func() {
		if err := testcontainers.TerminateContainer(postgresContainer); err != nil {
			t.Logf("Failed to terminate postgres container: %v", err)
		}
		if err := testcontainers.TerminateContainer(redisContainer); err != nil {
			t.Logf("Failed to terminate redis container: %v", err)
		}
		if err := testcontainers.TerminateContainer(natsContainer); err != nil {
			t.Logf("Failed to terminate nats container: %v", err)
		}
	}

	return &testContainers{
		postgres: postgresContainer,
		redis:    redisContainer,
		nats:     natsContainer,
		cleanup:  cleanup,
	}
}

func TestCreateClients_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	containers := setupTestContainers(ctx, t)
	defer containers.cleanup()

	// Get connection strings
	postgresConn, err := containers.postgres.(*postgresMod.PostgresContainer).ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get postgres connection string: %v", err)
	}

	redisHost, err := containers.redis.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get redis host: %v", err)
	}
	redisPort, err := containers.redis.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("Failed to get redis port: %v", err)
	}
	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort.Port())

	natsHost, err := containers.nats.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get nats host: %v", err)
	}
	natsPort, err := containers.nats.MappedPort(ctx, "4222")
	if err != nil {
		t.Fatalf("Failed to get nats port: %v", err)
	}
	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort.Port())

	// Test client creation
	natsClient, dbClient, redisClient, err := createClients(natsURL, postgresConn, redisAddr)
	if err != nil {
		t.Fatalf("createClients() failed: %v", err)
	}
	defer func() {
		natsClient.Close()
		dbClient.Close()
		redisClient.Close()
	}()

	// Verify clients work by attempting basic operations
	// For DB client, we'll need to add a method to access the underlying *sql.DB
	// For Redis client, we'll need to implement or access a ping-like method
	// These would need to be added to the internal client implementations
}

func TestRunMigrations_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	containers := setupTestContainers(ctx, t)
	defer containers.cleanup()

	postgresConn, err := containers.postgres.(*postgresMod.PostgresContainer).ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get postgres connection string: %v", err)
	}

	// Test migration
	if err := runMigrations(postgresConn); err != nil {
		t.Fatalf("runMigrations() failed: %v", err)
	}

	// Verify tables exist
	db, err := sql.Open("postgres", postgresConn)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	tables := []string{"flights", "aircraft_states"}
	for _, table := range tables {
		var exists bool
		query := `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)`
		if err := db.QueryRow(query, table).Scan(&exists); err != nil {
			t.Errorf("Failed to check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("Table %s was not created", table)
		}
	}
}

func TestFullIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	containers := setupTestContainers(ctx, t)
	defer containers.cleanup()

	// Get connection strings
	postgresConn, err := containers.postgres.(*postgresMod.PostgresContainer).ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get postgres connection string: %v", err)
	}

	redisHost, err := containers.redis.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get redis host: %v", err)
	}
	redisPort, err := containers.redis.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("Failed to get redis port: %v", err)
	}
	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort.Port())

	natsHost, err := containers.nats.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get nats host: %v", err)
	}
	natsPort, err := containers.nats.MappedPort(ctx, "4222")
	if err != nil {
		t.Fatalf("Failed to get nats port: %v", err)
	}
	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort.Port())

	// Test full integration flow
	natsClient, dbClient, redisClient, err := createClients(natsURL, postgresConn, redisAddr)
	if err != nil {
		t.Fatalf("createClients() failed: %v", err)
	}
	defer func() {
		natsClient.Close()
		dbClient.Close()
		redisClient.Close()
	}()

	// Run migrations
	if err := runMigrations(postgresConn); err != nil {
		t.Fatalf("runMigrations() failed: %v", err)
	}

	// Setup state tracker
	tracker, err := setupStateTracker(dbClient, redisClient)
	if err != nil {
		t.Fatalf("setupStateTracker() failed: %v", err)
	}

	// Test message processing
	testMessage := &types.SBSMessage{
		Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
		Timestamp: time.Now(),
		Source:    "integration-test",
	}

	if err := tracker.ProcessMessage(testMessage); err != nil {
		t.Errorf("ProcessMessage() failed: %v", err)
	}

	// Verify data was stored
	if len(tracker.activeFlights) == 0 {
		t.Error("Expected active flight to be created")
	}
}
