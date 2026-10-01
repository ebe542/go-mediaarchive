package sqlite_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/media"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestContentLocationRepositoryLifecycle(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	item := mediaRepositoryItem(t, nil)
	if err := sqlitestore.NewMediaRepository(database).Create(ctx, item); err != nil {
		t.Fatalf("create media fixture: %v", err)
	}

	repository := sqlitestore.NewContentLocationRepository(database)
	storedAt := time.Date(2026, 9, 20, 14, 30, 0, 123, time.UTC)
	location, err := content.NewLocation(item.ID, "32/"+item.ID, storedAt)
	if err != nil {
		t.Fatalf("create location fixture: %v", err)
	}
	if err := repository.Create(ctx, location); err != nil {
		t.Fatalf("create content location: %v", err)
	}

	stored, err := repository.FindByMediaID(ctx, item.ID)
	if err != nil {
		t.Fatalf("find content location: %v", err)
	}
	if stored != location {
		t.Fatalf("expected %#v, got %#v", location, stored)
	}

	if err := repository.Delete(ctx, item.ID); err != nil {
		t.Fatalf("delete content location: %v", err)
	}
	if _, err := repository.FindByMediaID(ctx, item.ID); !errors.Is(err, content.ErrLocationNotFound) {
		t.Fatalf("expected ErrLocationNotFound, got %v", err)
	}
	if err := repository.Delete(ctx, item.ID); !errors.Is(err, content.ErrLocationNotFound) {
		t.Fatalf("expected missing delete error, got %v", err)
	}
}

func TestContentLocationRepositoryReportsConflicts(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	mediaRepository := sqlitestore.NewMediaRepository(database)
	first := mediaRepositoryItem(t, nil)
	if err := mediaRepository.Create(ctx, first); err != nil {
		t.Fatalf("create first media fixture: %v", err)
	}
	second, err := media.NewItem(
		"423e4567-e89b-12d3-a456-426614174000",
		first.Title,
		first.Authors,
		first.OriginalFilename,
		first.Type,
		first.MIMEType,
		first.Size,
		first.Checksum[:],
		first.OwnerID,
		first.CreatedAt,
		first.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("create second media value: %v", err)
	}
	if err := mediaRepository.Create(ctx, second); err != nil {
		t.Fatalf("create second media fixture: %v", err)
	}

	repository := sqlitestore.NewContentLocationRepository(database)
	storedAt := time.Now().UTC()
	firstLocation, err := content.NewLocation(first.ID, "32/"+first.ID, storedAt)
	if err != nil {
		t.Fatalf("create first location: %v", err)
	}
	if err := repository.Create(ctx, firstLocation); err != nil {
		t.Fatalf("persist first location: %v", err)
	}
	if err := repository.Create(ctx, firstLocation); !errors.Is(err, content.ErrLocationConflict) {
		t.Fatalf("expected media conflict, got %v", err)
	}
	duplicateKey, err := content.NewLocation(second.ID, firstLocation.StorageKey, storedAt)
	if err != nil {
		t.Fatalf("create duplicate-key location: %v", err)
	}
	if err := repository.Create(ctx, duplicateKey); !errors.Is(err, content.ErrLocationConflict) {
		t.Fatalf("expected storage-key conflict, got %v", err)
	}
}

func TestContentLocationRepositoryRequiresExistingMedia(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	repository := sqlitestore.NewContentLocationRepository(database)
	location, err := content.NewLocation(
		schemaMediaID,
		"32/"+schemaMediaID,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("create location fixture: %v", err)
	}

	if err := repository.Create(ctx, location); err == nil {
		t.Fatal("expected unknown media to be rejected")
	}
}
