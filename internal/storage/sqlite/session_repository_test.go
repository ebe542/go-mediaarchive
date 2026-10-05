package sqlite_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
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

func TestSessionRepositoryCreatesSessionAndAuditAtomically(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "session_owner")
	repository := sqlitestore.NewSessionRepository(database)
	storedSession := auditedSessionFixture(t)
	event := sessionCreationEvent()

	if err := repository.CreateWithAudit(ctx, storedSession, event); err != nil {
		t.Fatalf("create audited session: %v", err)
	}
	if _, err := repository.FindByTokenHash(ctx, storedSession.TokenHash); err != nil {
		t.Fatalf("find audited session: %v", err)
	}
	var eventCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM audit_events WHERE id = ?`,
		event.ID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("count session audit event: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected one session audit event, got %d", eventCount)
	}
}

func TestSessionRepositoryRollsBackSessionAfterAuditFailure(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "session_owner")
	repository := sqlitestore.NewSessionRepository(database)
	storedSession := auditedSessionFixture(t)
	event := sessionCreationEvent()
	if err := sqlitestore.NewAuditRepository(database).Append(ctx, event); err != nil {
		t.Fatalf("append conflicting audit fixture: %v", err)
	}

	if err := repository.CreateWithAudit(ctx, storedSession, event); err == nil {
		t.Fatal("expected audited session creation to fail")
	}
	if _, err := repository.FindByTokenHash(
		ctx,
		storedSession.TokenHash,
	); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("expected session insert rollback, got %v", err)
	}
}

func TestSessionRepositoryRollsBackAuditAfterSessionFailure(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "session_owner")
	repository := sqlitestore.NewSessionRepository(database)
	storedSession := auditedSessionFixture(t)
	event := sessionCreationEvent()
	if err := repository.Create(ctx, storedSession); err != nil {
		t.Fatalf("create conflicting session fixture: %v", err)
	}

	if err := repository.CreateWithAudit(ctx, storedSession, event); err == nil {
		t.Fatal("expected audited session creation to fail")
	}
	var eventCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM audit_events WHERE id = ?`,
		event.ID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("count rolled-back audit event: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected no audit event after session failure, got %d", eventCount)
	}
}

func TestSessionRepositoryRevokesWithOneAuditEvent(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "session_owner")
	repository := sqlitestore.NewSessionRepository(database)
	storedSession := auditedSessionFixture(t)
	if err := repository.Create(ctx, storedSession); err != nil {
		t.Fatalf("create session fixture: %v", err)
	}
	event := sessionRevocationEvent()
	revokedAt := event.OccurredAt

	changed, err := repository.RevokeWithAudit(
		ctx,
		storedSession.TokenHash,
		revokedAt,
		event,
	)
	if err != nil {
		t.Fatalf("revoke audited session: %v", err)
	}
	if !changed {
		t.Fatal("expected first revocation to change the session")
	}
	changed, err = repository.RevokeWithAudit(
		ctx,
		storedSession.TokenHash,
		revokedAt.Add(time.Minute),
		sessionRevocationEvent(),
	)
	if err != nil {
		t.Fatalf("repeat audited session revocation: %v", err)
	}
	if changed {
		t.Fatal("expected repeated revocation to be a no-op")
	}

	stored, err := repository.FindByTokenHash(ctx, storedSession.TokenHash)
	if err != nil {
		t.Fatalf("find revoked session: %v", err)
	}
	if stored.RevokedAt != revokedAt {
		t.Fatalf("expected revocation time %v, got %v", revokedAt, stored.RevokedAt)
	}
	var eventCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM audit_events WHERE event_type = ?`,
		audit.TypeSessionRevoked,
	).Scan(&eventCount); err != nil {
		t.Fatalf("count session revocation events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected one session revocation event, got %d", eventCount)
	}
}

func TestSessionRepositoryRollsBackRevocationAfterAuditFailure(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "session_owner")
	repository := sqlitestore.NewSessionRepository(database)
	storedSession := auditedSessionFixture(t)
	if err := repository.Create(ctx, storedSession); err != nil {
		t.Fatalf("create session fixture: %v", err)
	}
	event := sessionRevocationEvent()
	if err := sqlitestore.NewAuditRepository(database).Append(ctx, event); err != nil {
		t.Fatalf("append conflicting audit fixture: %v", err)
	}

	if _, err := repository.RevokeWithAudit(
		ctx,
		storedSession.TokenHash,
		event.OccurredAt,
		event,
	); err == nil {
		t.Fatal("expected audited session revocation to fail")
	}
	stored, err := repository.FindByTokenHash(ctx, storedSession.TokenHash)
	if err != nil {
		t.Fatalf("find session after failed revocation: %v", err)
	}
	if !stored.RevokedAt.IsZero() {
		t.Fatalf("expected revocation rollback, got %v", stored.RevokedAt)
	}
}

func auditedSessionFixture(t *testing.T) session.Session {
	t.Helper()

	storedSession, err := session.New(
		sha256.Sum256([]byte("audited-session-token")),
		schemaOwnerID,
		time.Date(2026, time.October, 4, 12, 30, 0, 0, time.UTC),
		8*time.Hour,
	)
	if err != nil {
		t.Fatalf("create audited session fixture: %v", err)
	}

	return storedSession
}

func sessionCreationEvent() audit.Event {
	return audit.Event{
		ID:            "923e4567-e89b-12d3-a456-426614174000",
		OccurredAt:    time.Date(2026, time.October, 4, 12, 30, 0, 0, time.UTC),
		Type:          audit.TypeSessionCreated,
		Outcome:       audit.OutcomeSuccess,
		ActorID:       schemaOwnerID,
		ActorUsername: "session_owner",
		ActorRole:     "viewer",
		TargetType:    audit.TargetSession,
	}
}

func sessionRevocationEvent() audit.Event {
	event := sessionCreationEvent()
	event.ID = "a23e4567-e89b-12d3-a456-426614174000"
	event.OccurredAt = time.Date(2026, time.October, 5, 10, 0, 0, 0, time.UTC)
	event.Type = audit.TypeSessionRevoked

	return event
}
