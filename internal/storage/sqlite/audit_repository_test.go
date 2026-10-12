package sqlite_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	appauditing "github.com/ebe542/go-mediaarchive/internal/application/auditing"
	"github.com/ebe542/go-mediaarchive/internal/audit"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

const auditActorID = "523e4567-e89b-12d3-a456-426614174000"

func TestAuditRepositoryAppendsEventSnapshots(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	repository := sqlitestore.NewAuditRepository(database)
	event := auditRepositoryEvent()

	if err := repository.Append(ctx, event); err != nil {
		t.Fatalf("append audit event: %v", err)
	}

	var storedType string
	var storedOutcome string
	var storedActorID string
	var storedReason sql.NullString
	var rangeStart int64
	var rangeEnd int64
	var rangeTotal int64
	err := database.QueryRowContext(
		ctx,
		`SELECT event_type, outcome, actor_id, reason,
			range_start, range_end, range_total
		FROM audit_events WHERE id = ?`,
		event.ID,
	).Scan(
		&storedType,
		&storedOutcome,
		&storedActorID,
		&storedReason,
		&rangeStart,
		&rangeEnd,
		&rangeTotal,
	)
	if err != nil {
		t.Fatalf("read stored audit event: %v", err)
	}
	if storedType != string(event.Type) || storedOutcome != string(event.Outcome) ||
		storedActorID != event.ActorID || storedReason.Valid ||
		rangeStart != 100 || rangeEnd != 199 || rangeTotal != 1000 {
		t.Fatalf("unexpected stored audit event values")
	}
}

func TestAuditRepositoryRejectsInvalidAndDuplicateEvents(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	repository := sqlitestore.NewAuditRepository(database)
	event := auditRepositoryEvent()

	invalid := event
	invalid.Type = "unsafe.custom_event"
	if err := repository.Append(ctx, invalid); !errors.Is(err, audit.ErrInvalidType) {
		t.Fatalf("expected ErrInvalidType, got %v", err)
	}
	if err := repository.Append(ctx, event); err != nil {
		t.Fatalf("append fixture event: %v", err)
	}
	if err := repository.Append(ctx, event); !errors.Is(err, audit.ErrEventConflict) {
		t.Fatalf("expected ErrEventConflict, got %v", err)
	}
}

func TestAuditSchemaRejectsUpdatesAndDeletes(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	repository := sqlitestore.NewAuditRepository(database)
	event := auditRepositoryEvent()
	if err := repository.Append(ctx, event); err != nil {
		t.Fatalf("append audit fixture: %v", err)
	}

	if _, err := database.ExecContext(
		ctx,
		`UPDATE audit_events SET target_name = 'changed' WHERE id = ?`,
		event.ID,
	); err == nil {
		t.Fatal("expected audit event update to fail")
	}
	if _, err := database.ExecContext(
		ctx,
		`DELETE FROM audit_events WHERE id = ?`,
		event.ID,
	); err == nil {
		t.Fatal("expected audit event deletion to fail")
	}

	var count int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM audit_events WHERE id = ?`,
		event.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count immutable audit event: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected immutable audit event to remain, got %d", count)
	}
}

func TestAuditHistorySurvivesActorAndTargetDeletion(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, auditActorID, "audit_actor")
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertValidSchemaMedia(t, ctx, database)
	repository := sqlitestore.NewAuditRepository(database)
	event := auditRepositoryEvent()
	event.TargetID = schemaMediaID
	if err := repository.Append(ctx, event); err != nil {
		t.Fatalf("append audit fixture: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`DELETE FROM media_authors WHERE media_id = ?`,
		schemaMediaID,
	); err != nil {
		t.Fatalf("delete media author fixtures: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`DELETE FROM media_items WHERE id = ?`,
		schemaMediaID,
	); err != nil {
		t.Fatalf("delete media fixture: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`DELETE FROM users WHERE id = ?`,
		auditActorID,
	); err != nil {
		t.Fatalf("delete actor fixture: %v", err)
	}

	var username string
	var targetID string
	if err := database.QueryRowContext(
		ctx,
		`SELECT actor_username, target_id FROM audit_events WHERE id = ?`,
		event.ID,
	).Scan(&username, &targetID); err != nil {
		t.Fatalf("read retained audit snapshot: %v", err)
	}
	if username != event.ActorUsername {
		t.Fatalf("expected username snapshot %q, got %q", event.ActorUsername, username)
	}
	if targetID != event.TargetID {
		t.Fatalf("expected target snapshot %q, got %q", event.TargetID, targetID)
	}
}

func TestAuditRepositoryFiltersAndPaginatesEvents(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	repository := sqlitestore.NewAuditRepository(database)
	for index, fixture := range []struct {
		id      string
		hour    int
		outcome audit.Outcome
	}{
		{"a23e4567-e89b-12d3-a456-426614174000", 10, audit.OutcomeDenied},
		{"b23e4567-e89b-12d3-a456-426614174000", 11, audit.OutcomeSuccess},
		{"c23e4567-e89b-12d3-a456-426614174000", 12, audit.OutcomeDenied},
	} {
		event := auditRepositoryEvent()
		event.ID = fixture.id
		event.OccurredAt = time.Date(2026, time.October, 12, fixture.hour, 0, 0, index, time.UTC)
		event.Outcome = fixture.outcome
		event.Range = nil
		if fixture.outcome == audit.OutcomeDenied {
			event.Reason = audit.ReasonUnknownTarget
		}
		if err := repository.Append(ctx, event); err != nil {
			t.Fatalf("append audit page fixture: %v", err)
		}
	}

	firstPage, err := repository.ListAuditEvents(
		ctx,
		appauditing.Filter{Outcome: audit.OutcomeDenied},
		nil,
		1,
	)
	if err != nil {
		t.Fatalf("list first audit page: %v", err)
	}
	if len(firstPage) != 1 || firstPage[0].ID != "c23e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected first audit page: %+v", firstPage)
	}

	cursor := &appauditing.Cursor{
		OccurredAt: firstPage[0].OccurredAt,
		ID:         firstPage[0].ID,
	}
	secondPage, err := repository.ListAuditEvents(
		ctx,
		appauditing.Filter{Outcome: audit.OutcomeDenied},
		cursor,
		2,
	)
	if err != nil {
		t.Fatalf("list second audit page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != "a23e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected second audit page: %+v", secondPage)
	}
}

func auditRepositoryEvent() audit.Event {
	return audit.Event{
		ID:            "623e4567-e89b-12d3-a456-426614174000",
		OccurredAt:    time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC),
		Type:          audit.TypeMediaContentRead,
		Outcome:       audit.OutcomeSuccess,
		ActorID:       auditActorID,
		ActorUsername: "audit_actor",
		ActorRole:     "viewer",
		TargetType:    audit.TargetMedia,
		TargetID:      "723e4567-e89b-12d3-a456-426614174000",
		TargetName:    "Retained title",
		Range: &audit.ByteRange{
			Start: 100,
			End:   199,
			Total: 1000,
		},
	}
}
