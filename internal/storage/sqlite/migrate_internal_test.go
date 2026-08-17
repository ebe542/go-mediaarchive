package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMigrateAppliesMigrationsInVersionOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer database.Close()

	migrationFiles := fstest.MapFS{
		"migrations/002_insert_record.sql": {
			Data: []byte(`
				INSERT INTO migration_order (value)
				VALUES ('second migration')
			`),
		},
		"migrations/001_create_table.sql": {
			Data: []byte(`
				CREATE TABLE migration_order (
					value TEXT NOT NULL
				)
			`),
		},
	}

	if err := migrate(ctx, database, migrationFiles); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	var value string
	if err := database.QueryRowContext(
		ctx,
		"SELECT value FROM migration_order",
	).Scan(&value); err != nil {
		t.Fatalf("query migration result: %v", err)
	}

	const expectedValue = "second migration"
	if value != expectedValue {
		t.Errorf(
			"expected migration result %q, got %q",
			expectedValue,
			value,
		)
	}
}
func TestMigrateRollsBackFailedMigration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer database.Close()

	migrationFiles := fstest.MapFS{
		"migrations/001_failing_migration.sql": {
			Data: []byte(`
				CREATE TABLE must_be_rolled_back (
					id INTEGER PRIMARY KEY
				);

				THIS IS NOT VALID SQL;
			`),
		},
	}

	if err := migrate(ctx, database, migrationFiles); err == nil {
		t.Fatal("expected migration failure")
	}

	var tableCount int
	if err := database.QueryRowContext(
		ctx,
		`
			SELECT COUNT(*)
			FROM sqlite_master
			WHERE type = 'table'
			  AND name = 'must_be_rolled_back'
		`,
	).Scan(&tableCount); err != nil {
		t.Fatalf("check rolled-back table: %v", err)
	}

	if tableCount != 0 {
		t.Errorf(
			"expected failed migration table to be absent, got %d",
			tableCount,
		)
	}

	var migrationCount int
	if err := database.QueryRowContext(
		ctx,
		`
			SELECT COUNT(*)
			FROM schema_migrations
			WHERE version = 1
		`,
	).Scan(&migrationCount); err != nil {
		t.Fatalf("check rolled-back migration record: %v", err)
	}

	if migrationCount != 0 {
		t.Errorf(
			"expected failed migration record to be absent, got %d",
			migrationCount,
		)
	}
}

func TestLoadMigrationsRejectsNonPaddedVersion(t *testing.T) {
	t.Parallel()

	migrationFiles := fstest.MapFS{
		"migrations/1_invalid.sql": {
			Data: []byte("SELECT 1;"),
		},
	}

	_, err := loadMigrations(migrationFiles)
	if err == nil {
		t.Fatal("expected a non-padded migration version to be rejected")
	}
}
