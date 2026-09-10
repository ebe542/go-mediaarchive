package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"path/filepath"
	"testing"

	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

const (
	schemaOwnerID   = "123e4567-e89b-12d3-a456-426614174000"
	schemaGranteeID = "223e4567-e89b-12d3-a456-426614174000"
	schemaMediaID   = "323e4567-e89b-12d3-a456-426614174000"
	schemaTimestamp = "2026-09-10T10:00:00Z"
)

func TestMediaMigrationCreatesExpectedTablesAndIndexes(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)

	for _, object := range []struct {
		kind string
		name string
	}{
		{"table", "media_items"},
		{"table", "media_authors"},
		{"table", "media_grants"},
		{"index", "media_items_owner_id_index"},
		{"index", "media_grants_user_id_index"},
	} {
		var count int
		if err := database.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`,
			object.kind,
			object.name,
		).Scan(&count); err != nil {
			t.Fatalf("find %s %s: %v", object.kind, object.name, err)
		}
		if count != 1 {
			t.Errorf("expected one %s named %s, got %d", object.kind, object.name, count)
		}
	}
}

func TestMediaSchemaAcceptsValidRelatedRecords(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertMediaSchemaUser(t, ctx, database, schemaGranteeID, "media_grantee")
	insertValidSchemaMedia(t, ctx, database)

	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_authors (media_id, position, name) VALUES (?, ?, ?), (?, ?, ?)`,
		schemaMediaID,
		0,
		"First Author",
		schemaMediaID,
		1,
		"Second Author",
	); err != nil {
		t.Fatalf("insert ordered authors: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_grants (media_id, user_id, permissions) VALUES (?, ?, ?)`,
		schemaMediaID,
		schemaGranteeID,
		7,
	); err != nil {
		t.Fatalf("insert combined media grant: %v", err)
	}
}

func TestMediaSchemaRejectsInvalidMediaMetadata(t *testing.T) {
	tests := map[string]struct {
		column string
		value  any
	}{
		"empty title":         {"title", ""},
		"unsafe Unix path":    {"original_filename", "private/book.pdf"},
		"unsafe Windows path": {"original_filename", `private\book.pdf`},
		"drive designator":    {"original_filename", "C:book.pdf"},
		"unknown media type":  {"media_type", "audio"},
		"malformed MIME type": {"mime_type", "application"},
		"zero size":           {"size", 0},
		"short checksum":      {"checksum", make([]byte, sha256.Size-1)},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, database := openMediaSchemaDatabase(t)
			insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
			insertValidSchemaMedia(t, ctx, database)

			query := "UPDATE media_items SET " + testCase.column + " = ? WHERE id = ?"
			if _, err := database.ExecContext(
				ctx,
				query,
				testCase.value,
				schemaMediaID,
			); err == nil {
				t.Fatal("expected invalid media metadata to be rejected")
			}
		})
	}
}

func TestMediaSchemaRejectsInvalidPermissionMasks(t *testing.T) {
	for name, permissions := range map[string]int{
		"empty":       0,
		"unknown bit": 64,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, database := openMediaSchemaDatabase(t)
			insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
			insertMediaSchemaUser(t, ctx, database, schemaGranteeID, "media_grantee")
			insertValidSchemaMedia(t, ctx, database)

			if _, err := database.ExecContext(
				ctx,
				`INSERT INTO media_grants (media_id, user_id, permissions) VALUES (?, ?, ?)`,
				schemaMediaID,
				schemaGranteeID,
				permissions,
			); err == nil {
				t.Fatal("expected invalid permission mask to be rejected")
			}
		})
	}
}

