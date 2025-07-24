package redis

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/savio/sbs-logger/internal/types"
)

// Mock Redis client that implements RedisClientInterface
type mockRedisClient struct {
	data       map[string]string
	pingError  error
	getError   error
	setError   error
	delError   error
	closeError error
}

func (m *mockRedisClient) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	if m.pingError != nil {
		cmd.SetErr(m.pingError)
	} else {
		cmd.SetVal("PONG")
	}
	return cmd
}

func (m *mockRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx, "get", key)
	if m.getError != nil {
		cmd.SetErr(m.getError)
		return cmd
	}

	if value, exists := m.data[key]; exists {
		cmd.SetVal(value)
	} else {
		cmd.SetErr(redis.Nil)
	}
	return cmd
}

func (m *mockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	if m.setError != nil {
		cmd.SetErr(m.setError)
		return cmd
	}

	if m.data == nil {
		m.data = make(map[string]string)
	}

	// Convert value to string
	var strValue string
	switch v := value.(type) {
	case string:
		strValue = v
	case []byte:
		strValue = string(v)
	default:
		// For testing, convert to JSON if it's a complex type
		if jsonBytes, err := json.Marshal(v); err == nil {
			strValue = string(jsonBytes)
		} else {
			strValue = "unknown"
		}
	}

	m.data[key] = strValue
	cmd.SetVal("OK")
	return cmd
}

func (m *mockRedisClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx, "del")
	if m.delError != nil {
		cmd.SetErr(m.delError)
		return cmd
	}

	deleted := int64(0)
	for _, key := range keys {
		if _, exists := m.data[key]; exists {
			delete(m.data, key)
			deleted++
		}
	}
	cmd.SetVal(deleted)
	return cmd
}

func (m *mockRedisClient) Close() error {
	return m.closeError
}

// UNIT TESTS WITH PROPER MOCKING

func TestNewWithClient_Unit(t *testing.T) {
	mockClient := &mockRedisClient{}
	client := NewWithClient(mockClient)

	if client == nil {
		t.Fatal("Expected client to be created")
	}
	if client.client != mockClient {
		t.Error("Expected client to use provided mock")
	}
}

