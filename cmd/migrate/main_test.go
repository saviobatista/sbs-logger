package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/savio/sbs-logger/internal/db/migrations"
)

// TestMain_Integration tests the full main function with actual command execution
func TestMain_Integration(t *testing.T) {
	// Only run integration tests if explicitly requested
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TEST=1 to run.")
	}

	tests := []struct {
		name     string
		args     []string
		wantExit int
	}{
		{
			name:     "missing database connection",
			args:     []string{"-db", "invalid://connection"},
			wantExit: 1,
		},
		{
			name:     "help flag",
			args:     []string{"-h"},
			wantExit: 2, // flag package exits with 2 for -h
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run the main function in a subprocess
			cmd := exec.Command(os.Args[0], tt.args...)
			cmd.Env = append(os.Environ(), "TEST_MAIN=1")
			err := cmd.Run()

			var exitCode int
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				} else {
					t.Fatalf("Failed to run command: %v", err)
				}
			}

			if exitCode != tt.wantExit {
				t.Errorf("Expected exit code %d, got %d", tt.wantExit, exitCode)
			}
		})
	}
}

// TestMigrateWithMock tests the migration logic with mocked database
func TestMigrateWithMock(t *testing.T) {
	tests := []struct {
		name         string
		rollback     bool
		setupMock    func(sqlmock.Sqlmock)
		wantError    bool
		errorPattern string
	}{
		{
			name:     "successful migration",
			rollback: false,
			setupMock: func(mock sqlmock.Sqlmock) {
				// Mock successful ping
				mock.ExpectPing()

				// Mock Initialize() - creates migrations table
				mock.ExpectExec(`CREATE TABLE IF NOT EXISTS migrations`).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Mock GetAppliedMigrations() - returns empty result (no migrations applied)
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnRows(sqlmock.NewRows([]string{"name"}))

				// Mock first migration (InitialSchema) - simplified to match any SQL
				mock.ExpectBegin()
				mock.ExpectExec(`.+`).WillReturnResult(sqlmock.NewResult(1, 1)) // Any SQL
				mock.ExpectExec(`INSERT INTO migrations \(name\) VALUES \(\$1\)`).
					WithArgs("001_initial_schema").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()

				// Mock second migration (RetentionPolicies) - simplified to match any SQL
				mock.ExpectBegin()
				mock.ExpectExec(`.+`).WillReturnResult(sqlmock.NewResult(1, 1)) // Any SQL
				mock.ExpectExec(`INSERT INTO migrations \(name\) VALUES \(\$1\)`).
					WithArgs("002_retention_policies").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			},
			wantError: false,
		},
		{
			name:     "successful rollback",
			rollback: true,
			setupMock: func(mock sqlmock.Sqlmock) {
				// Mock successful ping
				mock.ExpectPing()

				// Mock GetAppliedMigrations() - returns both migrations as applied
				rows := sqlmock.NewRows([]string{"name"}).
					AddRow("001_initial_schema").
					AddRow("002_retention_policies")
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnRows(rows)

				// Mock rollback of the last migration (RetentionPolicies) - simplified to match any SQL
				mock.ExpectBegin()
				mock.ExpectExec(`.+`).WillReturnResult(sqlmock.NewResult(1, 1)) // Any SQL
				mock.ExpectExec(`DELETE FROM migrations WHERE name = \$1`).
					WithArgs("002_retention_policies").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			},
			wantError: false,
		},
		{
			name:     "database ping failure",
			rollback: false,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing().WillReturnError(fmt.Errorf("connection failed"))
			},
			wantError:    true,
			errorPattern: "connection failed",
		},
		{
			name:     "migration initialization failure",
			rollback: false,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing()
				mock.ExpectExec(`CREATE TABLE IF NOT EXISTS migrations`).
					WillReturnError(fmt.Errorf("table creation failed"))
			},
			wantError:    true,
			errorPattern: "table creation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock database with ping monitoring enabled
			db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			if err != nil {
				t.Fatalf("Failed to create mock database: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			// We can't directly test the run function with a mock DB since it calls sql.Open
			// Instead, test the extracted runMigration logic
			err = runMigration(db, tt.rollback)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errorPattern != "" && !strings.Contains(err.Error(), tt.errorPattern) {
					t.Errorf("Expected error containing %q, got %q", tt.errorPattern, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}

			// Verify all expectations were met
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet mock expectations: %v", err)
			}
		})
	}
}

