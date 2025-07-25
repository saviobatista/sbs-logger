package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/saviobatista/sbs-logger/internal/db"
	"github.com/saviobatista/sbs-logger/internal/nats"
	"github.com/saviobatista/sbs-logger/internal/parser"
	"github.com/saviobatista/sbs-logger/internal/redis"
	"github.com/saviobatista/sbs-logger/internal/stats"
	"github.com/saviobatista/sbs-logger/internal/types"
)

// DBClient interface for testability
type DBClient interface {
	GetActiveFlights() ([]*types.Flight, error)
	CreateFlight(flight *types.Flight) error
	UpdateFlight(flight *types.Flight) error
	StoreAircraftState(state *types.AircraftState) error
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
	SetFlightValidation(ctx context.Context, hexIdent string, valid bool) error
	GetFlightValidation(ctx context.Context, hexIdent string) (bool, error)
	Close() error
}

// StateTracker tracks aircraft states and flight sessions
type StateTracker struct {
	db            DBClient
	redis         RedisClient
	activeFlights map[string]*types.Flight
	states        map[string]*types.AircraftState // Cache of latest states
	stats         *stats.Stats
}

// NewStateTracker creates a new state tracker
func NewStateTracker(db DBClient, redis RedisClient) *StateTracker {
	return &StateTracker{
		db:            db,
		redis:         redis,
		activeFlights: make(map[string]*types.Flight),
		states:        make(map[string]*types.AircraftState),
		stats:         stats.New(),
	}
}

// Start initializes the state tracker
func (t *StateTracker) Start(ctx context.Context) error {
	// Load active flights from database
	flights, err := t.db.GetActiveFlights()
	if err != nil {
		return fmt.Errorf("failed to load active flights: %w", err)
	}

	// Initialize active flights map and Redis cache
	for _, flight := range flights {
		t.activeFlights[flight.HexIdent] = flight
		if err := t.redis.StoreFlight(ctx, flight); err != nil {
			log.Printf("Warning: Failed to cache flight in Redis: %v", err)
		}
	}

	// Set database client for statistics (only if it's the concrete type)
	if dbClient, ok := t.db.(*db.Client); ok {
		t.stats.SetDB(dbClient)
	}

	// Start statistics logging and persistence
	go t.logStats(ctx)
	go t.stats.StartPersistence(ctx, 5*time.Minute)

	return nil
}

