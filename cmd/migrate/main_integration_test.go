//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestIntegration_MigrateWithRealPostgreSQL tests the migration command against real PostgreSQL
func TestIntegration_MigrateWithRealPostgreSQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL container
	postgresContainer, dbURL := startPostgreSQLContainer(t, ctx)
	defer func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}()

	tests := []struct {
		name     string
		rollback bool
		wantErr  bool
	}{
		{
			name:     "successful_forward_migration",
			rollback: false,
			wantErr:  false,
		},
		{
			name:     "successful_rollback_migration",
			rollback: true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For rollback test, we need to run a forward migration first
			if tt.rollback {
				err := run(dbURL, false)
				if err != nil {
					t.Fatalf("Failed to run forward migration before rollback: %v", err)
				}
			}

			// Run the migration
			err := run(dbURL, tt.rollback)
			if (err != nil) != tt.wantErr {
				t.Errorf("run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify the migration results
			if !tt.wantErr {
				err = verifyMigrationState(dbURL, tt.rollback)
				if err != nil {
					t.Errorf("Migration verification failed: %v", err)
				}
			}
		})
	}
}

// TestIntegration_MigrateErrorCases tests error scenarios with real PostgreSQL
func TestIntegration_MigrateErrorCases(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	tests := []struct {
		name    string
		dbURL   string
		wantErr bool
	}{
		{
			name:    "invalid_connection_string",
			dbURL:   "postgres://invalid:invalid@nonexistent:5432/invalid",
			wantErr: true,
		},
		{
			name:    "malformed_connection_string",
			dbURL:   "not-a-valid-url",
			wantErr: true,
		},
		{
			name:    "empty_connection_string",
			dbURL:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.dbURL, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestIntegration_MigrateIdempotency tests that migrations can be run multiple times
func TestIntegration_MigrateIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL container
	postgresContainer, dbURL := startPostgreSQLContainer(t, ctx)
	defer func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}()

	// Run migration multiple times
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("run_%d", i+1), func(t *testing.T) {
			err := run(dbURL, false)
			if err != nil {
				t.Errorf("Migration run %d failed: %v", i+1, err)
			}

			// Verify state after each run
			err = verifyMigrationState(dbURL, false)
			if err != nil {
				t.Errorf("Migration verification failed on run %d: %v", i+1, err)
			}
		})
	}
}

// TestIntegration_MigrateRollbackIdempotency tests rollback idempotency
func TestIntegration_MigrateRollbackIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL container
	postgresContainer, dbURL := startPostgreSQLContainer(t, ctx)
	defer func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}()

	// First, run forward migration
	err := run(dbURL, false)
	if err != nil {
		t.Fatalf("Failed to run forward migration: %v", err)
	}

	// Run rollback multiple times
	for i := 0; i < 2; i++ {
		t.Run(fmt.Sprintf("rollback_%d", i+1), func(t *testing.T) {
			err := run(dbURL, true)
			if err != nil {
				t.Errorf("Rollback run %d failed: %v", i+1, err)
			}
		})
	}
}

// TestIntegration_ParseFlagsWithRealUsage tests flag parsing in realistic scenarios
func TestIntegration_ParseFlagsWithRealUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL container
	postgresContainer, dbURL := startPostgreSQLContainer(t, ctx)
	defer func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}()

	// Test parsing and actual usage with real database
	tests := []struct {
		name     string
		testFunc func(t *testing.T, dbURL string)
	}{
		{
			name: "default_database_URL_usage",
			testFunc: func(t *testing.T, realDBURL string) {
				// We can't easily test parseFlags() directly due to flag.Parse()
				// but we can test the run function with the real database URL
				err := run(realDBURL, false)
				if err != nil {
					t.Errorf("Failed to run migration with real database URL: %v", err)
				}
			},
		},
		{
			name: "rollback_flag_usage",
			testFunc: func(t *testing.T, realDBURL string) {
				// Run forward migration first
				err := run(realDBURL, false)
				if err != nil {
					t.Errorf("Failed to run forward migration: %v", err)
					return
				}

				// Test rollback
				err = run(realDBURL, true)
				if err != nil {
					t.Errorf("Failed to run rollback: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t, dbURL)
		})
	}
}

// startPostgreSQLContainer starts a PostgreSQL container for testing
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

	// Get connection string
	dbURL, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get connection string: %v", err)
	}

	t.Logf("PostgreSQL container started with URL: %s", dbURL)

	return postgresContainer, dbURL
}

// verifyMigrationState verifies the database state after migration/rollback
func verifyMigrationState(dbURL string, rollback bool) error {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Check if migrations table exists
	var exists bool
	err = db.QueryRow("SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'migrations')").Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check migrations table: %w", err)
	}

	if !exists {
		return fmt.Errorf("migrations table does not exist")
	}

	// Check applied migrations
	rows, err := db.Query("SELECT name FROM migrations ORDER BY id")
	if err != nil {
		return fmt.Errorf("failed to query migrations: %w", err)
	}
	defer rows.Close()

	var appliedMigrations []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("failed to scan migration name: %w", err)
		}
		appliedMigrations = append(appliedMigrations, name)
	}

	if rollback {
		// After rollback, we should have fewer migrations
		if len(appliedMigrations) > 1 {
			return fmt.Errorf("expected 1 or fewer migrations after rollback, got %d", len(appliedMigrations))
		}
	} else {
		// After forward migration, we should have both migrations
		if len(appliedMigrations) != 2 {
			return fmt.Errorf("expected 2 migrations after forward migration, got %d", len(appliedMigrations))
		}

		// Verify specific migrations are applied
		expectedMigrations := []string{"001_initial_schema", "002_retention_policies"}
		for i, expected := range expectedMigrations {
			if i >= len(appliedMigrations) || appliedMigrations[i] != expected {
				return fmt.Errorf("expected migration %s, got %s", expected, appliedMigrations[i])
			}
		}

		// Verify that the actual schema objects exist
		err = verifySchemaObjects(db)
		if err != nil {
			return fmt.Errorf("schema verification failed: %w", err)
		}
	}

	return nil
}

// verifySchemaObjects checks that the expected database objects exist
func verifySchemaObjects(db *sql.DB) error {
	// Tables that should exist after migration
	expectedTables := []string{"flights", "aircraft_states", "system_stats"}

	for _, table := range expectedTables {
		var exists bool
		err := db.QueryRow("SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)", table).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check table %s: %w", table, err)
		}
		if !exists {
			return fmt.Errorf("table %s does not exist", table)
		}
	}

	return nil
}