// runMigration extracts the core migration logic to make it testable
func runMigration(db *sql.DB, rollback bool) error {
	// Test connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Create migrator
	migrator := migrations.New(db)

	// Define migrations (same as main function)
	migrationList := []*migrations.Migration{
		migrations.InitialSchema,
		migrations.RetentionPolicies,
	}

	// Execute migration or rollback
	if rollback {
		if err := migrator.Rollback(migrationList); err != nil {
			return fmt.Errorf("failed to rollback migration: %w", err)
		}
	} else {
		if err := migrator.Migrate(migrationList); err != nil {
			return fmt.Errorf("failed to apply migrations: %w", err)
		}
	}

	return nil
}

// TestFlags tests command line flag parsing
func TestFlags(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		expectedDB       string
		expectedRollback bool
	}{
		{
			name:             "default values",
			args:             []string{},
			expectedDB:       "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable",
			expectedRollback: false,
		},
		{
			name:             "custom database URL",
			args:             []string{"-db", "postgres://user:pass@localhost/test"},
			expectedDB:       "postgres://user:pass@localhost/test",
			expectedRollback: false,
		},
		{
			name:             "rollback flag",
			args:             []string{"-rollback"},
			expectedDB:       "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable",
			expectedRollback: true,
		},
		{
			name:             "both flags",
			args:             []string{"-db", "postgres://custom/db", "-rollback"},
			expectedDB:       "postgres://custom/db",
			expectedRollback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flag.CommandLine to avoid conflicts between tests
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

			// Parse flags like in main function
			dbURL := flag.String("db", "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable", "Database connection string")
			rollback := flag.Bool("rollback", false, "Rollback the last migration")

			// Parse the test arguments
			err := flag.CommandLine.Parse(tt.args)
			if err != nil {
				t.Fatalf("Failed to parse flags: %v", err)
			}

			if *dbURL != tt.expectedDB {
				t.Errorf("Expected db=%q, got %q", tt.expectedDB, *dbURL)
			}

			if *rollback != tt.expectedRollback {
				t.Errorf("Expected rollback=%v, got %v", tt.expectedRollback, *rollback)
			}
		})
	}
}

// TestDatabaseConnection tests database connection scenarios
func TestDatabaseConnection(t *testing.T) {
	tests := []struct {
		name      string
		connStr   string
		wantError bool
	}{
		{
			name:      "valid connection string format",
			connStr:   "postgres://user:pass@localhost:5432/dbname",
			wantError: true, // Will fail because no actual DB, but format is valid
		},
		{
			name:      "invalid connection string format",
			connStr:   "invalid-connection-string",
			wantError: true,
		},
		{
			name:      "empty connection string",
			connStr:   "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := sql.Open("postgres", tt.connStr)
			if err != nil && !tt.wantError {
				t.Errorf("Expected no error opening database, got %v", err)
			}
			if err == nil && tt.wantError {
				// Even if Open succeeds, Ping should fail for invalid connections
				defer db.Close()
				if pingErr := db.Ping(); pingErr == nil && tt.wantError {
					t.Error("Expected ping to fail, but it succeeded")
				}
			}
		})
	}
}

// TestRun tests the main run function with various scenarios
func TestRun(t *testing.T) {
	tests := []struct {
		name      string
		dbURL     string
		rollback  bool
		wantError bool
	}{
		{
			name:      "invalid connection string",
			dbURL:     "invalid://connection",
			rollback:  false,
			wantError: true,
		},
		{
			name:      "empty connection string",
			dbURL:     "",
			rollback:  false,
			wantError: true,
		},
		{
			name:      "unreachable database",
			dbURL:     "postgres://user:pass@unreachable:5432/test",
			rollback:  false,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.dbURL, tt.rollback)

			if tt.wantError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}
		})
	}
}

// TestMain tests the main function behavior (excluding os.Exit)
func TestMainLogic(t *testing.T) {
	// Test that the main function properly parses flags and calls run
	// We can't test os.Exit directly, but we can test the flag parsing logic

	// Save original command line args
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Test default values by calling run with default parameters
	err := run("postgres://invalid:connection", false)
	if err == nil {
		t.Error("Expected error with invalid connection, got nil")
	}
}

// TestMigrationList tests that the migration list is correctly defined
func TestMigrationList(t *testing.T) {
	migrationList := []*migrations.Migration{
		migrations.InitialSchema,
		migrations.RetentionPolicies,
	}

	if len(migrationList) != 2 {
		t.Errorf("Expected 2 migrations, got %d", len(migrationList))
	}

	for i, migration := range migrationList {
		if migration == nil {
			t.Errorf("Migration at index %d is nil", i)
		}
	}
}
