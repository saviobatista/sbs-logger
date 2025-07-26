package main

import (
	"context"
	"database/sql"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

type testContainers struct {
	postgres *postgres.PostgresContainer
	redis    *redis.RedisContainer
}

func setupTestContainers(t *testing.T) *testContainers {
	ctx := context.Background()

	// Start PostgreSQL container
	postgresContainer, err := postgres.Run(ctx, "postgres:14-alpine",
		postgres.WithDatabase("sbs_logger"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections"),
		),
	)
	if err != nil {
		t.Fatalf("Failed to start PostgreSQL container: %v", err)
	}

	// Start Redis container
	redisContainer, err := redis.Run(ctx, "redis:7-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections"),
		),
	)
	if err != nil {
		t.Fatalf("Failed to start Redis container: %v", err)
	}

	return &testContainers{
		postgres: postgresContainer,
		redis:    redisContainer,
	}
}

func TestContainerSetup_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		if err := containers.postgres.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
		if err := containers.redis.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate Redis container: %v", err)
		}
	}()

	// Test that containers are accessible
	dbConnStr, err := containers.postgres.ConnectionString(context.Background())
	if err != nil {
		t.Fatalf("Failed to get PostgreSQL connection string: %v", err)
	}

	// Test database connection
	dbConnStrWithSSL := dbConnStr + "&sslmode=disable"

	db, err := sql.Open("postgres", dbConnStrWithSSL)
	if err != nil {
		t.Fatalf("Failed to open database connection: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Errorf("Database ping failed: %v", err)
	}
}

func TestBasicDatabaseOperations_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := setupTestContainers(t)
	defer func() {
		if err := containers.postgres.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
		if err := containers.redis.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate Redis container: %v", err)
		}
	}()

	// Get database connection string
	dbConnStr, err := containers.postgres.ConnectionString(context.Background())
	if err != nil {
		t.Fatalf("Failed to get PostgreSQL connection string: %v", err)
	}

	// Test basic database operations with SSL disabled
	dbConnStrWithSSL := dbConnStr + "&sslmode=disable"
	db, err := sql.Open("postgres", dbConnStrWithSSL)
	if err != nil {
		t.Fatalf("Failed to open database connection: %v", err)
	}
	defer db.Close()

	// Test basic table creation (without TimescaleDB extensions)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS test_table (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Test inserting data
	_, err = db.Exec(`INSERT INTO test_table (name) VALUES ($1)`, "test_data")
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Test querying data
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM test_table`).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query test data: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 row, got %d", count)
	}

	// Clean up
	_, err = db.Exec(`DROP TABLE test_table`)
	if err != nil {
		t.Fatalf("Failed to drop test table: %v", err)
	}
}
