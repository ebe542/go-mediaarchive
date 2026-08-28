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
	if err := repository.SaveForCredentiallessUser(ctx, firstEnrollment); err != nil {
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
	if err := repository.SaveForCredentiallessUser(ctx, secondEnrollment); err != nil {
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

func TestPasswordEnrollmentRepositoryRejectsMissingUser(t *testing.T) {
	ctx := context.Background()
	database := openEnrollmentTestDatabase(t, ctx)
	repository := sqlitestore.NewPasswordEnrollmentRepository(database)

	enrollment, err := credential.NewPasswordEnrollment(
		"123e4567-e89b-12d3-a456-426614174000",
		sha256.Sum256([]byte("missing-user-token")),
		time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC),
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create enrollment fixture: %v", err)
	}

	err = repository.SaveForCredentiallessUser(ctx, enrollment)
	if !errors.Is(err, identity.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestPasswordEnrollmentRepositoryRejectsUserWithCredential(t *testing.T) {
	ctx := context.Background()
	database := openEnrollmentTestDatabase(t, ctx)
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	createEnrollmentUserFixture(t, ctx, database, userID, now)
	passwordCredential, err := credential.NewPasswordCredential(
		userID,
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		now,
	)
	if err != nil {
		t.Fatalf("create password credential fixture: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`
			INSERT INTO password_credentials (
				user_id,
				password_hash,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?)
		`,
		passwordCredential.UserID,
		passwordCredential.PasswordHash,
		passwordCredential.CreatedAt.Format(time.RFC3339Nano),
		passwordCredential.UpdatedAt.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("store password credential fixture: %v", err)
	}

	enrollment, err := credential.NewPasswordEnrollment(
		userID,
		sha256.Sum256([]byte("credential-user-token")),
		now,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create enrollment fixture: %v", err)
	}

	err = sqlitestore.NewPasswordEnrollmentRepository(
		database,
	).SaveForCredentiallessUser(ctx, enrollment)
	if !errors.Is(err, credential.ErrPasswordCredentialExists) {
		t.Fatalf("expected ErrPasswordCredentialExists, got %v", err)
	}
}

func TestPasswordEnrollmentRepositoryCreatesCredentialAndConsumesEnrollment(
	t *testing.T,
) {
	ctx := context.Background()
	database := openEnrollmentTestDatabase(t, ctx)
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	createEnrollmentUserFixture(t, ctx, database, userID, now)
	repository := sqlitestore.NewPasswordEnrollmentRepository(database)
	enrollment, err := credential.NewPasswordEnrollment(
		userID,
		sha256.Sum256([]byte("consumed-token")),
		now,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create enrollment fixture: %v", err)
	}
	if err := repository.SaveForCredentiallessUser(ctx, enrollment); err != nil {
		t.Fatalf("save enrollment fixture: %v", err)
	}

	passwordCredential, err := credential.NewPasswordCredential(
		userID,
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("create password credential fixture: %v", err)
	}
	if err := repository.CreateCredentialAndConsume(
		ctx,
		enrollment.TokenHash,
		passwordCredential,
		now.Add(time.Minute),
	); err != nil {
		t.Fatalf("complete enrollment: %v", err)
	}

	storedCredential, err := sqlitestore.NewPasswordCredentialRepository(
		database,
	).FindByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("find created credential: %v", err)
	}
	if storedCredential != passwordCredential {
		t.Fatalf("expected credential %#v, got %#v", passwordCredential, storedCredential)
	}

	_, err = repository.FindByTokenHash(ctx, enrollment.TokenHash)
	if !errors.Is(err, credential.ErrPasswordEnrollmentNotFound) {
		t.Fatalf("expected consumed enrollment to be absent, got %v", err)
	}
}

func TestPasswordEnrollmentRepositoryPreservesExpiredEnrollment(t *testing.T) {
	ctx := context.Background()
	database := openEnrollmentTestDatabase(t, ctx)
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	createEnrollmentUserFixture(t, ctx, database, userID, now)
	repository := sqlitestore.NewPasswordEnrollmentRepository(database)
	enrollment, err := credential.NewPasswordEnrollment(
		userID,
		sha256.Sum256([]byte("expired-token")),
		now,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("create enrollment fixture: %v", err)
	}
	if err := repository.SaveForCredentiallessUser(ctx, enrollment); err != nil {
		t.Fatalf("save enrollment fixture: %v", err)
	}

	passwordCredential, err := credential.NewPasswordCredential(
		userID,
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		now.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("create password credential fixture: %v", err)
	}
	err = repository.CreateCredentialAndConsume(
		ctx,
		enrollment.TokenHash,
		passwordCredential,
		now.Add(time.Hour),
	)
	if !errors.Is(err, credential.ErrPasswordEnrollmentNotFound) {
		t.Fatalf("expected expired enrollment to be rejected, got %v", err)
	}

	if _, err := repository.FindByTokenHash(ctx, enrollment.TokenHash); err != nil {
		t.Fatalf("expected expired enrollment to remain stored: %v", err)
	}
	_, err = sqlitestore.NewPasswordCredentialRepository(
		database,
	).FindByUserID(ctx, userID)
	if !errors.Is(err, credential.ErrPasswordCredentialNotFound) {
		t.Fatalf("expected credential creation to roll back, got %v", err)
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
