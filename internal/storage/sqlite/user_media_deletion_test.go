package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestUserRepositoryDeletesHeldMediaGrantsWithoutDeletingMedia(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertMediaSchemaUser(t, ctx, database, schemaGranteeID, "media_grantee")
	insertValidSchemaMedia(t, ctx, database)
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_grants (media_id, user_id, permissions) VALUES (?, ?, ?)`,
		schemaMediaID,
		schemaGranteeID,
		3,
	); err != nil {
		t.Fatalf("insert grant fixture: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	if err := repository.DeletePreservingLastAdministrator(ctx, schemaGranteeID); err != nil {
		t.Fatalf("delete media grantee: %v", err)
	}
	assertTableRecordCount(t, ctx, database, "users", "id", schemaGranteeID, 0)
	assertTableRecordCount(t, ctx, database, "media_grants", "user_id", schemaGranteeID, 0)
	assertTableRecordCount(t, ctx, database, "media_items", "id", schemaMediaID, 1)
}

func TestUserRepositoryPreservesUserAndCredentialsWhenMediaIsOwned(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertValidSchemaMedia(t, ctx, database)
	if _, err := database.ExecContext(
		ctx,
		`
			INSERT INTO password_credentials (
				user_id, password_hash, created_at, updated_at
			)
			VALUES (?, '$argon2id$fixture', ?, ?)
		`,
		schemaOwnerID,
		schemaTimestamp,
		schemaTimestamp,
	); err != nil {
		t.Fatalf("insert credential fixture: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	err := repository.DeletePreservingLastAdministrator(ctx, schemaOwnerID)
	if !errors.Is(err, identity.ErrUserOwnsMedia) {
		t.Fatalf("expected ErrUserOwnsMedia, got %v", err)
	}
	assertTableRecordCount(t, ctx, database, "users", "id", schemaOwnerID, 1)
	assertTableRecordCount(t, ctx, database, "password_credentials", "user_id", schemaOwnerID, 1)
	assertTableRecordCount(t, ctx, database, "media_items", "owner_id", schemaOwnerID, 1)
}

func insertDeletionTestMedia(
	test *testing.T,
	ctx context.Context,
	database *sql.DB,
	mediaID string,
	ownerID string,
) {
	test.Helper()

	if _, err := database.ExecContext(
		ctx,
		validMediaInsertSQL,
		mediaID,
		"Deletion Test",
		"deletion-test.pdf",
		"document",
		"application/pdf",
		1024,
		make([]byte, sha256.Size),
		ownerID,
		schemaTimestamp,
		schemaTimestamp,
	); err != nil {
		test.Fatalf("insert deletion test media: %v", err)
	}
}

func assertTableRecordCount(
	test *testing.T,
	ctx context.Context,
	database *sql.DB,
	table string,
	column string,
	value string,
	expected int,
) {
	test.Helper()

	var count int
	query := "SELECT COUNT(*) FROM " + table + " WHERE " + column + " = ?"
	if err := database.QueryRowContext(ctx, query, value).Scan(&count); err != nil {
		test.Fatalf("count %s records: %v", table, err)
	}
	if count != expected {
		test.Fatalf("expected %d %s records, got %d", expected, table, count)
	}
}
