package testutils

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/saviobatista/sbs-logger/internal/types"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// MockSBSMessage creates a mock SBS message for testing
func MockSBSMessage(msgType int, hexIdent string) *types.SBSMessage {
	return &types.SBSMessage{
		Raw:       fmt.Sprintf("MSG,%d,111,11111,111111,%s,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111,111111", msgType, hexIdent),
		Timestamp: time.Now().UTC(),
		Source:    "test-source",
	}
}

// WaitForCondition waits for a condition to be true with timeout
func WaitForCondition(condition func() bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for condition")
		case <-ticker.C:
			if condition() {
				return nil
			}
		}
	}
}

// WaitForPostgresReady waits for PostgreSQL to be fully ready for DDL operations
// This function can be used by any service that needs to ensure PostgreSQL containers
// are fully ready before running tests that require DDL operations.
func WaitForPostgresReady(ctx context.Context, container *postgres.PostgresContainer) error {
	// Get connection string
	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		return err
	}

	// Add SSL mode
	connStrWithSSL := connStr + "&sslmode=disable"

	// Try to connect and execute a simple query
	for i := 0; i < 30; i++ { // Try for up to 30 seconds
		db, err := sql.Open("postgres", connStrWithSSL)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		defer db.Close()

		// Test with a timeout
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		// Try to execute a simple query
		_, err = db.ExecContext(ctx, "SELECT 1")
		if err == nil {
			// Database is ready
			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return context.DeadlineExceeded
}

// IsIntegrationTest returns true if integration tests are enabled
func IsIntegrationTest() bool {
	return true // This can be controlled by build tags
}
