package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/media"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestMediaRepositoryCreatesManagedMediaAtomically(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	item := mediaRepositoryItem(t, []string{"First Author", "Second Author"})
	location, err := content.NewLocation(item.ID, "32/"+item.ID, item.CreatedAt)
	if err != nil {
		t.Fatalf("create content location: %v", err)
	}
	repository := sqlitestore.NewMediaRepository(database)

	event := managedUploadAuditEvent(t, item)
	if err := repository.CreateManagedWithAudit(
		ctx,
		item,
		location,
		event,
	); err != nil {
		t.Fatalf("create managed media: %v", err)
	}
	storedItem, err := repository.FindByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("find managed media: %v", err)
	}
	assertMediaItem(t, storedItem, item)
	storedLocation, err := sqlitestore.NewContentLocationRepository(database).FindByMediaID(ctx, item.ID)
	if err != nil {
		t.Fatalf("find managed location: %v", err)
	}
	if storedLocation != location {
		t.Fatalf("expected location %+v, got %+v", location, storedLocation)
	}
	var eventCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM audit_events WHERE id = ? AND event_type = ?`,
		event.ID,
		audit.TypeMediaUploaded,
	).Scan(&eventCount); err != nil {
		t.Fatalf("count upload audit events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected one upload audit event, got %d", eventCount)
	}
}

func TestMediaRepositoryRollsBackManagedMediaOnAuditConflict(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	item := mediaRepositoryItem(t, []string{"First Author"})
	location, err := content.NewLocation(item.ID, "32/"+item.ID, item.CreatedAt)
	if err != nil {
		t.Fatalf("create content location: %v", err)
	}
	event := managedUploadAuditEvent(t, item)
	if err := sqlitestore.NewAuditRepository(database).Append(ctx, event); err != nil {
		t.Fatalf("store conflicting audit event: %v", err)
	}

	err = sqlitestore.NewMediaRepository(database).CreateManagedWithAudit(
		ctx,
		item,
		location,
		event,
	)
	if !errors.Is(err, audit.ErrEventConflict) {
		t.Fatalf("expected ErrEventConflict, got %v", err)
	}
	assertNoManagedMediaRows(t, ctx, database, item.ID)
}

func TestMediaRepositoryRollsBackManagedMediaOnLocationConflict(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	repository := sqlitestore.NewMediaRepository(database)
	first := mediaRepositoryItem(t, nil)
	firstLocation, err := content.NewLocation(first.ID, "shared/content", first.CreatedAt)
	if err != nil {
		t.Fatalf("create first location: %v", err)
	}
	if err := repository.CreateManaged(ctx, first, firstLocation); err != nil {
		t.Fatalf("create first managed media: %v", err)
	}

	second := mediaRepositoryItemWithID(t, "423e4567-e89b-12d3-a456-426614174000")
	secondLocation, err := content.NewLocation(second.ID, firstLocation.StorageKey, second.CreatedAt)
	if err != nil {
		t.Fatalf("create conflicting location: %v", err)
	}
	if err := repository.CreateManaged(ctx, second, secondLocation); !errors.Is(err, content.ErrLocationConflict) {
		t.Fatalf("expected ErrLocationConflict, got %v", err)
	}
	if _, err := repository.FindByID(ctx, second.ID); !errors.Is(err, media.ErrItemNotFound) {
		t.Fatalf("expected media rollback, got %v", err)
	}
}

func TestMediaRepositoryRejectsMismatchedManagedLocation(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	item := mediaRepositoryItem(t, nil)
	location, err := content.NewLocation(
		"423e4567-e89b-12d3-a456-426614174000",
		"42/content",
		item.CreatedAt,
	)
	if err != nil {
		t.Fatalf("create mismatched location: %v", err)
	}

	err = sqlitestore.NewMediaRepository(database).CreateManaged(ctx, item, location)
	if !errors.Is(err, content.ErrLocationMediaMismatch) {
		t.Fatalf("expected ErrLocationMediaMismatch, got %v", err)
	}
}

func TestMediaRepositoryDeletesManagedMediaAtomically(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertMediaSchemaUser(t, ctx, database, schemaGranteeID, "media_grantee")
	repository := sqlitestore.NewMediaRepository(database)
	item := mediaRepositoryItem(t, []string{"Author"})
	location, err := content.NewLocation(item.ID, "32/"+item.ID, item.CreatedAt)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := repository.CreateManaged(ctx, item, location); err != nil {
		t.Fatalf("create managed media: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_grants (media_id, user_id, permissions) VALUES (?, ?, 1)`,
		item.ID,
		schemaGranteeID,
	); err != nil {
		t.Fatalf("create grant fixture: %v", err)
	}

	if err := repository.DeleteManaged(ctx, item.ID, location.StorageKey); err != nil {
		t.Fatalf("delete managed media: %v", err)
	}
	for _, table := range []string{"media_contents", "media_grants", "media_authors", "media_items"} {
		var count int
		query := "SELECT COUNT(*) FROM " + table + " WHERE media_id = ?"
		if table == "media_items" {
			query = "SELECT COUNT(*) FROM media_items WHERE id = ?"
		}
		if err := database.QueryRowContext(ctx, query, item.ID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected no %s rows, got %d", table, count)
		}
	}
}

