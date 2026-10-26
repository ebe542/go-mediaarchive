package sqlite_test

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestMediaRepositoryListsOnlyDiscoverableMedia(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertMediaSchemaUser(t, ctx, database, schemaGranteeID, "media_reader")
	repository := sqlitestore.NewMediaRepository(database)
	baseTime := time.Date(2026, time.October, 17, 10, 0, 0, 0, time.UTC)
	granted := directoryRepositoryItem(
		t,
		"423e4567-e89b-12d3-a456-426614174000",
		"Applied Cryptography",
		[]string{"Alice Security"},
		domainmedia.TypeBook,
		schemaOwnerID,
		baseTime,
	)
	hidden := directoryRepositoryItem(
		t,
		"523e4567-e89b-12d3-a456-426614174000",
		"Private Notes",
		[]string{"Hidden Author"},
		domainmedia.TypeDocument,
		schemaOwnerID,
		baseTime.Add(-time.Hour),
	)
	owned := directoryRepositoryItem(
		t,
		"623e4567-e89b-12d3-a456-426614174000",
		"Owned Video",
		[]string{"Media Reader"},
		domainmedia.TypeVideo,
		schemaGranteeID,
		baseTime.Add(-2*time.Hour),
	)
	for _, item := range []domainmedia.Item{granted, hidden, owned} {
		if err := repository.Create(ctx, item); err != nil {
			t.Fatalf("create directory fixture %q: %v", item.Title, err)
		}
	}
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_grants (media_id, user_id, permissions) VALUES (?, ?, ?)`,
		granted.ID,
		schemaGranteeID,
		int(domainmedia.PermissionDiscover),
	); err != nil {
		t.Fatalf("grant discover permission: %v", err)
	}

	items, err := repository.ListDiscoverableMedia(
		ctx,
		schemaGranteeID,
		appmedia.DirectoryFilter{},
		nil,
		10,
	)
	if err != nil {
		t.Fatalf("list discoverable media: %v", err)
	}
	if len(items) != 2 || items[0].ID != granted.ID || items[1].ID != owned.ID {
		t.Fatalf("unexpected discoverable media: %+v", items)
	}
}

func TestMediaRepositoryFiltersTitleAuthorTypeAndLiteralWildcards(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	repository := sqlitestore.NewMediaRepository(database)
	baseTime := time.Date(2026, time.October, 17, 10, 0, 0, 0, time.UTC)
	percentTitle := directoryRepositoryItem(
		t,
		"423e4567-e89b-12d3-a456-426614174000",
		"100% Go",
		[]string{"First Author"},
		domainmedia.TypeBook,
		schemaOwnerID,
		baseTime,
	)
	authorMatch := directoryRepositoryItem(
		t,
		"523e4567-e89b-12d3-a456-426614174000",
		"Security Manual",
		[]string{"Go Research Group"},
		domainmedia.TypeDocument,
		schemaOwnerID,
		baseTime.Add(-time.Hour),
	)
	for _, item := range []domainmedia.Item{percentTitle, authorMatch} {
		if err := repository.Create(ctx, item); err != nil {
			t.Fatalf("create filter fixture: %v", err)
		}
	}

	items, err := repository.ListDiscoverableMedia(
		ctx,
		schemaOwnerID,
		appmedia.DirectoryFilter{Query: "%", Type: domainmedia.TypeBook},
		nil,
		10,
	)
	if err != nil {
		t.Fatalf("filter literal wildcard: %v", err)
	}
	if len(items) != 1 || items[0].ID != percentTitle.ID {
		t.Fatalf("unexpected literal wildcard result: %+v", items)
	}

	items, err = repository.ListDiscoverableMedia(
		ctx,
		schemaOwnerID,
		appmedia.DirectoryFilter{Query: "research"},
		nil,
		10,
	)
	if err != nil {
		t.Fatalf("filter author: %v", err)
	}
	if len(items) != 1 || items[0].ID != authorMatch.ID {
		t.Fatalf("unexpected author result: %+v", items)
	}
}

func TestMediaRepositoryUsesDescendingImmutableCursor(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	repository := sqlitestore.NewMediaRepository(database)
	baseTime := time.Date(2026, time.October, 17, 10, 0, 0, 0, time.UTC)
	first := directoryRepositoryItem(
		t, "423e4567-e89b-12d3-a456-426614174000", "First", nil,
		domainmedia.TypeBook, schemaOwnerID, baseTime,
	)
	second := directoryRepositoryItem(
		t, "523e4567-e89b-12d3-a456-426614174000", "Second", nil,
		domainmedia.TypeBook, schemaOwnerID, baseTime.Add(-time.Hour),
	)
	for _, item := range []domainmedia.Item{first, second} {
		if err := repository.Create(ctx, item); err != nil {
			t.Fatalf("create cursor fixture: %v", err)
		}
	}
	cursor, err := appmedia.NewDirectoryCursor(
		first.CreatedAt,
		first.ID,
		[32]byte{1},
		10,
	)
	if err != nil {
		t.Fatalf("create directory cursor: %v", err)
	}

	items, err := repository.ListDiscoverableMedia(
		ctx,
		schemaOwnerID,
		appmedia.DirectoryFilter{},
		&cursor,
		10,
	)
	if err != nil {
		t.Fatalf("list next media page: %v", err)
	}
	if len(items) != 1 || items[0].ID != second.ID {
		t.Fatalf("unexpected next media page: %+v", items)
	}
}

func directoryRepositoryItem(
	t *testing.T,
	id string,
	title string,
	authors []string,
	mediaType domainmedia.Type,
	ownerID string,
	createdAt time.Time,
) domainmedia.Item {
	t.Helper()
	item, err := domainmedia.NewItem(
		id,
		title,
		authors,
		filenameForMediaType(mediaType),
		mediaType,
		mimeTypeForMediaType(mediaType),
		4096,
		bytes.Repeat([]byte{0x7a}, sha256.Size),
		ownerID,
		createdAt,
		createdAt,
	)
	if err != nil {
		t.Fatalf("create directory item: %v", err)
	}

	return item
}

func filenameForMediaType(mediaType domainmedia.Type) string {
	return "archive." + map[domainmedia.Type]string{
		domainmedia.TypeBook:     "pdf",
		domainmedia.TypeDocument: "txt",
		domainmedia.TypeVideo:    "mp4",
	}[mediaType]
}

func mimeTypeForMediaType(mediaType domainmedia.Type) string {
	return map[domainmedia.Type]string{
		domainmedia.TypeBook:     "application/pdf",
		domainmedia.TypeDocument: "text/plain",
		domainmedia.TypeVideo:    "video/mp4",
	}[mediaType]
}
