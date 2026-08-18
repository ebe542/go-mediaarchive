package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestMigrateAppliesEmbeddedMigrationsOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer database.Close()

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	var version int
	var name string

	if err := database.QueryRowContext(
		ctx,
		`
			SELECT version, name
			FROM schema_migrations
			WHERE version = 1
		`,
	).Scan(&version, &name); err != nil {
		t.Fatalf("query applied migration: %v", err)
	}

	if version != 1 {
		t.Errorf("expected migration version %d, got %d", 1, version)
	}

	const expectedName = "001_initialize.sql"
	if name != expectedName {
		t.Errorf(
			"expected migration name %q, got %q",
			expectedName,
			name,
		)
	}

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations again: %v", err)
	}

	var migrationCount int
	if err := database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM schema_migrations",
	).Scan(&migrationCount); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}

	if migrationCount != 2 {
		t.Fatalf("expected 2 applied migrations, got %d", migrationCount)
	}
}
