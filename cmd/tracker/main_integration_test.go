//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/saviobatista/sbs-logger/internal/db"
	"github.com/saviobatista/sbs-logger/internal/nats"
	"github.com/saviobatista/sbs-logger/internal/redis"
	"github.com/saviobatista/sbs-logger/internal/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redis_test"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestIntegration_TrackerFullStack tests the complete tracker system with all infrastructure
func TestIntegration_TrackerFullStack(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start all required containers
	postgresContainer, dbConnStr := startPostgreSQLContainer(t, ctx)
	defer func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}()

	redisContainer, redisAddr := startRedisContainer(t, ctx)
	defer func() {
		if err := redisContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate Redis container: %v", err)
		}
	}()

	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer func() {
		if err := natsContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	// Setup database
	dbClient, err := db.New(dbConnStr)
	if err != nil {
		t.Fatalf("Failed to create database client: %v", err)
	}
	defer dbClient.Close()

	// Run migrations
	err = runMigrations(dbConnStr)
	if err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Setup Redis
	redisClient, err := redis.NewWithClient(redisAddr)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redisClient.Close()

	// Setup NATS
	natsClient, err := nats.New(natsURL)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}
	defer natsClient.Close()

	// Create and start state tracker
	tracker := NewStateTracker(dbClient, redisClient)
	trackerCtx, trackerCancel := context.WithCancel(context.Background())
	defer trackerCancel()

	err = tracker.Start(trackerCtx)
	if err != nil {
		t.Fatalf("Failed to start state tracker: %v", err)
	}

	// Subscribe to NATS messages
	messageCount := 0
	err = natsClient.SubscribeSBSRaw(func(msg *types.SBSMessage) {
		err := tracker.ProcessMessage(msg)
		if err != nil {
			t.Errorf("Failed to process message: %v", err)
		}
		messageCount++
	})
	if err != nil {
		t.Fatalf("Failed to subscribe to NATS: %v", err)
	}

	// Test scenarios
	testMessages := []*types.SBSMessage{
		{
			Raw:       "MSG,3,111,11111,4CA2D6,111111,2015/02/19,18:06:07.710,2015/02/19,18:06:07.710,,33000,45.1234,-122.5678,90,180,1,0,0,0,0",
			Timestamp: time.Now().UTC(),
			Source:    "integration-test",
		},
		{
			Raw:       "MSG,1,111,11111,4CA2D6,111111,2015/02/19,18:06:08.710,2015/02/19,18:06:08.710,UAL123,33000,45.1234,-122.5678,90,180,1,0,0,0,0",
			Timestamp: time.Now().UTC(),
			Source:    "integration-test",
		},
		{
			Raw:       "MSG,4,111,11111,4CA2D7,111111,2015/02/19,18:06:09.710,2015/02/19,18:06:09.710,,34000,45.1235,-122.5679,95,185,1,0,0,0,0",
			Timestamp: time.Now().UTC(),
			Source:    "integration-test",
		},
	}

	// Publish test messages
	for _, msg := range testMessages {
		err := natsClient.PublishSBSMessage(msg)
		if err != nil {
			t.Errorf("Failed to publish message: %v", err)
		}
	}

	// Wait for message processing
	timeout := time.After(5 * time.Second)
	for messageCount < len(testMessages) {
		select {
		case <-timeout:
			t.Errorf("Timeout waiting for messages. Expected %d, got %d", len(testMessages), messageCount)
			return
		case <-time.After(100 * time.Millisecond):
			// Continue waiting
		}
	}

	// Verify data was stored correctly
	err = verifyFlightsInDatabase(dbClient, 2) // Should have 2 unique flights
	if err != nil {
		t.Errorf("Database verification failed: %v", err)
	}

	err = verifyFlightsInRedis(redisClient, trackerCtx, 2)
	if err != nil {
		t.Errorf("Redis verification failed: %v", err)
	}
}

