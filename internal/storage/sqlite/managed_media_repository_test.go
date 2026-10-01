package sqlite_test

import (
	"errors"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/content"
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

	if err := repository.CreateManaged(ctx, item, location); err != nil {
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

func mediaRepositoryItemWithID(argTest *testing.T, argID string) media.Item {
	argTest.Helper()
	item := mediaRepositoryItem(argTest, nil)
	created, err := media.NewItem(
		argID,
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
		argTest.Fatalf("create media item with ID: %v", err)
	}

	return created
}