func TestMediaSchemaRejectsDuplicateAuthors(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertValidSchemaMedia(t, ctx, database)

	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_authors (media_id, position, name) VALUES (?, 0, ?)`,
		schemaMediaID,
		"Repeated Author",
	); err != nil {
		t.Fatalf("insert author fixture: %v", err)
	}

	for name, arguments := range map[string][]any{
		"position": {schemaMediaID, 0, "Different Author"},
		"name":     {schemaMediaID, 1, "Repeated Author"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := database.ExecContext(
				ctx,
				`INSERT INTO media_authors (media_id, position, name) VALUES (?, ?, ?)`,
				arguments...,
			); err == nil {
				t.Fatalf("expected duplicate author %s to be rejected", name)
			}
		})
	}
}

func TestMediaSchemaEnforcesUserReferences(t *testing.T) {
	t.Run("unknown owner", func(t *testing.T) {
		ctx, database := openMediaSchemaDatabase(t)

		if _, err := database.ExecContext(
			ctx,
			validMediaInsertSQL,
			schemaMediaID,
			"Security Book",
			"security-book.pdf",
			"book",
			"application/pdf",
			1024,
			make([]byte, sha256.Size),
			schemaOwnerID,
			schemaTimestamp,
			schemaTimestamp,
		); err == nil {
			t.Fatal("expected unknown media owner to be rejected")
		}
	})

	t.Run("unknown grantee", func(t *testing.T) {
		ctx, database := openMediaSchemaDatabase(t)
		insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
		insertValidSchemaMedia(t, ctx, database)

		if _, err := database.ExecContext(
			ctx,
			`INSERT INTO media_grants (media_id, user_id, permissions) VALUES (?, ?, ?)`,
			schemaMediaID,
			schemaGranteeID,
			1,
		); err == nil {
			t.Fatal("expected unknown media grantee to be rejected")
		}
	})
}

func TestMediaSchemaRestrictsDeletionOfReferencedRecords(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertMediaSchemaUser(t, ctx, database, schemaGranteeID, "media_grantee")
	insertValidSchemaMedia(t, ctx, database)
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_grants (media_id, user_id, permissions) VALUES (?, ?, ?)`,
		schemaMediaID,
		schemaGranteeID,
		1,
	); err != nil {
		t.Fatalf("insert grant fixture: %v", err)
	}

	for name, userID := range map[string]string{
		"owner":   schemaOwnerID,
		"grantee": schemaGranteeID,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := database.ExecContext(
				ctx,
				`DELETE FROM users WHERE id = ?`,
				userID,
			); err == nil {
				t.Fatalf("expected referenced %s deletion to be restricted", name)
			}
		})
	}
}

const validMediaInsertSQL = `
	INSERT INTO media_items (
		id,
		title,
		original_filename,
		media_type,
		mime_type,
		size,
		checksum,
		owner_id,
		created_at,
		updated_at
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

func openMediaSchemaDatabase(argTest *testing.T) (context.Context, *sql.DB) {
	argTest.Helper()

	ctx := context.Background()
	database, err := sqlitestore.Open(
		ctx,
		filepath.Join(argTest.TempDir(), "mediaarchive.db"),
	)
	if err != nil {
		argTest.Fatalf("open SQLite database: %v", err)
	}
	argTest.Cleanup(func() {
		if closeErr := database.Close(); closeErr != nil {
			argTest.Errorf("close SQLite database: %v", closeErr)
		}
	})
	if err := sqlitestore.Migrate(ctx, database); err != nil {
		argTest.Fatalf("apply migrations: %v", err)
	}

	return ctx, database
}

func insertMediaSchemaUser(
	argTest *testing.T,
	argContext context.Context,
	argDatabase *sql.DB,
	argID string,
	argUsername string,
) {
	argTest.Helper()

	if _, err := argDatabase.ExecContext(
		argContext,
		`
			INSERT INTO users (
				id, username, display_name, role, active, created_at, updated_at
			)
			VALUES (?, ?, ?, 'viewer', 1, ?, ?)
		`,
		argID,
		argUsername,
		argUsername,
		schemaTimestamp,
		schemaTimestamp,
	); err != nil {
		argTest.Fatalf("insert media schema user: %v", err)
	}
}

func insertValidSchemaMedia(
	argTest *testing.T,
	argContext context.Context,
	argDatabase *sql.DB,
) {
	argTest.Helper()

	if _, err := argDatabase.ExecContext(
		argContext,
		validMediaInsertSQL,
		schemaMediaID,
		"Security Book",
		"security-book.pdf",
		"book",
		"application/pdf",
		1024,
		make([]byte, sha256.Size),
		schemaOwnerID,
		schemaTimestamp,
		schemaTimestamp,
	); err != nil {
		argTest.Fatalf("insert valid media: %v", err)
	}
}
