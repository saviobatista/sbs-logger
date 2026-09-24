package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/saviobatista/sbs-logger/internal/db"
	"github.com/saviobatista/sbs-logger/internal/db/migrations"
	"github.com/saviobatista/sbs-logger/internal/metrics"
	"github.com/saviobatista/sbs-logger/internal/nats"
	"github.com/saviobatista/sbs-logger/internal/parser"
	"github.com/saviobatista/sbs-logger/internal/redis"
	"github.com/saviobatista/sbs-logger/internal/stats"
	"github.com/saviobatista/sbs-logger/internal/types"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// DBClient interface for testability
type DBClient interface {
	GetActiveFlights() ([]*types.Flight, error)
	CreateFlight(flight *types.Flight) error
	UpdateFlight(flight *types.Flight) error
	StoreAircraftStates(states []*types.AircraftState) error
	Close() error
}

// RedisClient interface for testability
type RedisClient interface {
	StoreFlight(ctx context.Context, flight *types.Flight) error
	GetFlight(ctx context.Context, hexIdent string) (*types.Flight, error)
	DeleteFlight(ctx context.Context, hexIdent string) error
	StoreAircraftState(ctx context.Context, state *types.AircraftState) error
	GetAircraftState(ctx context.Context, hexIdent string) (*types.AircraftState, error)
	DeleteAircraftState(ctx context.Context, hexIdent string) error
	Close() error
}

// Flight session rules.
//
// A flight is one continuous period in which an aircraft (hex ident) is heard.
// It starts with the first message of an aircraft that has no active flight
// and gets a new session_id. While messages keep coming it tracks the
// callsign, the first and last known position, and the maximum altitude and
// ground speed seen. It ends when the aircraft has been silent for longer
// than flightTimeout, with ended_at = the last time it was heard; a message
// after such a gap ends the old flight and starts a new one.
//
// Time is the message time (the ingest timestamp), not the wall clock, so a
// tracker running behind the stream still measures the gaps correctly.
// metricsPort is the default port of the /metrics endpoint.
const metricsPort = "9103"

const (
	flightTimeout       = 5 * time.Minute
	flightFlushInterval = time.Minute      // how often an active flight is written to the database
	sweepInterval       = 30 * time.Second // how often silent flights are looked for
)

// StateTracker tracks aircraft states and flight sessions
type StateTracker struct {
	mu            sync.Mutex // ProcessMessage runs on the NATS goroutine, the sweep on its own
	db            DBClient
	redis         RedisClient
	activeFlights map[string]*types.Flight
	flushedAt     map[string]time.Time            // last database write of each active flight
	states        map[string]*types.AircraftState // Cache of latest states
	clock         time.Time                       // latest message time processed
	stats         *stats.Stats
}

// NewStateTracker creates a new state tracker
func NewStateTracker(db DBClient, redis RedisClient) *StateTracker {
	return &StateTracker{
		db:            db,
		redis:         redis,
		activeFlights: make(map[string]*types.Flight),
		flushedAt:     make(map[string]time.Time),
		states:        make(map[string]*types.AircraftState),
		stats:         stats.New(),
	}
}

// Start initializes the state tracker
func (t *StateTracker) Start(ctx context.Context) error {
	if err := t.loadActiveFlights(ctx); err != nil {
		return err
	}

	// Set database client for statistics (only if it's the concrete type)
	if dbClient, ok := t.db.(*db.Client); ok {
		t.stats.SetDB(dbClient)
	}

	// Start statistics logging and persistence
	go t.logStats(ctx)
	go t.stats.StartPersistence(ctx, 5*time.Minute)
	go t.sweepLoop(ctx)

	return nil
}

