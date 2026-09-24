package db

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/saviobatista/sbs-logger/internal/types"
)

// UNIT TESTS WITH SQLMOCK (New comprehensive tests)

func TestNew_Unit(t *testing.T) {
	tests := []struct {
		name        string
		connStr     string
		expectError bool
	}{
		{
			name:        "valid connection string",
			connStr:     "postgres://user:password@localhost:5432/db?sslmode=disable",
			expectError: false,
		},
		{
			name:        "empty connection string",
			connStr:     "",
			expectError: false, // sql.Open doesn't validate immediately
		},
		{
			name:        "invalid driver name",
			connStr:     "invalid://connection:string",
			expectError: false, // sql.Open only validates driver name, postgres is still valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.connStr)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
			if !tt.expectError && client == nil {
				t.Error("Expected client to be created, got nil")
			}
			if client != nil && client.db == nil {
				t.Error("Expected database connection to be initialized")
			}
			if client != nil {
				_ = client.Close()
			}
		})
	}
}

func TestClient_Close_Unit(t *testing.T) {
	// Test with proper mock DB
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock DB: %v", err)
	}

	// Expect the close call
	mock.ExpectClose()

	client := &Client{db: db}
	err = client.Close()
	if err != nil {
		t.Errorf("Close() should not fail: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet expectations: %v", err)
	}
}

const activeFlightsQuery = `SELECT session_id, hex_ident, callsign, started_at, last_seen_at,
			first_latitude, first_longitude, last_latitude, last_longitude,
			max_altitude, max_ground_speed
		FROM flights
		WHERE ended_at IS NULL`

var activeFlightColumns = []string{
	"session_id", "hex_ident", "callsign", "started_at", "last_seen_at",
	"first_latitude", "first_longitude", "last_latitude", "last_longitude",
	"max_altitude", "max_ground_speed",
}

