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
	"github.com/ebe542/go-mediaarchive/internal/session"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestPasswordCredentialRepositoryFindsCredentialByUserID(
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

	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	adminUser, err := identity.NewUser(
		userID,
		"archive_admin",
		"Archive Administrator",
		identity.RoleAdmin,
		now,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	expectedCredential, err := credential.NewPasswordCredential(
		userID,
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		now,
	)
	if err != nil {
		t.Fatalf("create credential fixture: %v", err)
	}

	if err := sqlitestore.NewAdminBootstrapRepository(
		database,
	).BootstrapAdmin(
		ctx,
		adminUser,
		expectedCredential,
	); err != nil {
		t.Fatalf("store credential fixture: %v", err)
	}

	repository := sqlitestore.NewPasswordCredentialRepository(database)

	storedCredential, err := repository.FindByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("find password credential: %v", err)
	}

	if storedCredential != expectedCredential {
		t.Fatalf(
			"expected credential %#v, got %#v",
			expectedCredential,
			storedCredential,
		)
	}
}

func TestPasswordChangeUpdatesCredentialAndRevokesEverySession(t *testing.T) {
	ctx := context.Background()
	database, repository, storedCredential := passwordChangeRepositoryFixture(
		t,
		ctx,
	)
	changedAt := storedCredential.CreatedAt.Add(time.Hour)
	updatedCredential, err := storedCredential.WithPasswordHash(
		"$argon2id$updated-hash",
		changedAt,
	)
	if err != nil {
		t.Fatalf("create updated credential: %v", err)
	}

	if err := repository.ChangePasswordAndRevokeSessions(
		ctx,
		updatedCredential,
	); err != nil {
		t.Fatalf("change password and revoke sessions: %v", err)
	}

	actualCredential, err := repository.FindByUserID(
		ctx,
		storedCredential.UserID,
	)
	if err != nil {
		t.Fatalf("find updated credential: %v", err)
	}
	if actualCredential != updatedCredential {
		t.Fatalf(
			"expected credential %+v, got %+v",
			updatedCredential,
			actualCredential,
		)
	}

	var activeSessions int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND revoked_at IS NULL`,
		storedCredential.UserID,
	).Scan(&activeSessions); err != nil {
		t.Fatalf("count active sessions: %v", err)
	}
	if activeSessions != 0 {
		t.Fatalf("expected no active sessions, got %d", activeSessions)
	}

	var matchingRevocations int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND revoked_at = ?`,
		storedCredential.UserID,
		changedAt.Format(time.RFC3339Nano),
	).Scan(&matchingRevocations); err != nil {
		t.Fatalf("count revoked sessions: %v", err)
	}
	if matchingRevocations != 2 {
		t.Fatalf(
			"expected two sessions revoked at password change, got %d",
			matchingRevocations,
		)
	}
}

func TestPasswordChangeRollsBackCredentialWhenSessionRevocationFails(
	t *testing.T,
) {
	ctx := context.Background()
	database, repository, storedCredential := passwordChangeRepositoryFixture(
		t,
		ctx,
	)

	_, err := database.ExecContext(
		ctx,
		`
			CREATE TRIGGER reject_session_revocation
			BEFORE UPDATE OF revoked_at ON sessions
			BEGIN
				SELECT RAISE(ABORT, 'synthetic revocation failure');
			END
		`,
	)
	if err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	updatedCredential, err := storedCredential.WithPasswordHash(
		"$argon2id$must-be-rolled-back",
		storedCredential.CreatedAt.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("create updated credential: %v", err)
	}

	err = repository.ChangePasswordAndRevokeSessions(
		ctx,
		updatedCredential,
	)
	if err == nil {
		t.Fatal("expected session revocation failure")
	}

	actualCredential, err := repository.FindByUserID(
		ctx,
		storedCredential.UserID,
	)
	if err != nil {
		t.Fatalf("find rolled-back credential: %v", err)
	}
	if actualCredential != storedCredential {
		t.Fatalf(
			"expected original credential %+v, got %+v",
			storedCredential,
			actualCredential,
		)
	}
}

func passwordChangeRepositoryFixture(
	t *testing.T,
	argContext context.Context,
) (
	*sql.DB,
	*sqlitestore.PasswordCredentialRepository,
	credential.PasswordCredential,
) {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")
	database, err := sqlitestore.Open(argContext, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(argContext, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	createdAt := time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC)
	user, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"password_change_user",
		"Password Change User",
		identity.RoleViewer,
		createdAt,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}
	storedCredential, err := credential.NewPasswordCredential(
		user.ID,
		"$argon2id$stored-hash",
		createdAt,
	)
	if err != nil {
		t.Fatalf("create password credential fixture: %v", err)
	}
	if err := sqlitestore.NewAdminBootstrapRepository(
		database,
	).BootstrapAdmin(argContext, user, storedCredential); err != nil {
		t.Fatalf("store password change fixture: %v", err)
	}

	sessionRepository := sqlitestore.NewSessionRepository(database)
	for _, tokenValue := range []string{"first-session", "second-session"} {
		storedSession, err := session.New(
			sha256.Sum256([]byte(tokenValue)),
			user.ID,
			createdAt,
			8*time.Hour,
		)
		if err != nil {
			t.Fatalf("create session fixture: %v", err)
		}
		if err := sessionRepository.Create(
			argContext,
			storedSession,
		); err != nil {
			t.Fatalf("store session fixture: %v", err)
		}
	}

	return database, sqlitestore.NewPasswordCredentialRepository(database), storedCredential
}

func TestPasswordCredentialRepositoryReturnsNotFoundForUserWithoutCredential(
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

	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	user, err := identity.NewUser(
		userID,
		"credentialless_user",
		"Credentialless User",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	if err := sqlitestore.NewUserRepository(database).Create(
		ctx,
		user,
	); err != nil {
		t.Fatalf("store user fixture: %v", err)
	}

	repository := sqlitestore.NewPasswordCredentialRepository(database)

	_, err = repository.FindByUserID(ctx, userID)
	if !errors.Is(
		err,
		credential.ErrPasswordCredentialNotFound,
	) {
		t.Fatalf(
			"expected ErrPasswordCredentialNotFound, got %v",
			err,
		)
	}
}
