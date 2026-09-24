package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/db/migrations"
	"github.com/saviobatista/sbs-logger/internal/testutils"
	"github.com/saviobatista/sbs-logger/internal/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Same TimescaleDB major/minor as production (timescaledb 2.27 on pg16), so
// the migrations run against the real extension, hypertables and continuous
// aggregates instead of a mock.
const timescaleImage = "timescale/timescaledb:2.27.1-pg16"

func startTimescale(t *testing.T) (*sql.DB, string) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, timescaleImage,
		postgres.WithDatabase("sbs_logger"),
		postgres.WithUsername("sbs"),
		postgres.WithPassword("sbs"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start TimescaleDB container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate TimescaleDB container: %v", err)
		}
	})
	if err := testutils.WaitForPostgresReady(ctx, container); err != nil {
		t.Fatalf("TimescaleDB not ready: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	connStr += "&sslmode=disable"

	sqlDB, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB, connStr
}

// SBS ground speed and track are decimals ("157.9", "295.7"). They used to go
// into INTEGER columns and every such insert failed with
// `pq: invalid input syntax for type integer: "157.9"`, dropping the state.
func TestDecimalSpeedAndTrackAreStored_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	sqlDB, connStr := startTimescale(t)
	if err := migrations.New(sqlDB).Migrate(migrations.All()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	client, err := New(connStr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ts := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	state := &types.AircraftState{
		HexIdent: "E49329", GroundSpeed: 157.9, Track: 295.7,
		VerticalRate: 64, MsgType: 4, Timestamp: ts,
	}
	if err := client.StoreAircraftState(state); err != nil {
		t.Fatalf("StoreAircraftState with decimal speed/track: %v", err)
	}
	var gs, track float64
	if err := sqlDB.QueryRow(
		`SELECT ground_speed, track FROM aircraft_states WHERE hex_ident = $1`, "E49329",
	).Scan(&gs, &track); err != nil {
		t.Fatal(err)
	}
	if gs != 157.9 || track != 295.7 {
		t.Errorf("stored ground_speed=%v track=%v, want 157.9 and 295.7", gs, track)
	}

	// flights.max_ground_speed takes the same value and had the same type.
	flight := &types.Flight{
		SessionID: "s-1", HexIdent: "E49329", StartedAt: ts, MaxGroundSpeed: 157.9,
	}
	if err := client.CreateFlight(flight); err != nil {
		t.Fatalf("CreateFlight with decimal max ground speed: %v", err)
	}
	flight.MaxGroundSpeed = 233.4
	flight.EndedAt = ts.Add(time.Hour)
	if err := client.UpdateFlight(flight); err != nil {
		t.Fatalf("UpdateFlight with decimal max ground speed: %v", err)
	}
	var maxGS float64
	if err := sqlDB.QueryRow(
		`SELECT max_ground_speed FROM flights WHERE session_id = $1`, "s-1",
	).Scan(&maxGS); err != nil {
		t.Fatal(err)
	}
	if maxGS != 233.4 {
		t.Errorf("stored max_ground_speed=%v, want 233.4", maxGS)
	}
}

// Production already has 001 and 002 applied and rows in the tables: the new
// migration must convert the columns in place, keep the existing values and
// leave the continuous aggregate on aircraft_states working.
func TestDecimalSpeedMigrationUpgradesExistingData_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	sqlDB, connStr := startTimescale(t)
	all := migrations.All()
	m := migrations.New(sqlDB)
	if err := m.Migrate(all[:2]); err != nil {
		t.Fatalf("migrate 001-002: %v", err)
	}
	client, err := New(connStr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ts := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) // a different chunk from the insert after the upgrade
	if err := client.StoreAircraftState(&types.AircraftState{
		HexIdent: "OLD001", GroundSpeed: 233, Track: 272, Altitude: 5850, MsgType: 4, Timestamp: ts,
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	// Raw SQL: the client now writes columns added by later migrations.
	if _, err := sqlDB.Exec(
		`INSERT INTO flights (session_id, hex_ident, started_at, max_ground_speed) VALUES ('old', 'OLD001', $1, 351)`, ts,
	); err != nil {
		t.Fatalf("seed flight: %v", err)
	}

	if err := m.Migrate(all); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	var gs, track float64
	var alt int
	if err := sqlDB.QueryRow(
		`SELECT ground_speed, track, altitude FROM aircraft_states WHERE hex_ident = 'OLD001'`,
	).Scan(&gs, &track, &alt); err != nil {
		t.Fatal(err)
	}
	if gs != 233 || track != 272 || alt != 5850 {
		t.Errorf("existing row changed: ground_speed=%v track=%v altitude=%v", gs, track, alt)
	}
	var maxGS float64
	if err := sqlDB.QueryRow(`SELECT max_ground_speed FROM flights WHERE session_id = 'old'`).Scan(&maxGS); err != nil {
		t.Fatal(err)
	}
	if maxGS != 351 {
		t.Errorf("existing flight changed: max_ground_speed=%v", maxGS)
	}

	if err := client.StoreAircraftState(&types.AircraftState{
		HexIdent: "NEW001", GroundSpeed: 157.9, Track: 295.7, MsgType: 4, Timestamp: ts.AddDate(0, 0, 13),
	}); err != nil {
		t.Fatalf("decimal insert after upgrade: %v", err)
	}
	if _, err := sqlDB.Exec(`CALL refresh_continuous_aggregate('aircraft_states_hourly', NULL, NULL)`); err != nil {
		t.Fatalf("refresh aircraft_states_hourly: %v", err)
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COALESCE(SUM(state_count), 0) FROM aircraft_states_hourly`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("aircraft_states_hourly counts %d states, want 2", n)
	}
}

// Flight rows through the real schema: an active flight has ended_at NULL,
// GetActiveFlights must read it back (scanning NULL into time.Time failed),
// UpdateFlight must keep it NULL while active and report a missing row.
func TestFlightRows_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	sqlDB, connStr := startTimescale(t)
	if err := migrations.New(sqlDB).Migrate(migrations.All()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	client, err := New(connStr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ts := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	flight := &types.Flight{
		SessionID: "s-1", HexIdent: "E49329", StartedAt: ts, LastSeenAt: ts,
	}
	if err := client.CreateFlight(flight); err != nil {
		t.Fatalf("CreateFlight: %v", err)
	}
	flight.Callsign = "TAM3456"
	flight.LastSeenAt = ts.Add(time.Minute)
	flight.MaxAltitude = 5850
	if err := client.UpdateFlight(flight); err != nil {
		t.Fatalf("UpdateFlight (active): %v", err)
	}

	active, err := client.GetActiveFlights()
	if err != nil {
		t.Fatalf("GetActiveFlights: %v", err)
	}
	if len(active) != 1 || active[0].Callsign != "TAM3456" || !active[0].LastSeenAt.Equal(ts.Add(time.Minute)) {
		t.Fatalf("active flights = %+v", active)
	}

	if err := client.UpdateFlight(&types.Flight{SessionID: "missing"}); !errors.Is(err, ErrFlightNotFound) {
		t.Errorf("UpdateFlight on a missing row: %v, want ErrFlightNotFound", err)
	}

	// One active flight per aircraft.
	if err := client.CreateFlight(&types.Flight{SessionID: "s-2", HexIdent: "E49329", StartedAt: ts}); err == nil {
		t.Error("second active flight for the same aircraft was accepted")
	}

	flight.EndedAt = flight.LastSeenAt
	if err := client.UpdateFlight(flight); err != nil {
		t.Fatalf("UpdateFlight (end): %v", err)
	}
	if active, err = client.GetActiveFlights(); err != nil || len(active) != 0 {
		t.Fatalf("after end: %d active flights, err %v", len(active), err)
	}
	if err := client.CreateFlight(&types.Flight{SessionID: "s-2", HexIdent: "E49329", StartedAt: ts.Add(time.Hour)}); err != nil {
		t.Errorf("new flight after the previous one ended: %v", err)
	}
}

// Migration 004 on a database at 003 with rows: last_seen_at is backfilled
// and duplicate active flights are ended so the unique index can be built.
func TestFlightLastSeenMigration_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	sqlDB, _ := startTimescale(t)
	all := migrations.All()
	m := migrations.New(sqlDB)
	if err := m.Migrate(all[:3]); err != nil {
		t.Fatalf("migrate 001-003: %v", err)
	}
	ts := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	if _, err := sqlDB.Exec(`
		INSERT INTO flights (session_id, hex_ident, started_at, ended_at) VALUES
			('ended', 'AAA111', $1, $2),
			('dup-old', 'BBB222', $1, NULL),
			('dup-new', 'BBB222', $2, NULL)`, ts, ts.Add(time.Hour)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := m.Migrate(all); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	check := func(id string, wantSeen time.Time, wantEnded bool) {
		t.Helper()
		var seen time.Time
		var ended sql.NullTime
		if err := sqlDB.QueryRow(`SELECT last_seen_at, ended_at FROM flights WHERE session_id = $1`, id).Scan(&seen, &ended); err != nil {
			t.Fatal(err)
		}
		if !seen.Equal(wantSeen) || ended.Valid != wantEnded {
			t.Errorf("%s: last_seen_at=%v ended=%v, want %v %v", id, seen, ended.Valid, wantSeen, wantEnded)
		}
	}
	check("ended", ts.Add(time.Hour), true)
	check("dup-old", ts, true)
	check("dup-new", ts.Add(time.Hour), false)
}

// A batch of states lands in one transaction, decimals included.
func TestStoreAircraftStates_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	sqlDB, connStr := startTimescale(t)
	if err := migrations.New(sqlDB).Migrate(migrations.All()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	client, err := New(connStr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ts := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	states := make([]*types.AircraftState, 2500) // spans three INSERT chunks
	for i := range states {
		states[i] = &types.AircraftState{
			HexIdent: "E49329", GroundSpeed: 233.5, Track: 272.5, MsgType: 4,
			Timestamp: ts.Add(time.Duration(i) * time.Millisecond),
		}
	}
	if err := client.StoreAircraftStates(states); err != nil {
		t.Fatalf("StoreAircraftStates: %v", err)
	}
	var n int
	var gs float64
	if err := sqlDB.QueryRow(`SELECT count(*), max(ground_speed) FROM aircraft_states`).Scan(&n, &gs); err != nil {
		t.Fatal(err)
	}
	if n != len(states) || gs != 233.5 {
		t.Errorf("stored %d states (max ground speed %v), want %d", n, gs, len(states))
	}
}
