package migrations

import (
	"database/sql"
	"fmt"
	"os"
	"time"
)

// Migration represents a database migration
type Migration struct {
	ID        string
	Name      string
	UpSQL     string
	DownSQL   string
	CreatedAt time.Time
}

// Migrator manages database migrations
type Migrator struct {
	db *sql.DB
}

// New creates a new Migrator
func New(db *sql.DB) *Migrator {
	return &Migrator{db: db}
}

// Initialize creates the migrations table if it doesn't exist
func (m *Migrator) Initialize() error {
	query := `
		CREATE TABLE IF NOT EXISTS migrations (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`
	_, err := m.db.Exec(query)
	return err
}

// GetAppliedMigrations returns a list of applied migrations
func (m *Migrator) GetAppliedMigrations() (map[string]bool, error) {
	query := `SELECT name FROM migrations ORDER BY id`
	rows, err := m.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "error closing rows: %v\n", cerr)
		}
	}()

	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

// executeMigration executes a migration with common transaction logic
func (m *Migrator) executeMigration(migration *Migration, sql, recordQuery string, recordArgs ...interface{}) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			fmt.Printf("Warning: failed to rollback transaction: %v\n", err)
		}
	}()

	// Execute the migration
	if _, err := tx.Exec(sql); err != nil {
		return fmt.Errorf("failed to execute migration %s: %w", migration.Name, err)
	}

	// Record the migration
	if _, err := tx.Exec(recordQuery, recordArgs...); err != nil {
		return fmt.Errorf("failed to record migration %s: %w", migration.Name, err)
	}

	return tx.Commit()
}

// ApplyMigration applies a single migration
func (m *Migrator) ApplyMigration(migration *Migration) error {
	return m.executeMigration(
		migration,
		migration.UpSQL,
		"INSERT INTO migrations (name) VALUES ($1)",
		migration.Name,
	)
}

// RollbackMigration rolls back a single migration
func (m *Migrator) RollbackMigration(migration *Migration) error {
	return m.executeMigration(
		migration,
		migration.DownSQL,
		"DELETE FROM migrations WHERE name = $1",
		migration.Name,
	)
}

// Migrate applies all pending migrations
func (m *Migrator) Migrate(migrations []*Migration) error {
	// Initialize migrations table
	if err := m.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize migrations: %w", err)
	}

	// Get applied migrations
	applied, err := m.GetAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Apply pending migrations
	for _, migration := range migrations {
		if !applied[migration.Name] {
			if err := m.ApplyMigration(migration); err != nil {
				return fmt.Errorf("failed to apply migration %s: %w", migration.Name, err)
			}
			fmt.Printf("Applied migration: %s\n", migration.Name)
		}
	}

	return nil
}

// Rollback rolls back the last migration
func (m *Migrator) Rollback(migrations []*Migration) error {
	// Get applied migrations
	applied, err := m.GetAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Find the last applied migration
	var lastMigration *Migration
	for i := len(migrations) - 1; i >= 0; i-- {
		if applied[migrations[i].Name] {
			lastMigration = migrations[i]
			break
		}
	}

	if lastMigration == nil {
		return fmt.Errorf("no migrations to rollback")
	}

	// Rollback the last migration
	if err := m.RollbackMigration(lastMigration); err != nil {
		return fmt.Errorf("failed to rollback migration %s: %w", lastMigration.Name, err)
	}

	fmt.Printf("Rolled back migration: %s\n", lastMigration.Name)
	return nil
}
