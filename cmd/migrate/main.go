package main

import (
	"database/sql"
	"flag"
	"log"

	_ "github.com/lib/pq"
	"github.com/savio/sbs-logger/internal/db/migrations"
)

func main() {
	// Parse command line flags
	dbURL := flag.String("db", "postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable", "Database connection string")
	rollback := flag.Bool("rollback", false, "Rollback the last migration")
	flag.Parse()

	// Connect to database
	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	// Create migrator
	migrator := migrations.New(db)

	// Define migrations
	migrationList := []*migrations.Migration{
		migrations.InitialSchema,
		migrations.RetentionPolicies,
	}

	// Execute migration or rollback
	if *rollback {
		if err := migrator.Rollback(migrationList); err != nil {
			log.Fatalf("Failed to rollback migration: %v", err)
		}
	} else {
		if err := migrator.Migrate(migrationList); err != nil {
			log.Fatalf("Failed to apply migrations: %v", err)
		}
	}
}