// ProcessMessage processes an SBS message and updates aircraft state
func (t *StateTracker) ProcessMessage(msg *types.SBSMessage) error {
	start := time.Now()
	t.stats.IncrementTotalMessages()
	t.stats.UpdateLastMessageTime()

	// Parse message into aircraft state
	state, err := parser.ParseMessage(msg.Raw, msg.Timestamp)
	if err != nil {
		t.stats.IncrementFailedMessages()
		return fmt.Errorf("failed to parse message: %w", err)
	}

	// Skip if no state information
	if state == nil {
		return nil
	}

	t.stats.IncrementParsedMessages()
	t.stats.IncrementMessageType(state.MsgType)

	// Check flight validation in Redis
	valid, err := t.redis.GetFlightValidation(context.Background(), state.HexIdent)
	if err != nil {
		log.Printf("Warning: Failed to get flight validation: %v", err)
	} else if !valid {
		return nil // Skip invalid flights
	}

	// Update state cache
	latestState, exists := t.states[state.HexIdent]
	if !exists {
		t.states[state.HexIdent] = state
	} else {
		// Merge new state with existing state
		t.mergeStates(latestState, state)
	}

	// Store aircraft state in Redis
	if err := t.redis.StoreAircraftState(context.Background(), state); err != nil {
		log.Printf("Warning: Failed to store aircraft state in Redis: %v", err)
	}

	// Store aircraft state in database
	if err := t.db.StoreAircraftState(state); err != nil {
		return fmt.Errorf("failed to store aircraft state: %w", err)
	}
	t.stats.IncrementStoredStates()

	// Update flight session
	if err := t.updateFlight(state); err != nil {
		return fmt.Errorf("failed to update flight: %w", err)
	}

	// Update statistics
	t.stats.SetActiveAircraft(uint64(len(t.states)))
	t.stats.SetActiveFlights(uint64(len(t.activeFlights)))
	t.stats.AddProcessingTime(time.Since(start))

	return nil
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

// updateFlight updates or creates a flight session
func (t *StateTracker) updateFlight(state *types.AircraftState) error {
	// Try to get flight from Redis first
	flight, err := t.redis.GetFlight(context.Background(), state.HexIdent)
	if err != nil {
		log.Printf("Warning: Failed to get flight from Redis: %v", err)
	}

	// If not in Redis, check local cache
	if flight == nil {
		flight = t.activeFlights[state.HexIdent]
	}

	if flight == nil {
		// Create new flight
		flight = &types.Flight{
			SessionID:      uuid.New().String(),
			HexIdent:       state.HexIdent,
			Callsign:       state.Callsign,
			StartedAt:      state.Timestamp,
			FirstLatitude:  state.Latitude,
			FirstLongitude: state.Longitude,
		}
		t.activeFlights[state.HexIdent] = flight

		// Store in Redis
		if err := t.redis.StoreFlight(context.Background(), flight); err != nil {
			log.Printf("Warning: Failed to store flight in Redis: %v", err)
		}

		// Store in database
		if err := t.db.CreateFlight(flight); err != nil {
			return fmt.Errorf("failed to create flight: %w", err)
		}
		t.stats.IncrementCreatedFlights()
	} else {
		// Update existing flight
		flight.LastLatitude = state.Latitude
		flight.LastLongitude = state.Longitude
		if state.Altitude > flight.MaxAltitude {
			flight.MaxAltitude = state.Altitude
		}
		if state.GroundSpeed > flight.MaxGroundSpeed {
			flight.MaxGroundSpeed = state.GroundSpeed
		}

		// Check if flight has ended (no updates for 5 minutes)
		if time.Since(state.Timestamp) > 5*time.Minute {
			flight.EndedAt = state.Timestamp
			delete(t.activeFlights, state.HexIdent)
			delete(t.states, state.HexIdent)

			// Remove from Redis
			if err := t.redis.DeleteFlight(context.Background(), state.HexIdent); err != nil {
				log.Printf("Warning: Failed to delete flight from Redis: %v", err)
			}
			if err := t.redis.DeleteAircraftState(context.Background(), state.HexIdent); err != nil {
				log.Printf("Warning: Failed to delete aircraft state from Redis: %v", err)
			}

			// Update in database
			if err := t.db.UpdateFlight(flight); err != nil {
				return fmt.Errorf("failed to update flight: %w", err)
			}
			t.stats.IncrementEndedFlights()
		} else {
			// Update in Redis
			if err := t.redis.StoreFlight(context.Background(), flight); err != nil {
				log.Printf("Warning: Failed to update flight in Redis: %v", err)
			}
			t.stats.IncrementUpdatedFlights()
		}
	}

	return nil
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

// setupStateTracker creates and starts the state tracker
func setupStateTracker(dbClient *db.Client, redisClient *redis.Client) (*StateTracker, error) {
	// Create state tracker
	tracker := NewStateTracker(dbClient, redisClient)
	if err := tracker.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start state tracker: %w", err)
	}
	return tracker, nil
}

// setupNATSSubscription sets up the NATS subscription for SBS messages
func setupNATSSubscription(natsClient *nats.Client, tracker *StateTracker) error {
	if err := natsClient.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		if err := tracker.ProcessMessage(msg); err != nil {
			log.Printf("Failed to process message: %v", err)
		}
	}); err != nil {
		return fmt.Errorf("failed to subscribe to SBS messages: %w", err)
	}
	return nil
}

// waitForShutdown waits for shutdown signals and handles cleanup
func waitForShutdown(natsClient *nats.Client, dbClient *db.Client, redisClient *redis.Client) {
	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	// Fix: call natsClient.Close() without error check
	natsClient.Close()
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

	// Subscribe to SBS messages
	if err := setupNATSSubscription(natsClient, tracker); err != nil {
		log.Printf("Failed to setup NATS subscription: %v", err)
		natsClient.Close()
		if err := dbClient.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing dbClient: %v\n", err)
		}
		if err := redisClient.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing redisClient: %v\n", err)
		}
		os.Exit(1)
	}

	// Wait for shutdown
	waitForShutdown(natsClient, dbClient, redisClient)
}
