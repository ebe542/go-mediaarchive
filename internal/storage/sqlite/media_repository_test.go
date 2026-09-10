package sqlite_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/media"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestMediaRepositoryCreatesAndFindsOrderedAuthors(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	repository := sqlitestore.NewMediaRepository(database)
	item := mediaRepositoryItem(t, []string{"First Author", "Second Author"})

	if err := repository.Create(ctx, item); err != nil {
		t.Fatalf("create media: %v", err)
	}
	stored, err := repository.FindByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("find media: %v", err)
	}
	assertMediaItem(t, stored, item)
}

func TestMediaRepositorySupportsItemWithoutAuthors(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	repository := sqlitestore.NewMediaRepository(database)
	item := mediaRepositoryItem(t, nil)

	if err := repository.Create(ctx, item); err != nil {
		t.Fatalf("create media without authors: %v", err)
	}
	stored, err := repository.FindByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("find media without authors: %v", err)
	}
	if stored.Authors == nil || len(stored.Authors) != 0 {
		t.Fatalf("expected non-nil empty authors, got %#v", stored.Authors)
	}
}

func TestMediaRepositoryReportsConflictAndMissingItems(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	repository := sqlitestore.NewMediaRepository(database)
	item := mediaRepositoryItem(t, nil)
	if err := repository.Create(ctx, item); err != nil {
		t.Fatalf("create media fixture: %v", err)
	}
	if err := repository.Create(ctx, item); !errors.Is(err, media.ErrItemConflict) {
		t.Fatalf("expected ErrItemConflict, got %v", err)
	}

	const missingID = "423e4567-e89b-12d3-a456-426614174000"
	if _, err := repository.FindByID(ctx, missingID); !errors.Is(err, media.ErrItemNotFound) {
		t.Fatalf("expected missing find error, got %v", err)
	}
	missing := item
	missing.ID = missingID
	if err := repository.Update(ctx, missing); !errors.Is(err, media.ErrItemNotFound) {
		t.Fatalf("expected missing update error, got %v", err)
	}
	if err := repository.Delete(ctx, missingID); !errors.Is(err, media.ErrItemNotFound) {
		t.Fatalf("expected missing delete error, got %v", err)
	}
}

func TestMediaRepositoryUpdatesMutableFieldsAndPreservesOwnership(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	repository := sqlitestore.NewMediaRepository(database)
	original := mediaRepositoryItem(t, []string{"Original Author"})
	if err := repository.Create(ctx, original); err != nil {
		t.Fatalf("create media fixture: %v", err)
	}

	updated, err := media.NewItem(
		original.ID,
		"Updated Security Video",
		[]string{"Replacement Author", "Second Author"},
		"updated-security.mp4",
		media.TypeVideo,
		"video/mp4",
		8192,
		bytes.Repeat([]byte{0x7c}, sha256.Size),
		schemaGranteeID,
		original.CreatedAt.Add(-time.Hour),
		original.UpdatedAt.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("create updated media value: %v", err)
	}
	if err := repository.Update(ctx, updated); err != nil {
		t.Fatalf("update media: %v", err)
	}

	stored, err := repository.FindByID(ctx, original.ID)
	if err != nil {
		t.Fatalf("find updated media: %v", err)
	}
	if stored.Title != updated.Title ||
		stored.Type != updated.Type ||
		stored.OriginalFilename != updated.OriginalFilename ||
		stored.MIMEType != updated.MIMEType ||
		stored.Size != updated.Size ||
		stored.Checksum != updated.Checksum {
		t.Errorf("expected mutable fields to be replaced, got %+v", stored)
	}
	if stored.OwnerID != original.OwnerID || !stored.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("expected ownership and creation time to be preserved, got %+v", stored)
	}
	if len(stored.Authors) != 2 || stored.Authors[0] != "Replacement Author" {
		t.Errorf("expected replacement authors in order, got %v", stored.Authors)
	}
}

func TestMediaRepositoryRollsBackFailedAuthorCreation(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	if _, err := database.ExecContext(
		ctx,
		`
			CREATE TRIGGER reject_second_author
			BEFORE INSERT ON media_authors
			WHEN NEW.position = 1
			BEGIN
				SELECT RAISE(ABORT, 'synthetic author failure');
			END
		`,
	); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	repository := sqlitestore.NewMediaRepository(database)
	item := mediaRepositoryItem(t, []string{"First Author", "Second Author"})
	if err := repository.Create(ctx, item); err == nil {
		t.Fatal("expected media creation to fail")
	}
	if _, err := repository.FindByID(ctx, item.ID); !errors.Is(err, media.ErrItemNotFound) {
		t.Fatalf("expected media insert rollback, got %v", err)
	}
}