func TestMediaRepositoryPreservesManagedMediaForWrongKey(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	repository := sqlitestore.NewMediaRepository(database)
	item := mediaRepositoryItem(t, nil)
	location, err := content.NewLocation(item.ID, "32/"+item.ID, item.CreatedAt)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := repository.CreateManaged(ctx, item, location); err != nil {
		t.Fatalf("create managed media: %v", err)
	}

	err = repository.DeleteManaged(ctx, item.ID, "wrong/key")
	if !errors.Is(err, content.ErrLocationNotFound) {
		t.Fatalf("expected ErrLocationNotFound, got %v", err)
	}
	if _, err := repository.FindByID(ctx, item.ID); err != nil {
		t.Fatalf("expected media rollback: %v", err)
	}
}

func mediaRepositoryItemWithID(test *testing.T, id string) media.Item {
	test.Helper()
	item := mediaRepositoryItem(test, nil)
	created, err := media.NewItem(
		id,
		item.Title,
		item.Authors,
		item.OriginalFilename,
		item.Type,
		item.MIMEType,
		item.Size,
		item.Checksum[:],
		item.OwnerID,
		item.CreatedAt,
		item.UpdatedAt,
	)
	if err != nil {
		test.Fatalf("create media item with ID: %v", err)
	}

	return created
}

func managedUploadAuditEvent(t *testing.T, item media.Item) audit.Event {
	t.Helper()

	event, err := audit.NewEvent(audit.Event{
		ID:            "923e4567-e89b-12d3-a456-426614174000",
		OccurredAt:    item.CreatedAt,
		Type:          audit.TypeMediaUploaded,
		Outcome:       audit.OutcomeSuccess,
		ActorID:       item.OwnerID,
		ActorUsername: "media_owner",
		ActorRole:     string(identity.RoleEditor),
		TargetType:    audit.TargetMedia,
		TargetID:      item.ID,
		TargetName:    item.Title,
	})
	if err != nil {
		t.Fatalf("create managed upload audit event: %v", err)
	}

	return event
}

func assertNoManagedMediaRows(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	mediaID string,
) {
	t.Helper()

	for _, table := range []string{"media_contents", "media_authors", "media_items"} {
		var count int
		query := "SELECT COUNT(*) FROM " + table + " WHERE media_id = ?"
		if table == "media_items" {
			query = "SELECT COUNT(*) FROM media_items WHERE id = ?"
		}
		if err := database.QueryRowContext(ctx, query, mediaID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected no %s rows, got %d", table, count)
		}
	}
}