// loadActiveFlights resumes the flights that were active when the tracker
// stopped, so an aircraft still in the air continues its session instead of
// getting a second one. Flights that ended while the tracker was down are
// closed by the first sweep, once the message clock shows the gap.
func (t *StateTracker) loadActiveFlights(ctx context.Context) error {
	flights, err := t.db.GetActiveFlights()
	if err != nil {
		return fmt.Errorf("failed to load active flights: %w", err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	for _, flight := range flights {
		if flight.LastSeenAt.IsZero() {
			flight.LastSeenAt = flight.StartedAt
		}
		if prev, ok := t.activeFlights[flight.HexIdent]; ok {
			// Only one active flight per aircraft: keep the latest one.
			older := flight
			if flight.LastSeenAt.After(prev.LastSeenAt) {
				older = prev
				t.activeFlights[flight.HexIdent] = flight
			}
			older.EndedAt = older.LastSeenAt
			if err := t.db.UpdateFlight(older); err != nil {
				return fmt.Errorf("failed to end duplicate flight %s: %w", older.SessionID, err)
			}
			t.stats.IncrementEndedFlights()
		} else {
			t.activeFlights[flight.HexIdent] = flight
		}
	}
	for hex, flight := range t.activeFlights {
		t.flushedAt[hex] = flight.LastSeenAt
		if err := t.redis.StoreFlight(ctx, flight); err != nil {
			log.Printf("Warning: Failed to cache flight in Redis: %v", err)
		}
	}
	t.stats.SetActiveFlights(uint64(len(t.activeFlights)))
	log.Printf("Resumed %d active flights", len(t.activeFlights))
	return nil
}

// ProcessBatch processes a batch of SBS messages and stores their states in
// one transaction. It returns an error only when the states could not be
// stored, so the batch is redelivered; a message that does not parse or a
// flight write that fails is logged and counted and does not block the rest.
func (t *StateTracker) ProcessBatch(msgs []*types.SBSMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	start := time.Now()
	states := make([]*types.AircraftState, 0, len(msgs))
	for _, msg := range msgs {
		state, err := t.applyMessage(msg)
		if err != nil {
			log.Printf("Failed to process message: %v", err)
		}
		if state != nil {
			states = append(states, state)
		}
	}
	if err := t.storeStates(states); err != nil {
		return err
	}
	t.stats.AddProcessingTime(time.Since(start))
	return nil
}

// ProcessMessage processes a single SBS message and reports any error.
func (t *StateTracker) ProcessMessage(msg *types.SBSMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	start := time.Now()
	state, err := t.applyMessage(msg)
	if state != nil {
		if serr := t.storeStates([]*types.AircraftState{state}); serr != nil {
			return serr
		}
	}
	t.stats.AddProcessingTime(time.Since(start))
	return err
}

func (t *StateTracker) storeStates(states []*types.AircraftState) error {
	if len(states) == 0 {
		return nil
	}
	if err := t.db.StoreAircraftStates(states); err != nil {
		return fmt.Errorf("failed to store aircraft states: %w", err)
	}
	for range states {
		t.stats.IncrementStoredStates()
	}
	return nil
}

// applyMessage parses a message, updates the aircraft's cached state and its
// flight, and returns the parsed state to store. The state is returned even
// when the flight update fails. The caller holds t.mu.
func (t *StateTracker) applyMessage(msg *types.SBSMessage) (*types.AircraftState, error) {
	t.stats.IncrementTotalMessages()
	t.stats.UpdateLastMessageTime()

	// Parse message into aircraft state
	state, err := parser.ParseMessage(msg.Raw, msg.Timestamp)
	if err != nil {
		t.stats.IncrementFailedMessages()
		return nil, fmt.Errorf("failed to parse message: %w (raw: %q)", err, msg.Raw)
	}

	// Skip if no state information
	if state == nil || state.HexIdent == "" {
		return nil, nil
	}

	t.stats.IncrementParsedMessages()
	t.stats.IncrementMessageType(state.MsgType)

	// Update state cache
	latestState, exists := t.states[state.HexIdent]
	if !exists {
		cached := *state
		t.states[state.HexIdent] = &cached
	} else {
		// Merge new state with existing state
		t.mergeStates(latestState, state)
	}

	// Store the merged aircraft state in Redis: it is the "current state"
	// cache, and one SBS message carries only a slice of it (a MSG,1 has the
	// callsign, a MSG,3 the position). Storing the partial message left the
	// cache with whatever the last message happened to contain.
	if err := t.redis.StoreAircraftState(context.Background(), t.states[state.HexIdent]); err != nil {
		log.Printf("Warning: Failed to store aircraft state in Redis: %v", err)
	}

	// Update flight session
	err = t.updateFlight(state)
	if err != nil {
		err = fmt.Errorf("failed to update flight: %w", err)
	}

	// Update statistics
	t.stats.SetActiveAircraft(uint64(len(t.states)))
	t.stats.SetActiveFlights(uint64(len(t.activeFlights)))
	return state, err
}

// mergeStates merges newState into existing state
func (t *StateTracker) mergeStates(existing, newState *types.AircraftState) {
	if newState.Callsign != "" {
		existing.Callsign = newState.Callsign
	}
	if newState.Altitude != 0 {
		existing.Altitude = newState.Altitude
	}
	if newState.GroundSpeed != 0 {
		existing.GroundSpeed = newState.GroundSpeed
	}
	if newState.Track != 0 {
		existing.Track = newState.Track
	}
	if newState.Latitude != 0 {
		existing.Latitude = newState.Latitude
	}
	if newState.Longitude != 0 {
		existing.Longitude = newState.Longitude
	}
	if newState.VerticalRate != 0 {
		existing.VerticalRate = newState.VerticalRate
	}
	if newState.Squawk != "" {
		existing.Squawk = newState.Squawk
	}
	existing.OnGround = newState.OnGround
	existing.Timestamp = newState.Timestamp
}

func hasPosition(state *types.AircraftState) bool {
	return state.Latitude != 0 || state.Longitude != 0
}

// updateFlight applies one aircraft state to the aircraft's flight session,
// creating the session when there is none.
//
// The tracker's own map is the source of truth for active flights. It used
// to ask Redis first, and Redis returned an empty Flight (not nil) for a
// missing key, so a flight was never created: every message "updated" or
// "ended" a flight with no session_id, and the flights table stayed empty.
func (t *StateTracker) updateFlight(state *types.AircraftState) error {
	ts := state.Timestamp
	if ts.After(t.clock) {
		t.clock = ts
	}

	flight := t.activeFlights[state.HexIdent]
	if flight != nil && ts.Sub(flight.LastSeenAt) > flightTimeout {
		// The aircraft was silent for longer than the timeout (and the sweep
		// has not closed its flight yet): that flight ended when it was last
		// heard, and this message starts a new one.
		if err := t.endFlight(flight, false); err != nil {
			return err
		}
		flight = nil
	}

	if flight == nil {
		return t.createFlight(state)
	}

	if state.Callsign != "" {
		flight.Callsign = state.Callsign
	}
	if hasPosition(state) {
		if flight.FirstLatitude == 0 && flight.FirstLongitude == 0 {
			flight.FirstLatitude, flight.FirstLongitude = state.Latitude, state.Longitude
		}
		flight.LastLatitude, flight.LastLongitude = state.Latitude, state.Longitude
	}
	if state.Altitude > flight.MaxAltitude {
		flight.MaxAltitude = state.Altitude
	}
	if state.GroundSpeed > flight.MaxGroundSpeed {
		flight.MaxGroundSpeed = state.GroundSpeed
	}
	if ts.After(flight.LastSeenAt) {
		flight.LastSeenAt = ts
	}

	if err := t.redis.StoreFlight(context.Background(), flight); err != nil {
		log.Printf("Warning: Failed to update flight in Redis: %v", err)
	}

	// Write the running values periodically, not on every message: the
	// tracker already does one INSERT per message, and the row only has to be
	// current enough to resume the flight after a restart.
	if ts.Sub(t.flushedAt[state.HexIdent]) >= flightFlushInterval {
		if err := t.db.UpdateFlight(flight); err != nil {
			if errors.Is(err, db.ErrFlightNotFound) {
				// The row is gone: forget the flight so the next message
				// starts a new one instead of failing forever.
				t.forgetFlight(flight.HexIdent)
			}
			return fmt.Errorf("failed to update flight: %w", err)
		}
		t.flushedAt[state.HexIdent] = ts
	}
	t.stats.IncrementUpdatedFlights()
	return nil
}

// createFlight starts a new flight session from the aircraft's first state.
// The row is inserted before the flight becomes active, so a failed insert
// is retried on the next message instead of leaving a flight that exists
// only in memory.
func (t *StateTracker) createFlight(state *types.AircraftState) error {
	flight := &types.Flight{
		SessionID:      uuid.New().String(),
		HexIdent:       state.HexIdent,
		Callsign:       state.Callsign,
		StartedAt:      state.Timestamp,
		LastSeenAt:     state.Timestamp,
		MaxAltitude:    state.Altitude,
		MaxGroundSpeed: state.GroundSpeed,
	}
	if hasPosition(state) {
		flight.FirstLatitude, flight.FirstLongitude = state.Latitude, state.Longitude
		flight.LastLatitude, flight.LastLongitude = state.Latitude, state.Longitude
	}

	if err := t.db.CreateFlight(flight); err != nil {
		return fmt.Errorf("failed to create flight: %w", err)
	}
	t.activeFlights[state.HexIdent] = flight
	t.flushedAt[state.HexIdent] = state.Timestamp
	t.stats.IncrementCreatedFlights()

	if err := t.redis.StoreFlight(context.Background(), flight); err != nil {
		log.Printf("Warning: Failed to store flight in Redis: %v", err)
	}
	return nil
}

// endFlight closes a flight at the time the aircraft was last heard. When
// the aircraft is gone (sweep), its cached state is dropped as well.
func (t *StateTracker) endFlight(flight *types.Flight, aircraftGone bool) error {
	flight.EndedAt = flight.LastSeenAt
	if err := t.db.UpdateFlight(flight); err != nil {
		if errors.Is(err, db.ErrFlightNotFound) {
			t.forgetFlight(flight.HexIdent)
		} else {
			flight.EndedAt = time.Time{}
		}
		return fmt.Errorf("failed to end flight %s: %w", flight.SessionID, err)
	}
	t.forgetFlight(flight.HexIdent)
	t.stats.IncrementEndedFlights()

	if err := t.redis.DeleteFlight(context.Background(), flight.HexIdent); err != nil {
		log.Printf("Warning: Failed to delete flight from Redis: %v", err)
	}
	if aircraftGone {
		delete(t.states, flight.HexIdent)
		if err := t.redis.DeleteAircraftState(context.Background(), flight.HexIdent); err != nil {
			log.Printf("Warning: Failed to delete aircraft state from Redis: %v", err)
		}
	}
	return nil
}

func (t *StateTracker) forgetFlight(hexIdent string) {
	delete(t.activeFlights, hexIdent)
	delete(t.flushedAt, hexIdent)
}

// sweepStaleFlights ends the flights of aircraft silent for longer than
// flightTimeout, measured on the message clock. Without messages the clock
// does not move and nothing ends; the flights still end at their last-seen
// time once messages resume.
func (t *StateTracker) sweepStaleFlights() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.clock.IsZero() {
		return nil
	}
	var errs []error
	for _, flight := range t.activeFlights {
		if t.clock.Sub(flight.LastSeenAt) > flightTimeout {
			if err := t.endFlight(flight, true); err != nil {
				errs = append(errs, err)
			}
		}
	}
	// Drop states of aircraft without a flight (e.g. a failed insert).
	for hex, state := range t.states {
		if _, ok := t.activeFlights[hex]; !ok && t.clock.Sub(state.Timestamp) > flightTimeout {
			delete(t.states, hex)
		}
	}
	t.stats.SetActiveAircraft(uint64(len(t.states)))
	t.stats.SetActiveFlights(uint64(len(t.activeFlights)))
	return errors.Join(errs...)
}

func (t *StateTracker) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := t.sweepStaleFlights(); err != nil {
				log.Printf("Failed to end stale flights: %v", err)
			}
		}
	}
}

