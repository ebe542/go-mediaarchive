package sqlite_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestSessionSchemaEnforcesTokenAndUserConstraints(t *testing.T) {
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

	const userID = "123e4567-e89b-12d3-a456-426614174000"
	const createdAt = "2026-08-19T10:00:00Z"
	const lastSeenAt = "2026-08-19T10:00:00Z"
	const expiresAt = "2026-08-19T18:00:00Z"

	_, err = database.ExecContext(
		ctx,
		`
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
		`,
		userID,
		"session_user",
		"Session User",
		"viewer",
		1,
		createdAt,
		createdAt,
	)
	if err != nil {
		t.Fatalf("insert user fixture: %v", err)
	}

	const insertSession = `
		INSERT INTO sessions (
			token_hash,
			user_id,
			created_at,
			last_seen_at,
			expires_at,
			revoked_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	if _, err := database.ExecContext(
		ctx,
		insertSession,
		bytes.Repeat([]byte{0x01}, 32),
		userID,
		createdAt,
		lastSeenAt,
		expiresAt,
		nil,
	); err != nil {
		t.Fatalf("insert valid session: %v", err)
	}

	if _, err := database.ExecContext(
		ctx,
		insertSession,
		bytes.Repeat([]byte{0x02}, 31),
		userID,
		createdAt,
		lastSeenAt,
		expiresAt,
		nil,
	); err == nil {
		t.Fatal("expected invalid token hash length to be rejected")
	}

	if _, err := database.ExecContext(
		ctx,
		insertSession,
		bytes.Repeat([]byte{0x03}, 32),
		"223e4567-e89b-12d3-a456-426614174000",
		createdAt,
		lastSeenAt,
		expiresAt,
		nil,
	); err == nil {
		t.Fatal("expected session without user to be rejected")
	}
}
