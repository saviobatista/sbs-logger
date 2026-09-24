package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/db"
	redisclient "github.com/saviobatista/sbs-logger/internal/redis"
	"github.com/saviobatista/sbs-logger/internal/testutils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Flight lifecycle against the real stack: TimescaleDB with every migration
// applied, a real Redis and the real clients. Before the fix this ended with
// an empty flights table, because the real Redis client returned an empty
// Flight for a missing key and no flight was ever inserted.
func TestFlightLifecycle_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := postgres.Run(ctx, "timescale/timescaledb:2.27.1-pg16",
		postgres.WithDatabase("sbs_logger"),
		postgres.WithUsername("sbs"),
		postgres.WithPassword("sbs"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start TimescaleDB: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })
	if err := testutils.WaitForPostgresReady(ctx, pg); err != nil {
		t.Fatalf("TimescaleDB not ready: %v", err)
	}
	connStr, err := pg.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	connStr += "&sslmode=disable"

	rc, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start Redis: %v", err)
	}
	t.Cleanup(func() { _ = rc.Terminate(context.Background()) })
	redisURI, err := rc.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	redisAddr := redisURI[len("redis://"):]
	t.Setenv("REDIS_PASSWORD", "")

	if err := runMigrations(connStr); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	sqlDB, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	newTracker := func() (*StateTracker, context.CancelFunc) {
		dbClient, err := db.New(connStr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = dbClient.Close() })
		redisClient, err := redisclient.New(redisAddr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = redisClient.Close() })
		tracker := NewStateTracker(dbClient, redisClient)
		tctx, cancel := context.WithCancel(ctx)
		if err := tracker.Start(tctx); err != nil {
			cancel()
			t.Fatalf("Start: %v", err)
		}
		return tracker, cancel
	}

	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	tracker, cancel := newTracker()
	process(t, tracker, msgIdent, t0)
	process(t, tracker, msgPosition, t0.Add(time.Second))
	process(t, tracker, msgVelocity, t0.Add(flightFlushInterval+time.Second))
	process(t, tracker, msgOther, t0.Add(2*time.Minute))

	var active int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM flights WHERE ended_at IS NULL`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 2 {
		t.Fatalf("%d active flights in the table, want 2", active)
	}
	var (
		callsign string
		maxAlt   int
		maxGS    float64
		seen     time.Time
	)
	if err := sqlDB.QueryRow(
		`SELECT callsign, max_altitude, max_ground_speed, last_seen_at FROM flights WHERE hex_ident = 'E49329'`,
	).Scan(&callsign, &maxAlt, &maxGS, &seen); err != nil {
		t.Fatal(err)
	}
	if callsign != "TAM3456" || maxAlt != 5850 || maxGS != 233.5 || !seen.Equal(t0.Add(flightFlushInterval+time.Second)) {
		t.Errorf("flushed row: callsign=%q max_altitude=%d max_ground_speed=%v last_seen_at=%v", callsign, maxAlt, maxGS, seen)
	}

	// Restart: the new tracker loads the active flights (ended_at NULL used
	// to break the scan) and continues them instead of inserting new ones.
	if err := tracker.FlushActiveFlights(); err != nil {
		t.Fatal(err)
	}
	cancel()
	tracker, cancel = newTracker()
	defer cancel()
	if len(tracker.activeFlights) != 2 {
		t.Fatalf("resumed %d flights, want 2", len(tracker.activeFlights))
	}
	process(t, tracker, msgVelocity, t0.Add(3*time.Minute))

	// E4ABCD goes silent; E49329 keeps talking past the timeout.
	process(t, tracker, msgVelocity, t0.Add(8*time.Minute))
	if err := tracker.sweepStaleFlights(); err != nil {
		t.Fatal(err)
	}

	rows, err := sqlDB.Query(`SELECT hex_ident, ended_at FROM flights ORDER BY hex_ident`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]sql.NullTime{}
	n := 0
	for rows.Next() {
		var hex string
		var ended sql.NullTime
		if err := rows.Scan(&hex, &ended); err != nil {
			t.Fatal(err)
		}
		got[hex] = ended
		n++
	}
	if n != 2 {
		t.Fatalf("%d flight rows, want 2 (no duplicates after restart)", n)
	}
	if got["E49329"].Valid {
		t.Errorf("E49329 ended at %v, want still active", got["E49329"].Time)
	}
	if !got["E4ABCD"].Valid || !got["E4ABCD"].Time.Equal(t0.Add(2*time.Minute)) {
		t.Errorf("E4ABCD ended_at = %+v, want its last seen time %v", got["E4ABCD"], t0.Add(2*time.Minute))
	}

	// The partial unique index refuses a second active flight per aircraft.
	if _, err := sqlDB.Exec(
		`INSERT INTO flights (session_id, hex_ident, started_at) VALUES ('dup', 'E49329', now())`,
	); err == nil {
		t.Error("a second active flight for E49329 was accepted")
	}
}