// FlushActiveFlights writes every active flight to the database, so a
// restart resumes them with their latest values.
func (t *StateTracker) FlushActiveFlights() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var errs []error
	for hex, flight := range t.activeFlights {
		if err := t.db.UpdateFlight(flight); err != nil {
			errs = append(errs, err)
			continue
		}
		t.flushedAt[hex] = flight.LastSeenAt
	}
	return errors.Join(errs...)
}

// MessageClock returns the time of the latest message processed.
func (t *StateTracker) MessageClock() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.clock
}

// registerMetrics exposes the tracker statistics for Prometheus.
func (t *StateTracker) registerMetrics(reg *metrics.Registry) {
	stat := func(key string) func() float64 {
		return func() float64 {
			v, _ := t.stats.GetStats()[key].(uint64)
			return float64(v)
		}
	}
	reg.CounterFunc("sbs_tracker_messages_total", "SBS messages received by the tracker.", stat("total_messages"))
	reg.CounterFunc("sbs_tracker_messages_parsed_total", "SBS messages parsed into an aircraft state.", stat("parsed_messages"))
	reg.CounterFunc("sbs_tracker_messages_failed_total", "SBS messages that failed to parse.", stat("failed_messages"))
	reg.CounterFunc("sbs_tracker_states_stored_total", "Aircraft states written to TimescaleDB.", stat("stored_states"))
	reg.CounterFunc("sbs_tracker_flights_created_total", "Flight sessions started.", stat("created_flights"))
	reg.CounterFunc("sbs_tracker_flights_ended_total", "Flight sessions ended.", stat("ended_flights"))
	reg.GaugeFunc("sbs_tracker_flights_active", "Flight sessions currently active.", stat("active_flights"))
	reg.GaugeFunc("sbs_tracker_aircraft_active", "Aircraft with a cached state.", stat("active_aircraft"))
	reg.GaugeFunc("sbs_tracker_lag_seconds", "Wall clock minus the ingest time of the latest message processed.", func() float64 {
		clock := t.MessageClock()
		if clock.IsZero() {
			return 0
		}
		return time.Since(clock).Seconds()
	})
}

