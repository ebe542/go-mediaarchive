package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/session"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestContentAuditRepositoryAppendsMatchingEventOnce(t *testing.T) {
	ctx, database, sessionHash, actor := contentAuditFixture(t)
	repository := sqlitestore.NewContentAuditRepository(database)
	event := contentReadAuditEvent(t, actor, audit.OutcomeSuccess, "")

	created, err := repository.AppendOnce(
		ctx, sessionHash, schemaMediaID, event.Outcome, event.Reason,
		event.OccurredAt, event,
	)
	if err != nil || !created {
		t.Fatalf("append first content event: created=%t err=%v", created, err)
	}
	duplicateEvent := event
	duplicateEvent.ID = "d33e4567-e89b-12d3-a456-426614174000"
	duplicateEvent.OccurredAt = event.OccurredAt.Add(time.Minute)
	created, err = repository.AppendOnce(
		ctx, sessionHash, schemaMediaID,
		duplicateEvent.Outcome, duplicateEvent.Reason,
		duplicateEvent.OccurredAt, duplicateEvent,
	)
	if err != nil || created {
		t.Fatalf("append duplicate content event: created=%t err=%v", created, err)
	}

	var eventCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM audit_events WHERE event_type = ? AND actor_id = ?`,
		audit.TypeMediaContentRead,
		actor.ID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("count content audit events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected one content audit event, got %d", eventCount)
	}
}

func TestContentAuditRepositoryRollsBackMarkerOnAuditConflict(t *testing.T) {
	ctx, database, sessionHash, actor := contentAuditFixture(t)
	event := contentReadAuditEvent(t, actor, audit.OutcomeSuccess, "")
	if err := sqlitestore.NewAuditRepository(database).Append(ctx, event); err != nil {
		t.Fatalf("store conflicting audit event: %v", err)
	}

	created, err := sqlitestore.NewContentAuditRepository(database).AppendOnce(
		ctx, sessionHash, schemaMediaID, event.Outcome, event.Reason,
		event.OccurredAt, event,
	)
	if !errors.Is(err, audit.ErrEventConflict) || created {
		t.Fatalf("expected audit conflict rollback, created=%t err=%v", created, err)
	}
	var markerCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM content_audit_markers`,
	).Scan(&markerCount); err != nil {
		t.Fatalf("count content audit markers: %v", err)
	}
	if markerCount != 0 {
		t.Fatalf("expected marker rollback, got %d", markerCount)
	}
}

func TestContentAuditRepositoryRejectsMismatchedMarker(t *testing.T) {
	ctx, database, sessionHash, actor := contentAuditFixture(t)
	event := contentReadAuditEvent(t, actor, audit.OutcomeSuccess, "")

	created, err := sqlitestore.NewContentAuditRepository(database).AppendOnce(
		ctx, sessionHash, schemaMediaID, audit.OutcomeDenied,
		audit.ReasonUnknownTarget, event.OccurredAt, event,
	)
	if err == nil || created {
		t.Fatalf("expected mismatched marker rejection, created=%t err=%v", created, err)
	}

	var markerCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM content_audit_markers`,
	).Scan(&markerCount); err != nil {
		t.Fatalf("count content audit markers: %v", err)
	}
	if markerCount != 0 {
		t.Fatalf("expected no mismatched marker, got %d", markerCount)
	}
}

func contentAuditFixture(t *testing.T) (
	context.Context,
	*sql.DB,
	[sha256.Size]byte,
	identity.User,
) {
	t.Helper()

	ctx := context.Background()
	database, err := sqlitestore.Open(ctx, filepath.Join(t.TempDir(), "mediaarchive.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	now := time.Date(2026, time.October, 11, 10, 0, 0, 0, time.UTC)
	actor, err := identity.NewUser(
		schemaOwnerID, "content_reader", "Content Reader", identity.RoleViewer, now,
	)
	if err != nil {
		t.Fatalf("create content reader: %v", err)
	}
	if err := sqlitestore.NewUserRepository(database).Create(ctx, actor); err != nil {
		t.Fatalf("store content reader: %v", err)
	}
	sessionHash := session.HashToken("content-audit-session")
	storedSession, err := session.New(sessionHash, actor.ID, now, time.Hour)
	if err != nil {
		t.Fatalf("create content session: %v", err)
	}
	if err := sqlitestore.NewSessionRepository(database).Create(ctx, storedSession); err != nil {
		t.Fatalf("store content session: %v", err)
	}

	return ctx, database, sessionHash, actor
}

func contentReadAuditEvent(
	t *testing.T,
	actor identity.User,
	outcome audit.Outcome,
	reason audit.Reason,
) audit.Event {
	t.Helper()

	event, err := audit.NewEvent(audit.Event{
		ID:            "d23e4567-e89b-12d3-a456-426614174000",
		OccurredAt:    time.Date(2026, time.October, 11, 10, 1, 0, 0, time.UTC),
		Type:          audit.TypeMediaContentRead,
		Outcome:       outcome,
		ActorID:       actor.ID,
		ActorUsername: actor.Username,
		ActorRole:     string(actor.Role),
		TargetType:    audit.TargetMedia,
		TargetID:      schemaMediaID,
		TargetName:    "Security Book",
		Reason:        reason,
	})
	if err != nil {
		t.Fatalf("create content audit event: %v", err)
	}

	return event
}