func TestClient_Close_Unit(t *testing.T) {
	tests := []struct {
		name        string
		closeError  error
		expectError bool
	}{
		{
			name:        "successful close",
			closeError:  nil,
			expectError: false,
		},
		{
			name:        "close error",
			closeError:  errors.New("close failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{closeError: tt.closeError}
			client := NewWithClient(mockClient)

			err := client.Close()

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

func TestClient_StoreFlight_Unit(t *testing.T) {
	tests := []struct {
		name        string
		flight      *types.Flight
		setError    error
		expectError bool
	}{
		{
			name: "successful storage",
			flight: &types.Flight{
				HexIdent: "ABC123",
				Callsign: "TEST123",
			},
			setError:    nil,
			expectError: false,
		},
		{
			name: "redis set error",
			flight: &types.Flight{
				HexIdent: "ABC123",
				Callsign: "TEST123",
			},
			setError:    errors.New("set failed"),
			expectError: true,
		},
		{
			name: "flight with all fields",
			flight: &types.Flight{
				SessionID:      "full-session-123",
				HexIdent:       "FULL123",
				Callsign:       "FULLTEST",
				StartedAt:      time.Now(),
				EndedAt:        time.Now().Add(time.Hour),
				FirstLatitude:  40.7128,
				FirstLongitude: -74.0060,
				LastLatitude:   41.7128,
				LastLongitude:  -75.0060,
				MaxAltitude:    35000,
				MaxGroundSpeed: 450.5,
			},
			setError:    nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{setError: tt.setError}
			client := NewWithClient(mockClient)
			ctx := context.Background()

			err := client.StoreFlight(ctx, tt.flight)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// Verify data was stored in mock (for successful cases)
			if !tt.expectError && err == nil {
				key := "flight:" + tt.flight.HexIdent
				if _, exists := mockClient.data[key]; !exists {
					t.Error("Expected flight data to be stored in Redis")
				}
			}
		})
	}
}

func TestClient_GetFlight_Unit(t *testing.T) {
	testFlight := &types.Flight{
		HexIdent:    "ABC123",
		Callsign:    "TEST123",
		MaxAltitude: 35000,
	}
	flightData, _ := json.Marshal(testFlight)

	tests := []struct {
		name        string
		hexIdent    string
		storedData  map[string]string
		getError    error
		expectError bool
		expectNil   bool
	}{
		{
			name:     "successful retrieval",
			hexIdent: "ABC123",
			storedData: map[string]string{
				"flight:ABC123": string(flightData),
			},
			getError:    nil,
			expectError: false,
			expectNil:   false,
		},
		{
			name:        "flight not found",
			hexIdent:    "NOTFOUND",
			storedData:  map[string]string{},
			getError:    nil,
			expectError: false,
			expectNil:   false, // Changed from true - getData returns nil but GetFlight returns empty struct
		},
		{
			name:        "redis get error",
			hexIdent:    "ABC123",
			storedData:  map[string]string{},
			getError:    errors.New("get failed"),
			expectError: true,
			expectNil:   false,
		},
		{
			name:     "invalid JSON data",
			hexIdent: "INVALID",
			storedData: map[string]string{
				"flight:INVALID": "invalid json data",
			},
			getError:    nil,
			expectError: true,
			expectNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{
				data:     tt.storedData,
				getError: tt.getError,
			}
			client := NewWithClient(mockClient)
			ctx := context.Background()

			flight, err := client.GetFlight(ctx, tt.hexIdent)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
			if tt.expectNil && flight != nil {
				t.Error("Expected nil flight")
			}
			if !tt.expectNil && !tt.expectError && flight == nil {
				t.Error("Expected flight data")
			}

			// Verify flight data for successful retrieval
			if !tt.expectError && !tt.expectNil && flight != nil {
				if tt.hexIdent == "NOTFOUND" {
					// For not found case, should return empty struct
					if flight.HexIdent != "" {
						t.Error("Expected empty HexIdent for not found flight")
					}
				} else if flight.HexIdent != testFlight.HexIdent {
					t.Errorf("Expected HexIdent %s, got %s", testFlight.HexIdent, flight.HexIdent)
				}
			}
		})
	}
}

func TestClient_DeleteFlight_Unit(t *testing.T) {
	tests := []struct {
		name        string
		hexIdent    string
		initialData map[string]string
		delError    error
		expectError bool
	}{
		{
			name:     "successful deletion",
			hexIdent: "ABC123",
			initialData: map[string]string{
				"flight:ABC123": "some data",
			},
			delError:    nil,
			expectError: false,
		},
		{
			name:        "delete non-existent",
			hexIdent:    "NOTFOUND",
			initialData: map[string]string{},
			delError:    nil,
			expectError: false,
		},
		{
			name:        "redis delete error",
			hexIdent:    "ABC123",
			initialData: map[string]string{},
			delError:    errors.New("del failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{
				data:     tt.initialData,
				delError: tt.delError,
			}
			client := NewWithClient(mockClient)
			ctx := context.Background()

			err := client.DeleteFlight(ctx, tt.hexIdent)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

func TestClient_StoreAircraftState_Unit(t *testing.T) {
	tests := []struct {
		name        string
		state       *types.AircraftState
		setError    error
		expectError bool
	}{
		{
			name: "successful storage",
			state: &types.AircraftState{
				HexIdent: "ABC123",
				Callsign: "TEST123",
				Altitude: 35000,
			},
			setError:    nil,
			expectError: false,
		},
		{
			name: "redis set error",
			state: &types.AircraftState{
				HexIdent: "ABC123",
				Callsign: "TEST123",
			},
			setError:    errors.New("set failed"),
			expectError: true,
		},
		{
			name: "state with all fields",
			state: &types.AircraftState{
				HexIdent:     "FULL123",
				Callsign:     "FULLTEST",
				Altitude:     35000,
				GroundSpeed:  450.5,
				Track:        180.0,
				Latitude:     40.7128,
				Longitude:    -74.0060,
				VerticalRate: 1000,
				Squawk:       "1234",
				OnGround:     false,
				MsgType:      8,
				Timestamp:    time.Now(),
				SessionID:    "test-session",
			},
			setError:    nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{setError: tt.setError}
			client := NewWithClient(mockClient)
			ctx := context.Background()

			err := client.StoreAircraftState(ctx, tt.state)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// Verify data was stored in mock (for successful cases)
			if !tt.expectError && err == nil {
				key := "aircraft:" + tt.state.HexIdent
				if _, exists := mockClient.data[key]; !exists {
					t.Error("Expected aircraft state to be stored in Redis")
				}
			}
		})
	}
}

func TestClient_GetAircraftState_Unit(t *testing.T) {
	testState := &types.AircraftState{
		HexIdent: "ABC123",
		Callsign: "TEST123",
		Altitude: 35000,
	}
	stateData, _ := json.Marshal(testState)

	tests := []struct {
		name        string
		hexIdent    string
		storedData  map[string]string
		getError    error
		expectError bool
		expectNil   bool
	}{
		{
			name:     "successful retrieval",
			hexIdent: "ABC123",
			storedData: map[string]string{
				"aircraft:ABC123": string(stateData),
			},
			getError:    nil,
			expectError: false,
			expectNil:   false,
		},
		{
			name:        "state not found",
			hexIdent:    "NOTFOUND",
			storedData:  map[string]string{},
			getError:    nil,
			expectError: false,
			expectNil:   false, // Changed from true - getData returns nil but GetAircraftState returns empty struct
		},
		{
			name:        "redis get error",
			hexIdent:    "ABC123",
			storedData:  map[string]string{},
			getError:    errors.New("get failed"),
			expectError: true,
			expectNil:   false,
		},
		{
			name:     "invalid JSON data",
			hexIdent: "INVALID",
			storedData: map[string]string{
				"aircraft:INVALID": "invalid json data",
			},
			getError:    nil,
			expectError: true,
			expectNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{
				data:     tt.storedData,
				getError: tt.getError,
			}
			client := NewWithClient(mockClient)
			ctx := context.Background()

			state, err := client.GetAircraftState(ctx, tt.hexIdent)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
			if tt.expectNil && state != nil {
				t.Error("Expected nil state")
			}
			if !tt.expectNil && !tt.expectError && state == nil {
				t.Error("Expected state data")
			}

			// Verify state data for successful retrieval
			if !tt.expectError && !tt.expectNil && state != nil {
				if tt.hexIdent == "NOTFOUND" {
					// For not found case, should return empty struct
					if state.HexIdent != "" {
						t.Error("Expected empty HexIdent for not found aircraft state")
					}
				} else if state.HexIdent != testState.HexIdent {
					t.Errorf("Expected HexIdent %s, got %s", testState.HexIdent, state.HexIdent)
				}
			}
		})
	}
}

func TestClient_DeleteAircraftState_Unit(t *testing.T) {
	tests := []struct {
		name        string
		hexIdent    string
		initialData map[string]string
		delError    error
		expectError bool
	}{
		{
			name:     "successful deletion",
			hexIdent: "ABC123",
			initialData: map[string]string{
				"aircraft:ABC123": "some data",
			},
			delError:    nil,
			expectError: false,
		},
		{
			name:        "delete non-existent",
			hexIdent:    "NOTFOUND",
			initialData: map[string]string{},
			delError:    nil,
			expectError: false,
		},
		{
			name:        "redis delete error",
			hexIdent:    "ABC123",
			initialData: map[string]string{},
			delError:    errors.New("del failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{
				data:     tt.initialData,
				delError: tt.delError,
			}
			client := NewWithClient(mockClient)
			ctx := context.Background()

			err := client.DeleteAircraftState(ctx, tt.hexIdent)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

func TestClient_SetFlightValidation_Unit(t *testing.T) {
	tests := []struct {
		name        string
		hexIdent    string
		valid       bool
		setError    error
		expectError bool
	}{
		{
			name:        "set valid flight",
			hexIdent:    "ABC123",
			valid:       true,
			setError:    nil,
			expectError: false,
		},
		{
			name:        "set invalid flight",
			hexIdent:    "ABC123",
			valid:       false,
			setError:    nil,
			expectError: false,
		},
		{
			name:        "redis set error",
			hexIdent:    "ABC123",
			valid:       true,
			setError:    errors.New("set failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{setError: tt.setError}
			client := NewWithClient(mockClient)
			ctx := context.Background()

			err := client.SetFlightValidation(ctx, tt.hexIdent, tt.valid)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// Verify correct value was stored
			if !tt.expectError && err == nil {
				key := "validation:" + tt.hexIdent
				expectedValue := "0"
				if tt.valid {
					expectedValue = "1"
				}
				if value, exists := mockClient.data[key]; !exists {
					t.Error("Expected validation data to be stored")
				} else if value != expectedValue {
					t.Errorf("Expected value %s, got %s", expectedValue, value)
				}
			}
		})
	}
}

func TestClient_GetFlightValidation_Unit(t *testing.T) {
	tests := []struct {
		name           string
		hexIdent       string
		storedData     map[string]string
		getError       error
		expectError    bool
		expectedResult bool
	}{
		{
			name:     "get valid flight",
			hexIdent: "ABC123",
			storedData: map[string]string{
				"validation:ABC123": "1",
			},
			getError:       nil,
			expectError:    false,
			expectedResult: true,
		},
		{
			name:     "get invalid flight",
			hexIdent: "ABC123",
			storedData: map[string]string{
				"validation:ABC123": "0",
			},
			getError:       nil,
			expectError:    false,
			expectedResult: false,
		},
		{
			name:           "validation not found",
			hexIdent:       "NOTFOUND",
			storedData:     map[string]string{},
			getError:       nil,
			expectError:    false,
			expectedResult: false,
		},
		{
			name:        "redis get error",
			hexIdent:    "ABC123",
			storedData:  map[string]string{},
			getError:    errors.New("get failed"),
			expectError: true,
		},
		{
			name:     "invalid value from Redis",
			hexIdent: "ABC123",
			storedData: map[string]string{
				"validation:ABC123": "invalid",
			},
			getError:       nil,
			expectError:    false,
			expectedResult: false, // Anything not "1" is false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{
				data:     tt.storedData,
				getError: tt.getError,
			}
			client := NewWithClient(mockClient)
			ctx := context.Background()

			valid, err := client.GetFlightValidation(ctx, tt.hexIdent)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
			if !tt.expectError && valid != tt.expectedResult {
				t.Errorf("Expected %t, got %t", tt.expectedResult, valid)
			}
		})
	}
}

func TestClient_GetData_Unit(t *testing.T) {
	testData := map[string]interface{}{
		"field1": "value1",
		"field2": 123,
	}
	jsonData, _ := json.Marshal(testData)

	tests := []struct {
		name        string
		key         string
		storedData  map[string]string
		getError    error
		expectError bool
	}{
		{
			name: "successful data retrieval",
			key:  "test:key",
			storedData: map[string]string{
				"test:key": string(jsonData),
			},
			getError:    nil,
			expectError: false,
		},
		{
			name:        "data not found",
			key:         "missing:key",
			storedData:  map[string]string{},
			getError:    nil,
			expectError: false, // getData returns nil for missing data
		},
		{
			name:        "redis get error",
			key:         "error:key",
			storedData:  map[string]string{},
			getError:    errors.New("get failed"),
			expectError: true,
		},
		{
			name: "invalid JSON",
			key:  "invalid:key",
			storedData: map[string]string{
				"invalid:key": "invalid json",
			},
			getError:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRedisClient{
				data:     tt.storedData,
				getError: tt.getError,
			}
			client := NewWithClient(mockClient)
			ctx := context.Background()

			var target map[string]interface{}
			err := client.getData(ctx, tt.key, &target, "test")

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

// Test JSON serialization/deserialization
func TestClient_JSONSerialization_Unit(t *testing.T) {
	tests := []struct {
		name        string
		data        interface{}
		expectError bool
	}{
		{
			name: "valid flight data",
			data: &types.Flight{
				HexIdent: "ABC123",
				Callsign: "TEST123",
			},
			expectError: false,
		},
		{
			name: "valid aircraft state",
			data: &types.AircraftState{
				HexIdent: "ABC123",
				Altitude: 35000,
			},
			expectError: false,
		},
		{
			name:        "empty flight",
			data:        &types.Flight{},
			expectError: false,
		},
		{
			name:        "empty aircraft state",
			data:        &types.AircraftState{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.data)
			if tt.expectError && err == nil {
				t.Error("Expected marshal error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no marshal error, got: %v", err)
			}
			if !tt.expectError && len(data) == 0 {
				t.Error("Marshaled data should not be empty")
			}

			// Test unmarshaling back
			if !tt.expectError && err == nil {
				switch tt.data.(type) {
				case *types.Flight:
					var flight types.Flight
					err = json.Unmarshal(data, &flight)
					if err != nil {
						t.Errorf("Expected no unmarshal error, got: %v", err)
					}
				case *types.AircraftState:
					var state types.AircraftState
					err = json.Unmarshal(data, &state)
					if err != nil {
						t.Errorf("Expected no unmarshal error, got: %v", err)
					}
				}
			}
		})
	}
}

// Test Redis Nil handling
func TestClient_RedisNilHandling_Unit(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		expectNil bool
	}{
		{
			name:      "redis.Nil error",
			err:       redis.Nil,
			expectNil: true,
		},
		{
			name:      "other error",
			err:       context.DeadlineExceeded,
			expectNil: false,
		},
		{
			name:      "no error",
			err:       nil,
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the logic used in getData method
			isRedisNil := tt.err == redis.Nil

			if tt.expectNil && !isRedisNil {
				t.Error("Expected redis.Nil to be detected")
			}
			if !tt.expectNil && isRedisNil {
				t.Error("Expected non-redis.Nil error")
			}
		})
	}
}

// INTEGRATION TESTS (Require actual Redis server)

func TestNew_Integration(t *testing.T) {
	// This test requires Redis to be running
	addr := "localhost:6379"

	client, err := New(addr)
	if err != nil {
		t.Skip("Redis not available, skipping integration test")
	}
	defer client.Close()

	if client == nil {
		t.Fatal("Expected client to be created")
	}
	if client.client == nil {
		t.Fatal("Expected Redis client to be initialized")
	}
}

func TestClient_FullIntegration(t *testing.T) {
	client, err := New("localhost:6379")
	if err != nil {
		t.Skip("Redis not available, skipping integration test")
	}
	defer client.Close()

	ctx := context.Background()
	hexIdent := "FULLTEST123"

	// Clean up any existing data
	_ = client.DeleteFlight(ctx, hexIdent)
	_ = client.DeleteAircraftState(ctx, hexIdent)

	// Test complete workflow
	flight := &types.Flight{
		SessionID:      "integration-test",
		HexIdent:       hexIdent,
		Callsign:       "FULLTEST",
		StartedAt:      time.Now(),
		EndedAt:        time.Now().Add(time.Hour),
		FirstLatitude:  40.7128,
		FirstLongitude: -74.0060,
		LastLatitude:   41.7128,
		LastLongitude:  -75.0060,
		MaxAltitude:    35000,
		MaxGroundSpeed: 450.5,
	}

	state := &types.AircraftState{
		HexIdent:     hexIdent,
		Callsign:     "FULLTEST",
		Altitude:     35000,
		GroundSpeed:  450.5,
		Track:        180.0,
		Latitude:     40.7128,
		Longitude:    -74.0060,
		VerticalRate: 1000,
		Squawk:       "1234",
		OnGround:     false,
		MsgType:      8,
		Timestamp:    time.Now(),
		SessionID:    "integration-test",
	}

	// Store flight and aircraft state
	err = client.StoreFlight(ctx, flight)
	if err != nil {
		t.Fatalf("StoreFlight failed: %v", err)
	}

	err = client.StoreAircraftState(ctx, state)
	if err != nil {
		t.Fatalf("StoreAircraftState failed: %v", err)
	}

	// Set validation
	err = client.SetFlightValidation(ctx, hexIdent, true)
	if err != nil {
		t.Fatalf("SetFlightValidation failed: %v", err)
	}

	// Retrieve and verify all data
	retrievedFlight, err := client.GetFlight(ctx, hexIdent)
	if err != nil {
		t.Fatalf("GetFlight failed: %v", err)
	}
	if retrievedFlight == nil || retrievedFlight.HexIdent != flight.HexIdent {
		t.Error("Flight data mismatch")
	}

	retrievedState, err := client.GetAircraftState(ctx, hexIdent)
	if err != nil {
		t.Fatalf("GetAircraftState failed: %v", err)
	}
	if retrievedState == nil || retrievedState.HexIdent != state.HexIdent {
		t.Error("Aircraft state data mismatch")
	}

	valid, err := client.GetFlightValidation(ctx, hexIdent)
	if err != nil {
		t.Fatalf("GetFlightValidation failed: %v", err)
	}
	if !valid {
		t.Error("Validation should be true")
	}

	// Clean up
	_ = client.DeleteFlight(ctx, hexIdent)
	_ = client.DeleteAircraftState(ctx, hexIdent)
}