// logStats periodically logs statistics
func (t *StateTracker) logStats(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Printf("Statistics:\n%s", t.stats)
		}
	}
}

// parseEnvironment extracts environment variable parsing logic for testability
func parseEnvironment() (string, string, string) {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats:4222" // Default to Docker service name
	}

	dbConnStr := os.Getenv("DB_CONN_STR")
	if dbConnStr == "" {
		dbConnStr = "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379" // Default to Docker service name
	}

	return natsURL, dbConnStr, redisAddr
}

// createClients creates all the required clients for the application
func createClients(natsURL, dbConnStr, redisAddr string) (*nats.Client, *db.Client, *redis.Client, error) {
	// Create NATS client
	natsClient, err := nats.New(natsURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create NATS client: %w", err)
	}

	// Create database client
	dbClient, err := db.New(dbConnStr)
	if err != nil {
		natsClient.Close()
		return nil, nil, nil, fmt.Errorf("failed to create database client: %w", err)
	}

	// Run database migrations
	if err := runMigrations(dbConnStr); err != nil {
		natsClient.Close()
		if closeErr := dbClient.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "error closing dbClient: %v\n", closeErr)
		}
		return nil, nil, nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Create Redis client
	redisClient, err := redis.New(redisAddr)
	if err != nil {
		natsClient.Close()
		if closeErr := dbClient.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "error closing dbClient: %v\n", closeErr)
		}
		return nil, nil, nil, fmt.Errorf("failed to create Redis client: %w", err)
	}

	return natsClient, dbClient, redisClient, nil
}

