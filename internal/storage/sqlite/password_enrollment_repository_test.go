package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestPasswordEnrollmentRepositorySavesFindsAndReplacesEnrollment(
	t *testing.T,
) {
	ctx := context.Background()
	database := openEnrollmentTestDatabase(t, ctx)
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	createEnrollmentUserFixture(t, ctx, database, userID, now)
	repository := sqlitestore.NewPasswordEnrollmentRepository(database)

	firstEnrollment, err := credential.NewPasswordEnrollment(
		userID,
		sha256.Sum256([]byte("first-token")),
		now,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create first enrollment fixture: %v", err)
	}
	if err := repository.Save(ctx, firstEnrollment); err != nil {
		t.Fatalf("save first enrollment: %v", err)
	}

	storedEnrollment, err := repository.FindByTokenHash(
		ctx,
		firstEnrollment.TokenHash,
	)
	if err != nil {
		t.Fatalf("find first enrollment: %v", err)
	}
	if storedEnrollment != firstEnrollment {
		t.Fatalf("expected enrollment %#v, got %#v", firstEnrollment, storedEnrollment)
	}

	secondEnrollment, err := credential.NewPasswordEnrollment(
		userID,
		sha256.Sum256([]byte("second-token")),
		now.Add(time.Hour),
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create second enrollment fixture: %v", err)
	}
	if err := repository.Save(ctx, secondEnrollment); err != nil {
		t.Fatalf("replace enrollment: %v", err)
	}

	_, err = repository.FindByTokenHash(ctx, firstEnrollment.TokenHash)
	if !errors.Is(err, credential.ErrPasswordEnrollmentNotFound) {
		t.Fatalf("expected replaced token to be invalid, got %v", err)
	}

	storedEnrollment, err = repository.FindByTokenHash(
		ctx,
		secondEnrollment.TokenHash,
	)
	if err != nil {
		t.Fatalf("find replacement enrollment: %v", err)
	}
	if storedEnrollment != secondEnrollment {
		t.Fatalf("expected enrollment %#v, got %#v", secondEnrollment, storedEnrollment)
	}
}

func TestPasswordEnrollmentRepositoryReturnsNotFoundForUnknownToken(
	t *testing.T,
) {
	ctx := context.Background()
	database := openEnrollmentTestDatabase(t, ctx)
	repository := sqlitestore.NewPasswordEnrollmentRepository(database)

	_, err := repository.FindByTokenHash(
		ctx,
		sha256.Sum256([]byte("unknown-token")),
	)
	if !errors.Is(err, credential.ErrPasswordEnrollmentNotFound) {
		t.Fatalf("expected ErrPasswordEnrollmentNotFound, got %v", err)
	}
}

func openEnrollmentTestDatabase(
	t *testing.T,
	ctx context.Context,
) *sql.DB {
	t.Helper()

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

	return database
}

func createEnrollmentUserFixture(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	userID string,
	now time.Time,
) {
	t.Helper()

	user, err := identity.NewUser(
		userID,
		"enrollment_user",
		"Enrollment User",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}
	if err := sqlitestore.NewUserRepository(database).Create(ctx, user); err != nil {
		t.Fatalf("store user fixture: %v", err)
	}
}

func insertEnrollmentUserFixture(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	userID string,
	username string,
	timestamp string,
) {
	t.Helper()

	_, err := database.ExecContext(
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
		username,
		"Schema Enrollment User",
		"viewer",
		1,
		timestamp,
		timestamp,
	)
	if err != nil {
		t.Fatalf("insert user fixture: %v", err)
	}
}
