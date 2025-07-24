package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
	"github.com/savio/sbs-logger/internal/db/migrations"
)

// parseFlags extracts flag parsing logic for testability
func parseFlags() (string, bool) {
	dbURL := flag.String("db", "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable", "Database connection string")
	rollback := flag.Bool("rollback", false, "Rollback the last migration")
	flag.Parse()
	return *dbURL, *rollback
}

func main() {
	// Parse command line flags
	dbURL, rollback := parseFlags()

	if err := run(dbURL, rollback); err != nil {
		log.Printf("Migration failed: %v", err)
		os.Exit(1)
	}
}

// run contains the main migration logic, extracted for testability
func run(dbURL string, rollback bool) error {
	// Connect to database
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing db: %v\n", err)
		}
	}()

	// Test connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Create migrator
	migrator := migrations.New(db)

	// Define migrations
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