// runMigrations runs database migrations on startup
func runMigrations(dbConnStr string) error {
	log.Println("Running database migrations...")

	// Connect to database for migrations
	migrationDB, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		return fmt.Errorf("failed to connect to database for migrations: %w", err)
	}
	defer func() {
		if err := migrationDB.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing migration db: %v\n", err)
		}
	}()

	// Test connection
	if err := migrationDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Create migrator
	migrator := migrations.New(migrationDB)

	// Execute migrations
	if err := migrator.Migrate(migrations.All()); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	log.Println("Database migrations completed successfully")
	return nil
}

// setupStateTracker creates and starts the state tracker
func setupStateTracker(dbClient *db.Client, redisClient *redis.Client) (*StateTracker, error) {
	// Create state tracker
	tracker := NewStateTracker(dbClient, redisClient)
	if err := tracker.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start state tracker: %w", err)
	}
	return tracker, nil
}

// startConsumer consumes sbs.raw through the durable "sbs-tracker" consumer
// in batches, until ctx is done. The returned channel is closed when it stops.
func startConsumer(ctx context.Context, natsClient *nats.Client, tracker *StateTracker) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		opts := nats.ConsumeOptions{Durable: nats.ConsumerTracker, Batch: 500}
		if err := natsClient.Consume(ctx, opts, tracker.ProcessBatch); err != nil {
			// Without its consumer the tracker does nothing: exit and let
			// the orchestrator restart it.
			log.Printf("Consumer failed: %v", err)
			os.Exit(1)
		}
	}()
	return done
}