func TestMediaRepositoryRollsBackFailedAuthorUpdate(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	repository := sqlitestore.NewMediaRepository(database)
	original := mediaRepositoryItem(t, []string{"Original Author"})
	if err := repository.Create(ctx, original); err != nil {
		t.Fatalf("create media fixture: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`
			CREATE TRIGGER reject_second_update_author
			BEFORE INSERT ON media_authors
			WHEN NEW.position = 1
			BEGIN
				SELECT RAISE(ABORT, 'synthetic author failure');
			END
		`,
	); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	updated := original
	updated.Title = "Changed Title"
	updated.Authors = []string{"Replacement Author", "Rejected Author"}
	updated.UpdatedAt = original.UpdatedAt.Add(time.Hour)
	if err := repository.Update(ctx, updated); err == nil {
		t.Fatal("expected media update to fail")
	}
	stored, err := repository.FindByID(ctx, original.ID)
	if err != nil {
		t.Fatalf("find media after failed update: %v", err)
	}
	assertMediaItem(t, stored, original)
}

func TestMediaRepositoryDeletesGrantsAuthorsAndItem(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertMediaSchemaUser(t, ctx, database, schemaGranteeID, "media_grantee")
	repository := sqlitestore.NewMediaRepository(database)
	item := mediaRepositoryItem(t, []string{"Archive Author"})
	if err := repository.Create(ctx, item); err != nil {
		t.Fatalf("create media fixture: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_grants (media_id, user_id, permissions) VALUES (?, ?, ?)`,
		item.ID,
		schemaGranteeID,
		3,
	); err != nil {
		t.Fatalf("insert media grant: %v", err)
	}

	if err := repository.Delete(ctx, item.ID); err != nil {
		t.Fatalf("delete media: %v", err)
	}
	for _, table := range []string{"media_grants", "media_authors", "media_items"} {
		var count int
		query := "SELECT COUNT(*) FROM " + table + " WHERE media_id = ?"
		if table == "media_items" {
			query = "SELECT COUNT(*) FROM media_items WHERE id = ?"
		}
		if err := database.QueryRowContext(ctx, query, item.ID).Scan(&count); err != nil {
			t.Fatalf("count %s records: %v", table, err)
		}
		if count != 0 {
			t.Errorf("expected no %s records, got %d", table, count)
		}
	}
}

func mediaRepositoryItem(argTest *testing.T, argAuthors []string) media.Item {
	argTest.Helper()

	createdAt := time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)
	item, err := media.NewItem(
		schemaMediaID,
		"Security Book",
		argAuthors,
		"security-book.pdf",
		media.TypeBook,
		"application/pdf",
		4096,
		bytes.Repeat([]byte{0x5a}, sha256.Size),
		schemaOwnerID,
		createdAt,
		createdAt,
	)
	if err != nil {
		argTest.Fatalf("create media fixture: %v", err)
	}

	return item
}

func assertMediaItem(
	argTest *testing.T,
	argActual media.Item,
	argExpected media.Item,
) {
	argTest.Helper()

	if argActual.ID != argExpected.ID ||
		argActual.Title != argExpected.Title ||
		argActual.OriginalFilename != argExpected.OriginalFilename ||
		argActual.Type != argExpected.Type ||
		argActual.MIMEType != argExpected.MIMEType ||
		argActual.Size != argExpected.Size ||
		argActual.Checksum != argExpected.Checksum ||
		argActual.OwnerID != argExpected.OwnerID ||
		!argActual.CreatedAt.Equal(argExpected.CreatedAt) ||
		!argActual.UpdatedAt.Equal(argExpected.UpdatedAt) {
		argTest.Errorf("expected media %+v, got %+v", argExpected, argActual)
	}
	if len(argActual.Authors) != len(argExpected.Authors) {
		argTest.Fatalf("expected authors %v, got %v", argExpected.Authors, argActual.Authors)
	}
	for index := range argExpected.Authors {
		if argActual.Authors[index] != argExpected.Authors[index] {
			argTest.Errorf("expected authors %v, got %v", argExpected.Authors, argActual.Authors)
		}
	}
}
