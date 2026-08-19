package sqlite_test

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/session"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestSessionRepositoryCreatesAndFindsSession(t *testing.T) {
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
		"session_user",
		"Session User",
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

	expectedSession, err := session.New(
		sha256.Sum256([]byte("synthetic-session-token")),
		userID,
		now,
		8*time.Hour,
	)
	if err != nil {
		t.Fatalf("create session fixture: %v", err)
	}

	repository := sqlitestore.NewSessionRepository(database)

	if err := repository.Create(ctx, expectedSession); err != nil {
		t.Fatalf("store session: %v", err)
	}

	storedSession, err := repository.FindByTokenHash(
		ctx,
		expectedSession.TokenHash,
	)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}

	if storedSession != expectedSession {
		t.Fatalf(
			"expected session %#v, got %#v",
			expectedSession,
			storedSession,
		)
	}

	touchedAt := now.Add(10 * time.Minute)

	if err := repository.Touch(
		ctx,
		expectedSession.TokenHash,
		touchedAt,
	); err != nil {
		t.Fatalf("touch session: %v", err)
	}

	touchedSession, err := repository.FindByTokenHash(
		ctx,
		expectedSession.TokenHash,
	)
	if err != nil {
		t.Fatalf("find touched session: %v", err)
	}
	if touchedSession.LastSeenAt != touchedAt {
		t.Fatalf(
			"expected last-seen time %v, got %v",
			touchedAt,
			touchedSession.LastSeenAt,
		)
	}

	revokedAt := now.Add(15 * time.Minute)

	if err := repository.Revoke(
		ctx,
		expectedSession.TokenHash,
		revokedAt,
	); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	// Repeated logout must not replace the original revocation time.
	if err := repository.Revoke(
		ctx,
		expectedSession.TokenHash,
		revokedAt.Add(time.Minute),
	); err != nil {
		t.Fatalf("revoke session again: %v", err)
	}

	revokedSession, err := repository.FindByTokenHash(
		ctx,
		expectedSession.TokenHash,
	)
	if err != nil {
		t.Fatalf("find revoked session: %v", err)
	}
	if revokedSession.RevokedAt != revokedAt {
		t.Fatalf(
			"expected revocation time %v, got %v",
			revokedAt,
			revokedSession.RevokedAt,
		)
	}
}