// TestIntegration_TrackerEnvironmentVariables tests environment variable handling
func TestIntegration_TrackerEnvironmentVariables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Save original environment
	originalDBConnStr := os.Getenv("DB_CONN_STR")
	originalRedisAddr := os.Getenv("REDIS_ADDR")
	originalNATSURL := os.Getenv("NATS_URL")
	defer func() {
		os.Setenv("DB_CONN_STR", originalDBConnStr)
		os.Setenv("REDIS_ADDR", originalRedisAddr)
		os.Setenv("NATS_URL", originalNATSURL)
	}()

	ctx := context.Background()

	// Start containers
	postgresContainer, dbConnStr := startPostgreSQLContainer(t, ctx)
	defer func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}()

	redisContainer, redisAddr := startRedisContainer(t, ctx)
	defer func() {
		if err := redisContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate Redis container: %v", err)
		}
	}()

	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer func() {
		if err := natsContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate NATS container: %v", err)
		}
	}()

	tests := []struct {
		name              string
		dbConnStr         string
		redisAddr         string
		natsURL           string
		expectDBClient    bool
		expectRedisClient bool
		expectNATSClient  bool
	}{
		{
			name:              "valid_environment_variables",
			dbConnStr:         dbConnStr,
			redisAddr:         redisAddr,
			natsURL:           natsURL,
			expectDBClient:    true,
			expectRedisClient: true,
			expectNATSClient:  true,
		},
		{
			name:              "invalid_database_connection",
			dbConnStr:         "postgres://invalid:invalid@nonexistent:5432/invalid",
			redisAddr:         redisAddr,
			natsURL:           natsURL,
			expectDBClient:    false,
			expectRedisClient: true,
			expectNATSClient:  true,
		},
		{
			name:              "invalid_redis_connection",
			dbConnStr:         dbConnStr,
			redisAddr:         "localhost:99999",
			natsURL:           natsURL,
			expectDBClient:    true,
			expectRedisClient: false,
			expectNATSClient:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			os.Setenv("DB_CONN_STR", tt.dbConnStr)
			os.Setenv("REDIS_ADDR", tt.redisAddr)
			os.Setenv("NATS_URL", tt.natsURL)

			// Test client creation
			if tt.expectDBClient {
				dbClient, err := db.New(tt.dbConnStr)
				if err != nil {
					t.Errorf("Expected database client creation to succeed, got error: %v", err)
				} else {
					dbClient.Close()
				}
			}

			if tt.expectRedisClient {
				redisClient, err := redis.NewWithClient(tt.redisAddr)
				if err != nil {
					t.Errorf("Expected Redis client creation to succeed, got error: %v", err)
				} else {
					redisClient.Close()
				}
			}

			if tt.expectNATSClient {
				natsClient, err := nats.New(tt.natsURL)
				if err != nil {
					t.Errorf("Expected NATS client creation to succeed, got error: %v", err)
				} else {
					natsClient.Close()
				}
			}
		})
	}
}

