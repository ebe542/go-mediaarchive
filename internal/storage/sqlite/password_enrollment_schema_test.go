package sqlite_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestPasswordEnrollmentSchemaEnforcesTokenUserAndTimeConstraints(
	t *testing.T,
) {
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
	const invalidHashUserID = "223e4567-e89b-12d3-a456-426614174000"
	const invalidLifetimeUserID = "323e4567-e89b-12d3-a456-426614174000"
	const createdAt = "2026-08-28T10:00:00Z"
	const expiresAt = "2026-08-29T10:00:00Z"

	insertEnrollmentUserFixture(t, ctx, database, userID, "valid_enrollment", createdAt)
	insertEnrollmentUserFixture(
		t,
		ctx,
		database,
		invalidHashUserID,
		"invalid_hash_enrollment",
		createdAt,
	)
	insertEnrollmentUserFixture(
		t,
		ctx,
		database,
		invalidLifetimeUserID,
		"invalid_lifetime_enrollment",
		createdAt,
	)

	const insertEnrollment = `
		INSERT INTO password_enrollments (
			user_id,
			token_hash,
			created_at,
			expires_at
		)
		VALUES (?, ?, ?, ?)
	`

	if _, err := database.ExecContext(
		ctx,
		insertEnrollment,
		userID,
		bytes.Repeat([]byte{0x01}, 32),
		createdAt,
		expiresAt,
	); err != nil {
		t.Fatalf("insert valid password enrollment: %v", err)
	}

	if _, err := database.ExecContext(
		ctx,
		insertEnrollment,
		"423e4567-e89b-12d3-a456-426614174000",
		bytes.Repeat([]byte{0x02}, 32),
		createdAt,
		expiresAt,
	); err == nil {
		t.Fatal("expected enrollment without user to be rejected")
	}

	if _, err := database.ExecContext(
		ctx,
		insertEnrollment,
		invalidHashUserID,
		bytes.Repeat([]byte{0x03}, 31),
		createdAt,
		expiresAt,
	); err == nil {
		t.Fatal("expected invalid token hash length to be rejected")
	}

	if _, err := database.ExecContext(
		ctx,
		insertEnrollment,
		invalidLifetimeUserID,
		bytes.Repeat([]byte{0x04}, 32),
		createdAt,
		createdAt,
	); err == nil {
		t.Fatal("expected non-positive enrollment lifetime to be rejected")
	}
}
