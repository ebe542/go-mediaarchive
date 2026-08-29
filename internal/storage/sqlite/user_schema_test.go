package sqlite_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestUserSchemaIncludesPaginationIndex(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	var indexSQL string
	if err := database.QueryRowContext(
		ctx,
		`
			SELECT sql
			FROM sqlite_master
			WHERE type = 'index'
			  AND name = 'users_created_at_id_index'
		`,
	).Scan(&indexSQL); err != nil {
		t.Fatalf("find pagination index: %v", err)
	}

	if !strings.Contains(indexSQL, "users (created_at, id)") {
		t.Fatalf("unexpected pagination index definition %q", indexSQL)
	}
}

func TestUserSchemaEnforcesRoleAndActiveState(t *testing.T) {
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

	const insertUser = `
		INSERT INTO users (
			id,
			username,
			display_name,
			role,
			active,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	const timestamp = "2026-08-18T10:30:00Z"

	if _, err := database.ExecContext(
		ctx,
		insertUser,
		"123e4567-e89b-12d3-a456-426614174000",
		"alice",
		"Alice",
		"viewer",
		1,
		timestamp,
		timestamp,
	); err != nil {
		t.Fatalf("insert valid user: %v", err)
	}

	if _, err := database.ExecContext(
		ctx,
		insertUser,
		"223e4567-e89b-12d3-a456-426614174000",
		"invalid-role",
		"Invalid Role",
		"owner",
		1,
		timestamp,
		timestamp,
	); err == nil {
		t.Fatal("expected invalid role to be rejected")
	}

	if _, err := database.ExecContext(
		ctx,
		insertUser,
		"323e4567-e89b-12d3-a456-426614174000",
		"invalid-active",
		"Invalid Active State",
		"viewer",
		2,
		timestamp,
		timestamp,
	); err == nil {
		t.Fatal("expected invalid active state to be rejected")
	}
}