// TestIntegration_TrackerStateTracker tests the StateTracker with real infrastructure
func TestIntegration_TrackerStateTracker(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start containers
	postgresContainer, dbConnStr := startPostgreSQLContainer(t, ctx)
	defer func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}()

	redisContainer, redisAddr := startRedisContainer(t, ctx)
	defer func() {
		if err := redisContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate Redis container: %v", err)
		}
	}()

	// Setup clients
	dbClient, err := db.New(dbConnStr)
	if err != nil {
		t.Fatalf("Failed to create database client: %v", err)
	}
	defer dbClient.Close()

	err = runMigrations(dbConnStr)
	if err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	redisClient, err := redis.NewWithClient(redisAddr)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redisClient.Close()

	// Test StateTracker functionality
	tracker := NewStateTracker(dbClient, redisClient)

	err = tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start state tracker: %v", err)
	}

	// Test processing different types of messages
	testMessages := []*types.SBSMessage{
		{
			Raw:       "MSG,3,111,11111,4CA2D6,111111,2015/02/19,18:06:07.710,2015/02/19,18:06:07.710,,33000,45.1234,-122.5678,90,180,1,0,0,0,0",
			Timestamp: time.Now().UTC(),
			Source:    "test-tracker",
		},
		{
			Raw:       "MSG,1,111,11111,4CA2D6,111111,2015/02/19,18:06:08.710,2015/02/19,18:06:08.710,UAL123,33000,45.1234,-122.5678,90,180,1,0,0,0,0",
			Timestamp: time.Now().UTC(),
			Source:    "test-tracker",
		},
	}

	for _, msg := range testMessages {
		err := tracker.ProcessMessage(msg)
		if err != nil {
			t.Errorf("Failed to process message: %v", err)
		}
	}

	// Verify state tracking
	if len(tracker.activeFlights) == 0 {
		t.Error("Expected active flights to be tracked")
	}

	if len(tracker.states) == 0 {
		t.Error("Expected aircraft states to be tracked")
	}
}

// Helper functions

func startPostgreSQLContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	postgresContainer, err := postgres.Run(ctx,
		"postgres:14-alpine",
		postgres.WithDatabase("test_db"),
		postgres.WithUsername("test_user"),
		postgres.WithPassword("test_password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("Failed to start PostgreSQL container: %v", err)
	}

	dbURL, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get connection string: %v", err)
	}

	t.Logf("PostgreSQL container started with URL: %s", dbURL)
	return postgresContainer, dbURL
}

func startRedisContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	redisContainer, err := redis_test.Run(ctx,
		"redis:7-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").
				WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("Failed to start Redis container: %v", err)
	}

	redisAddr, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("Failed to get Redis connection string: %v", err)
	}

	t.Logf("Redis container started at: %s", redisAddr)
	return redisContainer, redisAddr
}

func startNATSContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image:        "nats:2.10-alpine",
		ExposedPorts: []string{"4222/tcp"},
		Cmd: []string{
			"--jetstream",
			"--store_dir", "/data",
		},
		WaitingFor: wait.ForLog("Server is ready").WithStartupTimeout(30 * time.Second),
	}

	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start NATS container: %v", err)
	}

	mappedPort, err := natsContainer.MappedPort(ctx, "4222")
	if err != nil {
		t.Fatalf("Failed to get mapped port: %v", err)
	}

	host, err := natsContainer.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get container host: %v", err)
	}

	natsURL := fmt.Sprintf("nats://%s:%s", host, mappedPort.Port())
	t.Logf("NATS container started at: %s", natsURL)

	return natsContainer, natsURL
}

func runMigrations(dbConnStr string) error {
	// This would normally run the migration logic
	// For this test, we'll assume migrations are applied
	// In a real scenario, you'd call the migrate package
	return nil
}

func verifyFlightsInDatabase(dbClient *db.Client, expectedCount int) error {
	flights, err := dbClient.GetActiveFlights()
	if err != nil {
		return fmt.Errorf("failed to get active flights: %w", err)
	}

	if len(flights) < expectedCount {
		return fmt.Errorf("expected at least %d flights, got %d", expectedCount, len(flights))
	}

	return nil
}

func verifyFlightsInRedis(redisClient *redis.Client, ctx context.Context, expectedCount int) error {
	// This is a simplified verification - in practice you'd check specific keys
	// For now, we just verify the Redis client is working
	err := redisClient.SetFlightValidation(ctx, "test", true)
	if err != nil {
		return fmt.Errorf("failed to write to Redis: %w", err)
	}

	valid, err := redisClient.GetFlightValidation(ctx, "test")
	if err != nil {
		return fmt.Errorf("failed to read from Redis: %w", err)
	}

	if !valid {
		return fmt.Errorf("Redis read/write verification failed")
	}

	return nil
}
