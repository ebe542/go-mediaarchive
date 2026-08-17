package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestOpenConfiguresFileDatabase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer database.Close()

	var foreignKeys int
	if err := database.QueryRowContext(
		ctx,
		"PRAGMA foreign_keys",
	).Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}

	if foreignKeys != 1 {
		t.Errorf("expected foreign_keys %d, got %d", 1, foreignKeys)
	}

	var journalMode string
	if err := database.QueryRowContext(
		ctx,
		"PRAGMA journal_mode",
	).Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("expected journal_mode %q, got %q", "wal", journalMode)
	}

	var busyTimeout int
	if err := database.QueryRowContext(
		ctx,
		"PRAGMA busy_timeout",
	).Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}

	if busyTimeout != 5000 {
		t.Errorf("expected busy_timeout %d, got %d", 5000, busyTimeout)
	}
}