// waitForShutdown waits for shutdown signals and handles cleanup: it stops
// the consumer (letting the batch in progress finish and be acknowledged),
// then writes the active flights and closes the clients.
func waitForShutdown(cancel context.CancelFunc, consumerDone <-chan struct{}, natsClient *nats.Client, dbClient *db.Client, redisClient *redis.Client, tracker *StateTracker) {
	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	cancel()
	select {
	case <-consumerDone:
	case <-time.After(10 * time.Second):
		log.Println("Consumer did not stop in time")
	}
	natsClient.Close()
	if err := tracker.FlushActiveFlights(); err != nil {
		log.Printf("Failed to flush active flights: %v", err)
	}
	if err := dbClient.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "error closing dbClient: %v\n", err)
	}
	if err := redisClient.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "error closing redisClient: %v\n", err)
	}
}

func main() {
	// Load configuration
	natsURL, dbConnStr, redisAddr := parseEnvironment()

	// Create clients
	natsClient, dbClient, redisClient, err := createClients(natsURL, dbConnStr, redisAddr)
	if err != nil {
		log.Printf("Failed to create clients: %v", err)
		os.Exit(1)
	}

	// Setup state tracker
	tracker, err := setupStateTracker(dbClient, redisClient)
	if err != nil {
		log.Printf("Failed to setup state tracker: %v", err)
		natsClient.Close()
		if err := dbClient.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing dbClient: %v\n", err)
		}
		if err := redisClient.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing redisClient: %v\n", err)
		}
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prometheus metrics (default :9103, METRICS_ADDR overrides, empty disables)
	reg := metrics.NewRegistry()
	tracker.registerMetrics(reg)
	reg.Serve(ctx, metrics.Addr(metricsPort))

	// Consume SBS messages
	consumerDone := startConsumer(ctx, natsClient, tracker)

	// Wait for shutdown
	waitForShutdown(cancel, consumerDone, natsClient, dbClient, redisClient, tracker)
}
