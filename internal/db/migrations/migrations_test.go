package migrations

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNew(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock DB: %v", err)
	}
	defer db.Close()

	migrator := New(db)
	if migrator == nil {
		t.Error("Expected migrator to be created, got nil")
		return
	}
	if migrator.db != db {
		t.Error("Expected migrator to have the provided DB connection")
	}
}

func TestMigratorInitialize(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(sqlmock.Sqlmock)
		expectError bool
	}{
		{
			name: "successful initialization",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`CREATE TABLE IF NOT EXISTS migrations`).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			expectError: false,
		},
		{
			name: "database error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`CREATE TABLE IF NOT EXISTS migrations`).
					WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			migrator := New(db)
			err = migrator.Initialize()

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestMigratorGetAppliedMigrations(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func(sqlmock.Sqlmock)
		expectError   bool
		expectedCount int
		expectedNames []string
	}{
		{
			name: "no applied migrations",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"name"})
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnRows(rows)
			},
			expectError:   false,
			expectedCount: 0,
		},
		{
			name: "multiple applied migrations",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"name"}).
					AddRow("001_initial_schema").
					AddRow("002_retention_policies")
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnRows(rows)
			},
			expectError:   false,
			expectedCount: 2,
			expectedNames: []string{"001_initial_schema", "002_retention_policies"},
		},
		{
			name: "database query error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
		{
			name: "scan error",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"name"}).
					AddRow("001_initial_schema").
					RowError(0, sql.ErrNoRows)
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnRows(rows)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			migrator := New(db)
			applied, err := migrator.GetAppliedMigrations()

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if !tt.expectError {
				if len(applied) != tt.expectedCount {
					t.Errorf("Expected %d applied migrations, got %d", tt.expectedCount, len(applied))
				}
				for _, name := range tt.expectedNames {
					if !applied[name] {
						t.Errorf("Expected migration %s to be applied", name)
					}
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestMigratorApplyMigration(t *testing.T) {
	migration := &Migration{
		ID:      "test_migration",
		Name:    "test_migration",
		UpSQL:   "CREATE TABLE test (id INTEGER);",
		DownSQL: "DROP TABLE test;",
	}

	tests := []struct {
		name        string
		setupMock   func(sqlmock.Sqlmock)
		expectError bool
	}{
		{
			name: "successful migration application",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectExec(`CREATE TABLE test`).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`INSERT INTO migrations`).
					WithArgs("test_migration").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			},
			expectError: false,
		},
		{
			name: "begin transaction error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin().WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
		{
			name: "migration execution error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectExec(`CREATE TABLE test`).
					WillReturnError(sql.ErrConnDone)
				mock.ExpectRollback()
			},
			expectError: true,
		},
		{
			name: "record migration error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectExec(`CREATE TABLE test`).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`INSERT INTO migrations`).
					WithArgs("test_migration").
					WillReturnError(sql.ErrConnDone)
				mock.ExpectRollback()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			migrator := New(db)
			err = migrator.ApplyMigration(migration)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestMigratorRollbackMigration(t *testing.T) {
	migration := &Migration{
		ID:      "test_migration",
		Name:    "test_migration",
		UpSQL:   "CREATE TABLE test (id INTEGER);",
		DownSQL: "DROP TABLE test;",
	}

	tests := []struct {
		name        string
		setupMock   func(sqlmock.Sqlmock)
		expectError bool
	}{
		{
			name: "successful migration rollback",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectExec(`DROP TABLE test`).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`DELETE FROM migrations WHERE name`).
					WithArgs("test_migration").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			},
			expectError: false,
		},
		{
			name: "rollback SQL execution error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectExec(`DROP TABLE test`).
					WillReturnError(sql.ErrConnDone)
				mock.ExpectRollback()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			migrator := New(db)
			err = migrator.RollbackMigration(migration)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestMigratorMigrate(t *testing.T) {
	migrations := []*Migration{
		{
			ID:      "001_test",
			Name:    "001_test",
			UpSQL:   "CREATE TABLE test1 (id INTEGER);",
			DownSQL: "DROP TABLE test1;",
		},
		{
			ID:      "002_test",
			Name:    "002_test",
			UpSQL:   "CREATE TABLE test2 (id INTEGER);",
			DownSQL: "DROP TABLE test2;",
		},
	}

	tests := []struct {
		name        string
		setupMock   func(sqlmock.Sqlmock)
		expectError bool
	}{
		{
			name: "successful migration of all pending",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Initialize
				mock.ExpectExec(`CREATE TABLE IF NOT EXISTS migrations`).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Get applied migrations (empty)
				rows := sqlmock.NewRows([]string{"name"})
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnRows(rows)

				// Apply first migration
				mock.ExpectBegin()
				mock.ExpectExec(`CREATE TABLE test1`).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`INSERT INTO migrations`).
					WithArgs("001_test").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()

				// Apply second migration
				mock.ExpectBegin()
				mock.ExpectExec(`CREATE TABLE test2`).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`INSERT INTO migrations`).
					WithArgs("002_test").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			},
			expectError: false,
		},
		{
			name: "partial migrations already applied",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Initialize
				mock.ExpectExec(`CREATE TABLE IF NOT EXISTS migrations`).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Get applied migrations (first one already applied)
				rows := sqlmock.NewRows([]string{"name"}).AddRow("001_test")
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnRows(rows)

				// Apply only second migration
				mock.ExpectBegin()
				mock.ExpectExec(`CREATE TABLE test2`).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`INSERT INTO migrations`).
					WithArgs("002_test").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			},
			expectError: false,
		},
		{
			name: "initialization error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`CREATE TABLE IF NOT EXISTS migrations`).
					WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
		{
			name: "get applied migrations error",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Initialize
				mock.ExpectExec(`CREATE TABLE IF NOT EXISTS migrations`).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Get applied migrations error
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			migrator := New(db)
			err = migrator.Migrate(migrations)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestMigratorRollback(t *testing.T) {
	migrations := []*Migration{
		{
			ID:      "001_test",
			Name:    "001_test",
			UpSQL:   "CREATE TABLE test1 (id INTEGER);",
			DownSQL: "DROP TABLE test1;",
		},
		{
			ID:      "002_test",
			Name:    "002_test",
			UpSQL:   "CREATE TABLE test2 (id INTEGER);",
			DownSQL: "DROP TABLE test2;",
		},
	}

	tests := []struct {
		name        string
		setupMock   func(sqlmock.Sqlmock)
		expectError bool
	}{
		{
			name: "successful rollback of last migration",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Get applied migrations (both applied)
				rows := sqlmock.NewRows([]string{"name"}).
					AddRow("001_test").
					AddRow("002_test")
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnRows(rows)

				// Rollback last migration (002_test)
				mock.ExpectBegin()
				mock.ExpectExec(`DROP TABLE test2`).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`DELETE FROM migrations WHERE name`).
					WithArgs("002_test").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			},
			expectError: false,
		},
		{
			name: "no migrations to rollback",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Get applied migrations (empty)
				rows := sqlmock.NewRows([]string{"name"})
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnRows(rows)
			},
			expectError: true,
		},
		{
			name: "get applied migrations error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT name FROM migrations ORDER BY id`).
					WillReturnError(sql.ErrConnDone)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock DB: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)

			migrator := New(db)
			err = migrator.Rollback(migrations)

			if tt.expectError && err == nil {
				t.Error("Expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unmet expectations: %v", err)
			}
		})
	}
}

func TestMigrationStruct(t *testing.T) {
	// Test the Migration struct and its fields
	migration := &Migration{
		ID:        "test_001",
		Name:      "test_migration",
		UpSQL:     "CREATE TABLE test (id INTEGER);",
		DownSQL:   "DROP TABLE test;",
		CreatedAt: time.Now(),
	}

	if migration.ID != "test_001" {
		t.Errorf("Expected ID 'test_001', got '%s'", migration.ID)
	}
	if migration.Name != "test_migration" {
		t.Errorf("Expected Name 'test_migration', got '%s'", migration.Name)
	}
	if migration.UpSQL != "CREATE TABLE test (id INTEGER);" {
		t.Errorf("Expected UpSQL to match, got '%s'", migration.UpSQL)
	}
	if migration.DownSQL != "DROP TABLE test;" {
		t.Errorf("Expected DownSQL to match, got '%s'", migration.DownSQL)
	}
	if migration.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
}