func TestClient_GetActiveFlights_Unit(t *testing.T) {
	started := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	seen := started.Add(20 * time.Minute)

	tests := []struct {
		name          string
		setupMock     func(sqlmock.Sqlmock)
		expectError   bool
		expectedCount int
		check         func(*testing.T, []*types.Flight)
	}{
		{
			name: "successful retrieval with flights",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(activeFlightColumns).
					AddRow("session1", "ABC123", "TEST123", started, seen, 40.7128, -74.0060, 41.0, -75.0, 35000, 450.5).
					AddRow("session2", "DEF456", "TEST456", started, seen, 42.0, -73.0, 43.0, -72.0, 30000, 400.0)
				mock.ExpectQuery(regexp.QuoteMeta(activeFlightsQuery)).WillReturnRows(rows)
			},
			expectedCount: 2,
			check: func(t *testing.T, flights []*types.Flight) {
				f := flights[0]
				if f.SessionID != "session1" || f.Callsign != "TEST123" || !f.LastSeenAt.Equal(seen) ||
					f.MaxAltitude != 35000 || f.MaxGroundSpeed != 450.5 || !f.EndedAt.IsZero() {
					t.Errorf("unexpected flight %+v", f)
				}
			},
		},
		{
			// Rows written by the old code or before migration 004 have NULL
			// columns. Scanning NULL into string/time.Time failed and kept the
			// tracker from starting.
			name: "NULL columns",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(activeFlightColumns).
					AddRow("session1", "ABC123", nil, started, nil, nil, nil, nil, nil, nil, nil)
				mock.ExpectQuery(regexp.QuoteMeta(activeFlightsQuery)).WillReturnRows(rows)
			},
			expectedCount: 1,
			check: func(t *testing.T, flights []*types.Flight) {
				f := flights[0]
				if f.Callsign != "" || !f.LastSeenAt.Equal(started) || f.MaxAltitude != 0 {
					t.Errorf("unexpected flight %+v (last_seen_at should default to started_at)", f)
				}
			},
		},
		{
			name: "no active flights",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(activeFlightsQuery)).WillReturnRows(sqlmock.NewRows(activeFlightColumns))
			},
			expectedCount: 0,
		},
		{
			name: "database query error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(activeFlightsQuery)).WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
		{
			name: "scan error",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(activeFlightColumns).
					AddRow("session1", "ABC123", "TEST123", started, seen, 40.7128, -74.0060, 41.0, -75.0, 35000, 450.5).
					RowError(0, sql.ErrNoRows)
				mock.ExpectQuery(regexp.QuoteMeta(activeFlightsQuery)).WillReturnRows(rows)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()
			tt.setupMock(mock)

			client := &Client{db: db}
			flights, err := client.GetActiveFlights()
			if (err != nil) != tt.expectError {
				t.Fatalf("GetActiveFlights() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				if len(flights) != tt.expectedCount {
					t.Fatalf("got %d flights, want %d", len(flights), tt.expectedCount)
				}
				if tt.check != nil {
					tt.check(t, flights)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestClient_CreateFlight_Unit(t *testing.T) {
	started := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	flight := &types.Flight{
		SessionID:      "test-session",
		HexIdent:       "ABC123",
		Callsign:       "TEST123",
		StartedAt:      started,
		LastSeenAt:     started,
		FirstLatitude:  40.7128,
		FirstLongitude: -74.0060,
		LastLatitude:   41.0000,
		LastLongitude:  -75.0000,
		MaxAltitude:    35000,
		MaxGroundSpeed: 450.5,
	}

	tests := []struct {
		name        string
		setupMock   func(sqlmock.Sqlmock)
		expectError bool
	}{
		{
			name: "successful flight creation (active: ended_at NULL)",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`INSERT INTO flights`).
					WithArgs("test-session", "ABC123", "TEST123", started, nil, started,
						40.7128, -74.0060, 41.0000, -75.0000, 35000, 450.5).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
		},
		{
			name: "database execution error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`INSERT INTO flights`).WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()
			tt.setupMock(mock)

			client := &Client{db: db}
			err = client.CreateFlight(flight)
			if (err != nil) != tt.expectError {
				t.Errorf("CreateFlight() error = %v, expectError %v", err, tt.expectError)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestClient_UpdateFlight_Unit(t *testing.T) {
	seen := time.Date(2026, 9, 24, 10, 30, 0, 0, time.UTC)
	flight := func(ended time.Time) *types.Flight {
		return &types.Flight{
			SessionID:      "test-session",
			HexIdent:       "ABC123",
			Callsign:       "UPDATED123",
			EndedAt:        ended,
			LastSeenAt:     seen,
			FirstLatitude:  40.0,
			FirstLongitude: -74.0,
			LastLatitude:   42.0000,
			LastLongitude:  -76.0000,
			MaxAltitude:    40000,
			MaxGroundSpeed: 500.0,
		}
	}

	tests := []struct {
		name        string
		flight      *types.Flight
		setupMock   func(sqlmock.Sqlmock)
		expectError error
		anyError    bool
	}{
		{
			name:   "ended flight",
			flight: flight(seen),
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`UPDATE flights SET`).
					WithArgs("UPDATED123", seen, seen, 40.0, -74.0, 42.0000, -76.0000, 40000, 500.0, "test-session").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			// A zero EndedAt must stay NULL, not become 0001-01-01.
			name:   "active flight keeps ended_at NULL",
			flight: flight(time.Time{}),
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`UPDATE flights SET`).
					WithArgs("UPDATED123", nil, seen, 40.0, -74.0, 42.0000, -76.0000, 40000, 500.0, "test-session").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			// Updating a flight that was never inserted used to succeed
			// silently; that hid the empty flights table.
			name:   "no row for the session",
			flight: flight(seen),
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`UPDATE flights SET`).WillReturnResult(sqlmock.NewResult(0, 0))
			},
			expectError: ErrFlightNotFound,
		},
		{
			name:   "database execution error",
			flight: flight(seen),
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`UPDATE flights SET`).WillReturnError(sql.ErrConnDone)
			},
			anyError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()
			tt.setupMock(mock)

			client := &Client{db: db}
			err = client.UpdateFlight(tt.flight)
			switch {
			case tt.expectError != nil:
				if !errors.Is(err, tt.expectError) {
					t.Errorf("UpdateFlight() error = %v, want %v", err, tt.expectError)
				}
			case tt.anyError:
				if err == nil {
					t.Error("UpdateFlight() expected an error")
				}
			case err != nil:
				t.Errorf("UpdateFlight() unexpected error: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestClient_StoreAircraftState_Unit(t *testing.T) {
	timestamp := time.Now()
	state := &types.AircraftState{
		HexIdent:     "ABC123",
		Callsign:     "TEST123",
		Altitude:     35000,
		GroundSpeed:  450.5,
		Track:        180.0,
		Latitude:     40.7128,
		Longitude:    -74.0060,
		VerticalRate: 1000,
		Squawk:       "1234",
		OnGround:     false,
		MsgType:      8,
		Timestamp:    timestamp,
	}

	tests := []struct {
		name        string
		setupMock   func(sqlmock.Sqlmock)
		expectError bool
	}{
		{
			name: "successful aircraft state storage",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`INSERT INTO aircraft_states`).
					WithArgs(timestamp, "ABC123", "TEST123", 35000, 450.5, 180.0, 40.7128, -74.0060, 1000, "1234", false, 8).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			expectError: false,
		},
		{
			name: "database execution error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`INSERT INTO aircraft_states`).
					WithArgs(timestamp, "ABC123", "TEST123", 35000, 450.5, 180.0, 40.7128, -74.0060, 1000, "1234", false, 8).
					WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			client := &Client{db: db}
			err = client.StoreAircraftState(state)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestClient_StoreSystemStats_Unit(t *testing.T) {
	lastMessageTime := time.Now()
	stats := map[string]interface{}{
		"total_messages":    uint64(100),
		"parsed_messages":   uint64(95),
		"failed_messages":   uint64(5),
		"stored_states":     uint64(90),
		"created_flights":   uint64(10),
		"updated_flights":   uint64(5),
		"ended_flights":     uint64(3),
		"active_aircraft":   uint64(15),
		"active_flights":    uint64(7),
		"message_types":     [10]uint64{0, 5, 10, 15, 20, 25, 30, 35, 40, 45},
		"last_message_time": lastMessageTime,
		"processing_time":   time.Duration(100) * time.Millisecond,
	}

	tests := []struct {
		name        string
		setupMock   func(sqlmock.Sqlmock)
		expectError bool
	}{
		{
			name: "successful system stats storage",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`INSERT INTO system_stats`).
					WithArgs(
						sqlmock.AnyArg(), // time.Now()
						uint64(100),      // total_messages
						uint64(95),       // parsed_messages
						uint64(5),        // failed_messages
						uint64(90),       // stored_states
						uint64(10),       // created_flights
						uint64(5),        // updated_flights
						uint64(3),        // ended_flights
						uint64(15),       // active_aircraft
						uint64(7),        // active_flights
						sqlmock.AnyArg(), // message_types array
						int64(100),       // processing_time_ms
						sqlmock.AnyArg(), // uptime_seconds
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			expectError: false,
		},
		{
			name: "database execution error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`INSERT INTO system_stats`).
					WithArgs(
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
					).
					WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			client := &Client{db: db}
			err = client.StoreSystemStats(stats)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestClient_GetSystemStats_Unit(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	end := time.Now()

	tests := []struct {
		name          string
		setupMock     func(sqlmock.Sqlmock)
		expectError   bool
		expectedCount int
	}{
		{
			name: "successful system stats retrieval",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"time", "total_messages", "parsed_messages", "failed_messages",
					"stored_states", "created_flights", "updated_flights", "ended_flights",
					"active_aircraft", "active_flights", "message_types",
					"processing_time_ms", "uptime_seconds",
				}).
					AddRow(time.Now(), int64(100), int64(95), int64(5), int64(90), int64(10), int64(5), int64(3), int64(15), int64(7), "{0,5,10,15,20,25,30,35,40,45}", int64(100), int64(3600))

				mock.ExpectQuery(`SELECT 
			time, total_messages, parsed_messages, failed_messages,
			stored_states, created_flights, updated_flights, ended_flights,
			active_aircraft, active_flights, message_types,
			processing_time_ms, uptime_seconds
		FROM system_stats
		WHERE time BETWEEN \$1 AND \$2
		ORDER BY time DESC`).
					WithArgs(start, end).
					WillReturnRows(rows)
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "no stats found",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"time", "total_messages", "parsed_messages", "failed_messages",
					"stored_states", "created_flights", "updated_flights", "ended_flights",
					"active_aircraft", "active_flights", "message_types",
					"processing_time_ms", "uptime_seconds",
				})

				mock.ExpectQuery(`SELECT 
			time, total_messages, parsed_messages, failed_messages,
			stored_states, created_flights, updated_flights, ended_flights,
			active_aircraft, active_flights, message_types,
			processing_time_ms, uptime_seconds
		FROM system_stats
		WHERE time BETWEEN \$1 AND \$2
		ORDER BY time DESC`).
					WithArgs(start, end).
					WillReturnRows(rows)
			},
			expectError:   false,
			expectedCount: 0,
		},
		{
			name: "database query error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT 
			time, total_messages, parsed_messages, failed_messages,
			stored_states, created_flights, updated_flights, ended_flights,
			active_aircraft, active_flights, message_types,
			processing_time_ms, uptime_seconds
		FROM system_stats
		WHERE time BETWEEN \$1 AND \$2
		ORDER BY time DESC`).
					WithArgs(start, end).
					WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
		{
			name: "scan error",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"time", "total_messages", "parsed_messages", "failed_messages",
					"stored_states", "created_flights", "updated_flights", "ended_flights",
					"active_aircraft", "active_flights", "message_types",
					"processing_time_ms", "uptime_seconds",
				}).
					AddRow(time.Now(), int64(100), int64(95), int64(5), int64(90), int64(10), int64(5), int64(3), int64(15), int64(7), "{0,5,10,15,20,25,30,35,40,45}", int64(100), int64(3600)).
					RowError(0, sql.ErrNoRows)

				mock.ExpectQuery(`SELECT 
			time, total_messages, parsed_messages, failed_messages,
			stored_states, created_flights, updated_flights, ended_flights,
			active_aircraft, active_flights, message_types,
			processing_time_ms, uptime_seconds
		FROM system_stats
		WHERE time BETWEEN \$1 AND \$2
		ORDER BY time DESC`).
					WithArgs(start, end).
					WillReturnRows(rows)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			client := &Client{db: db}
			stats, err := client.GetSystemStats(start, end)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
			if !tt.expectError && len(stats) != tt.expectedCount {
				t.Errorf("Expected %d stats, got %d", tt.expectedCount, len(stats))
			}

			// Validate structure of returned stats
			if !tt.expectError && len(stats) > 0 {
				firstStat := stats[0]
				requiredKeys := []string{
					"time", "total_messages", "parsed_messages", "failed_messages",
					"stored_states", "created_flights", "updated_flights", "ended_flights",
					"active_aircraft", "active_flights", "message_types",
					"processing_time", "uptime_seconds",
				}

				for _, key := range requiredKeys {
					if _, exists := firstStat[key]; !exists {
						t.Errorf("Expected key '%s' in system stats", key)
					}
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

// INTEGRATION TESTS (Existing tests that require PostgreSQL)

func TestNew_ValidConnectionString(t *testing.T) {
	// This test requires a PostgreSQL server running
	// For now, we'll test the function structure without actual connection
	connStr := "postgres://user:password@localhost:5432/sbs_logger?sslmode=disable"

	// Test that the function doesn't panic
	// Note: This will fail if PostgreSQL is not running, but that's expected
	client, err := New(connStr)
	if err != nil {
		// Expected if PostgreSQL is not running
		t.Logf("Expected error when PostgreSQL is not running: %v", err)
		return
	}

	if client == nil {
		t.Fatal("New() returned nil client")
	}

	if client.db == nil {
		t.Error("Expected database connection to be initialized")
	}

	// Clean up
	_ = client.Close()
}

func TestNew_InvalidConnectionString(t *testing.T) {
	connStr := "invalid://connection:string"

	client, err := New(connStr)
	if err != nil {
		// Expected error with invalid connection string
		t.Logf("Expected error with invalid connection string: %v", err)
		return
	}

	// If we get here, the connection string was accepted
	// This is actually valid behavior for sql.Open - it only validates the driver
	t.Log("Connection string was accepted (this is valid behavior)")
	_ = client.Close()
}

func TestNew_EmptyConnectionString(t *testing.T) {
	connStr := ""

	client, err := New(connStr)
	if err != nil {
		// Expected error with empty connection string
		t.Logf("Expected error with empty connection string: %v", err)
		return
	}

	// If we get here, the connection string was accepted
	// This is actually valid behavior for sql.Open - it only validates the driver
	t.Log("Empty connection string was accepted (this is valid behavior)")
	client.Close()
}

func TestClient_Close(t *testing.T) {
	// Test close without initialization
	client := &Client{}

	// This will panic because client.db is nil
	// We should test this with a proper client or handle the nil case
	defer func() {
		if r := recover(); r != nil {
			// Expected panic when db is nil
			t.Logf("Expected panic when db is nil: %v", r)
		}
	}()

	client.Close()
	t.Error("Close() should panic when db is nil")
}

func TestClient_CloseWithConnection(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Close should not panic
	err = client.Close()
	if err != nil {
		t.Errorf("Close() should not fail: %v", err)
	}
}

func TestClient_CreateFlight(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	flight := &types.Flight{
		SessionID:      "test-session",
		HexIdent:       "ABC123",
		Callsign:       "TEST123",
		StartedAt:      time.Now(),
		FirstLatitude:  40.7128,
		FirstLongitude: -74.0060,
		LastLatitude:   41.7128,
		LastLongitude:  -75.0060,
		MaxAltitude:    35000,
		MaxGroundSpeed: 450.5,
	}

	err = client.CreateFlight(flight)
	if err != nil {
		t.Fatalf("CreateFlight() failed: %v", err)
	}
}

func TestClient_UpdateFlight(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	flight := &types.Flight{
		SessionID:      "test-session",
		HexIdent:       "ABC123",
		Callsign:       "TEST123",
		StartedAt:      time.Now(),
		EndedAt:        time.Now().Add(time.Hour),
		FirstLatitude:  40.7128,
		FirstLongitude: -74.0060,
		LastLatitude:   41.7128,
		LastLongitude:  -75.0060,
		MaxAltitude:    35000,
		MaxGroundSpeed: 450.5,
	}

	// Create the flight first
	err = client.CreateFlight(flight)
	if err != nil {
		t.Fatalf("CreateFlight() failed: %v", err)
	}

	// Update the flight
	flight.Callsign = "UPDATED123"
	flight.MaxAltitude = 40000

	err = client.UpdateFlight(flight)
	if err != nil {
		t.Fatalf("UpdateFlight() failed: %v", err)
	}
}

func TestClient_GetActiveFlights(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Create a test flight
	flight := &types.Flight{
		SessionID:      "test-session",
		HexIdent:       "ABC123",
		Callsign:       "TEST123",
		StartedAt:      time.Now(),
		FirstLatitude:  40.7128,
		FirstLongitude: -74.0060,
		LastLatitude:   41.7128,
		LastLongitude:  -75.0060,
		MaxAltitude:    35000,
		MaxGroundSpeed: 450.5,
	}

	err = client.CreateFlight(flight)
	if err != nil {
		t.Fatalf("CreateFlight() failed: %v", err)
	}

	// Get active flights
	flights, err := client.GetActiveFlights()
	if err != nil {
		t.Fatalf("GetActiveFlights() failed: %v", err)
	}

	if len(flights) == 0 {
		t.Log("No active flights found (this is expected if the database is empty)")
		return
	}

	// Check that we can find our test flight
	found := false
	for _, f := range flights {
		if f.SessionID == flight.SessionID {
			found = true
			if f.HexIdent != flight.HexIdent {
				t.Errorf("Expected HexIdent %s, got %s", flight.HexIdent, f.HexIdent)
			}
			if f.Callsign != flight.Callsign {
				t.Errorf("Expected Callsign %s, got %s", flight.Callsign, f.Callsign)
			}
			break
		}
	}

	if !found {
		t.Log("Test flight not found in active flights (may have been ended)")
	}
}

func TestClient_StoreAircraftState(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	state := &types.AircraftState{
		HexIdent:     "ABC123",
		Callsign:     "TEST123",
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
	}

	err = client.StoreAircraftState(state)
	if err != nil {
		t.Fatalf("StoreAircraftState() failed: %v", err)
	}
}

func TestClient_StoreSystemStats(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	stats := map[string]interface{}{
		"total_messages":    uint64(100),
		"parsed_messages":   uint64(95),
		"failed_messages":   uint64(5),
		"stored_states":     uint64(90),
		"created_flights":   uint64(10),
		"updated_flights":   uint64(5),
		"ended_flights":     uint64(3),
		"active_aircraft":   uint64(15),
		"active_flights":    uint64(7),
		"message_types":     [10]uint64{0, 5, 10, 15, 20, 25, 30, 35, 40, 45},
		"last_message_time": time.Now(),
		"processing_time":   time.Duration(100) * time.Millisecond,
	}

	err = client.StoreSystemStats(stats)
	if err != nil {
		t.Fatalf("StoreSystemStats() failed: %v", err)
	}
}

func TestClient_GetSystemStats(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Store some test stats first
	stats := map[string]interface{}{
		"total_messages":    uint64(100),
		"parsed_messages":   uint64(95),
		"failed_messages":   uint64(5),
		"stored_states":     uint64(90),
		"created_flights":   uint64(10),
		"updated_flights":   uint64(5),
		"ended_flights":     uint64(3),
		"active_aircraft":   uint64(15),
		"active_flights":    uint64(7),
		"message_types":     [10]uint64{0, 5, 10, 15, 20, 25, 30, 35, 40, 45},
		"last_message_time": time.Now(),
		"processing_time":   time.Duration(100) * time.Millisecond,
	}

	err = client.StoreSystemStats(stats)
	if err != nil {
		t.Fatalf("StoreSystemStats() failed: %v", err)
	}

	// Get stats for the last hour
	end := time.Now()
	start := end.Add(-time.Hour)

	retrievedStats, err := client.GetSystemStats(start, end)
	if err != nil {
		t.Fatalf("GetSystemStats() failed: %v", err)
	}

	if len(retrievedStats) == 0 {
		t.Log("No system stats found in the specified time range")
		return
	}

	// Check the structure of the first stat
	firstStat := retrievedStats[0]

	requiredKeys := []string{
		"time", "total_messages", "parsed_messages", "failed_messages",
		"stored_states", "created_flights", "updated_flights", "ended_flights",
		"active_aircraft", "active_flights", "message_types",
		"processing_time", "uptime_seconds",
	}

	for _, key := range requiredKeys {
		if _, exists := firstStat[key]; !exists {
			t.Errorf("Expected key '%s' in system stats", key)
		}
	}
}

func TestClient_StoreSystemStats_EdgeCases(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Test with zero values
	stats := map[string]interface{}{
		"total_messages":    uint64(0),
		"parsed_messages":   uint64(0),
		"failed_messages":   uint64(0),
		"stored_states":     uint64(0),
		"created_flights":   uint64(0),
		"updated_flights":   uint64(0),
		"ended_flights":     uint64(0),
		"active_aircraft":   uint64(0),
		"active_flights":    uint64(0),
		"message_types":     [10]uint64{},
		"last_message_time": time.Now(),
		"processing_time":   time.Duration(0),
	}

	err = client.StoreSystemStats(stats)
	if err != nil {
		t.Fatalf("StoreSystemStats() with zero values failed: %v", err)
	}

	// Test with large values
	stats = map[string]interface{}{
		"total_messages":    uint64(999999999),
		"parsed_messages":   uint64(999999999),
		"failed_messages":   uint64(999999999),
		"stored_states":     uint64(999999999),
		"created_flights":   uint64(999999999),
		"updated_flights":   uint64(999999999),
		"ended_flights":     uint64(999999999),
		"active_aircraft":   uint64(999999999),
		"active_flights":    uint64(999999999),
		"message_types":     [10]uint64{999999999, 999999999, 999999999, 999999999, 999999999, 999999999, 999999999, 999999999, 999999999, 999999999},
		"last_message_time": time.Now(),
		"processing_time":   time.Duration(999999999) * time.Millisecond,
	}

	err = client.StoreSystemStats(stats)
	if err != nil {
		t.Fatalf("StoreSystemStats() with large values failed: %v", err)
	}
}

func TestClient_StoreAircraftState_EdgeCases(t *testing.T) {
	// This test requires PostgreSQL to be running
	client, err := New("postgres://user:password@localhost:5432/sbs_logger?sslmode=disable")
	if err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}
	defer client.Close()

	// Test that we can actually connect
	if err := client.db.Ping(); err != nil {
		t.Skip("PostgreSQL not available, skipping test")
	}

	// Test with zero values
	state := &types.AircraftState{
		HexIdent:     "",
		Callsign:     "",
		Altitude:     0,
		GroundSpeed:  0.0,
		Track:        0.0,
		Latitude:     0.0,
		Longitude:    0.0,
		VerticalRate: 0,
		Squawk:       "",
		OnGround:     false,
		MsgType:      0,
		Timestamp:    time.Now(),
		SessionID:    "",
	}

	err = client.StoreAircraftState(state)
	if err != nil {
		t.Fatalf("StoreAircraftState() with zero values failed: %v", err)
	}

	// Test with extreme values
	state = &types.AircraftState{
		HexIdent:     "FFFFFFFF",
		Callsign:     "VERY_LONG_CALLSIGN_THAT_MIGHT_EXCEED_LIMITS",
		Altitude:     999999,
		GroundSpeed:  999999.999,
		Track:        359.999,
		Latitude:     90.0,
		Longitude:    180.0,
		VerticalRate: 999999,
		Squawk:       "7777",
		OnGround:     true,
		MsgType:      99,
		Timestamp:    time.Now(),
		SessionID:    "very_long_session_id_that_might_exceed_limits",
	}

	err = client.StoreAircraftState(state)
	if err != nil {
		t.Fatalf("StoreAircraftState() with extreme values failed: %v", err)
	}
}

func TestClient_StoreAircraftStates_Unit(t *testing.T) {
	ts := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	states := make([]*types.AircraftState, stateInsertChunk+2)
	for i := range states {
		states[i] = &types.AircraftState{HexIdent: "E49329", MsgType: 4, GroundSpeed: 233.5, Timestamp: ts.Add(time.Duration(i) * time.Millisecond)}
	}

	t.Run("one transaction, chunked inserts", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO aircraft_states`).WillReturnResult(sqlmock.NewResult(0, stateInsertChunk))
		mock.ExpectExec(`INSERT INTO aircraft_states`).
			WithArgs(states[stateInsertChunk].Timestamp, "E49329", "", 0, 233.5, 0.0, 0.0, 0.0, 0, "", false, 4,
				states[stateInsertChunk+1].Timestamp, "E49329", "", 0, 233.5, 0.0, 0.0, 0.0, 0, "", false, 4).
			WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectCommit()

		if err := (&Client{db: db}).StoreAircraftStates(states); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})

	t.Run("failure rolls back", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO aircraft_states`).WillReturnError(sql.ErrConnDone)
		mock.ExpectRollback()
		if err := (&Client{db: db}).StoreAircraftStates(states[:3]); err == nil {
			t.Fatal("expected an error")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})

	t.Run("empty batch does nothing", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := (&Client{db: db}).StoreAircraftStates(nil); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
}

func TestBuildStatesInsert(t *testing.T) {
	q, args := buildStatesInsert([]*types.AircraftState{{HexIdent: "A"}, {HexIdent: "B"}})
	if !strings.Contains(q, "($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12), ($13,") || !strings.HasSuffix(q, "$24)") {
		t.Errorf("query placeholders: %s", q)
	}
	if len(args) != 24 || args[1] != "A" || args[13] != "B" {
		t.Errorf("args = %v", args)
	}
}
