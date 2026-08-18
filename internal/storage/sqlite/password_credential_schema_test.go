package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestPasswordCredentialSchemaEnforcesUserAndHashFormat(t *testing.T) {
	t.Parallel()

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

	const timestamp = "2026-08-18T19:00:00Z"
	const userID = "123e4567-e89b-12d3-a456-426614174000"

	if _, err := database.ExecContext(
		ctx,
		`DELETE FROM password_credentials WHERE user_id = ?`,
		userID,
	); err != nil {
		t.Fatalf("remove valid credential fixture: %v", err)
	}

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
		"bootstrap_admin",
		"Bootstrap Administrator",
		"admin",
		1,
		timestamp,
		timestamp,
	)
	if err != nil {
		t.Fatalf("insert user fixture: %v", err)
	}

	const insertCredential = `
		INSERT INTO password_credentials (
			user_id,
			password_hash,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?)
	`

	const encodedHash = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA"

	if _, err := database.ExecContext(
		ctx,
		insertCredential,
		userID,
		encodedHash,
		timestamp,
		timestamp,
	); err != nil {
		t.Fatalf("insert valid password credential: %v", err)
	}

	if _, err := database.ExecContext(
		ctx,
		insertCredential,
		"223e4567-e89b-12d3-a456-426614174000",
		encodedHash,
		timestamp,
		timestamp,
	); err == nil {
		t.Fatal("expected credential without user to be rejected")
	}

	if _, err := database.ExecContext(
		ctx,
		insertCredential,
		userID,
		"plaintext-password",
		timestamp,
		timestamp,
	); err == nil {
		t.Fatal("expected non-Argon2id credential to be rejected")
	}
}
