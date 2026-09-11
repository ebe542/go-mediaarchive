package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/media"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

const secondSchemaGranteeID = "423e4567-e89b-12d3-a456-426614174000"

func TestMediaGrantRepositorySavesFindsAndReplacesMask(t *testing.T) {
	ctx, repository := mediaGrantRepositoryFixture(t)
	initial := mediaGrantFixture(
		t,
		schemaGranteeID,
		media.PermissionDiscover,
		media.PermissionRead,
		media.PermissionDownload,
	)
	if err := repository.Save(ctx, initial); err != nil {
		t.Fatalf("save initial grant: %v", err)
	}

	stored, err := repository.Find(ctx, schemaMediaID, schemaGranteeID)
	if err != nil {
		t.Fatalf("find initial grant: %v", err)
	}
	if stored != initial {
		t.Fatalf("expected grant %+v, got %+v", initial, stored)
	}

	replacement := mediaGrantFixture(t, schemaGranteeID, media.PermissionRead)
	if err := repository.Save(ctx, replacement); err != nil {
		t.Fatalf("replace grant: %v", err)
	}
	stored, err = repository.Find(ctx, schemaMediaID, schemaGranteeID)
	if err != nil {
		t.Fatalf("find replacement grant: %v", err)
	}
	if stored != replacement || stored.Permissions.Has(media.PermissionDiscover) {
		t.Fatalf("expected complete permission replacement, got %+v", stored)
	}
}

func TestMediaGrantRepositoryListsInUserIDOrder(t *testing.T) {
	ctx, repository := mediaGrantRepositoryFixture(t)
	grants := []media.Grant{
		mediaGrantFixture(t, secondSchemaGranteeID, media.PermissionDownload),
		mediaGrantFixture(t, schemaGranteeID, media.PermissionRead),
	}
	for _, grant := range grants {
		if err := repository.Save(ctx, grant); err != nil {
			t.Fatalf("save grant: %v", err)
		}
	}

	stored, err := repository.ListByMedia(ctx, schemaMediaID)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(stored) != 2 ||
		stored[0].UserID != schemaGranteeID ||
		stored[1].UserID != secondSchemaGranteeID {
		t.Fatalf("expected grants ordered by user ID, got %+v", stored)
	}
}

func TestMediaGrantRepositoryReturnsNonNilEmptyList(t *testing.T) {
	ctx, repository := mediaGrantRepositoryFixture(t)

	grants, err := repository.ListByMedia(ctx, schemaMediaID)
	if err != nil {
		t.Fatalf("list empty grants: %v", err)
	}
	if grants == nil || len(grants) != 0 {
		t.Fatalf("expected non-nil empty grant list, got %#v", grants)
	}
}

func TestMediaGrantRepositoryDeletesExplicitly(t *testing.T) {
	ctx, repository := mediaGrantRepositoryFixture(t)
	grant := mediaGrantFixture(t, schemaGranteeID, media.PermissionRead)
	if err := repository.Save(ctx, grant); err != nil {
		t.Fatalf("save grant fixture: %v", err)
	}
	if err := repository.Delete(ctx, grant.MediaID, grant.UserID); err != nil {
		t.Fatalf("delete grant: %v", err)
	}
	if _, err := repository.Find(ctx, grant.MediaID, grant.UserID); !errors.Is(err, media.ErrGrantNotFound) {
		t.Fatalf("expected deleted grant not found, got %v", err)
	}
	if err := repository.Delete(ctx, grant.MediaID, grant.UserID); !errors.Is(err, media.ErrGrantNotFound) {
		t.Fatalf("expected missing delete error, got %v", err)
	}
}

func TestMediaGrantRepositoryRejectsInvalidGrantBeforeSave(t *testing.T) {
	ctx, repository := mediaGrantRepositoryFixture(t)

	for name, grant := range map[string]media.Grant{
		"empty permissions": {
			MediaID: schemaMediaID, UserID: schemaGranteeID, Permissions: 0,
		},
		"unknown permission bit": {
			MediaID: schemaMediaID, UserID: schemaGranteeID, Permissions: 64,
		},
		"invalid media ID": {
			MediaID: "media-id", UserID: schemaGranteeID, Permissions: 1,
		},
		"invalid user ID": {
			MediaID: schemaMediaID, UserID: "user-id", Permissions: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := repository.Save(ctx, grant); !errors.Is(err, media.ErrInvalidGrant) {
				t.Fatalf("expected ErrInvalidGrant, got %v", err)
			}
		})
	}
}

func TestMediaGrantRepositoryEnforcesForeignKeys(t *testing.T) {
	ctx, repository := mediaGrantRepositoryFixture(t)
	permissions := mediaPermissionSet(t, media.PermissionRead)

	for name, grant := range map[string]media.Grant{
		"unknown media": {
			MediaID: "523e4567-e89b-12d3-a456-426614174000",
			UserID:  schemaGranteeID, Permissions: permissions,
		},
		"unknown user": {
			MediaID: schemaMediaID,
			UserID:  "623e4567-e89b-12d3-a456-426614174000", Permissions: permissions,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := repository.Save(ctx, grant); err == nil {
				t.Fatal("expected foreign-key error")
			}
		})
	}
}

func mediaGrantRepositoryFixture(
	argTest *testing.T,
) (context.Context, *sqlitestore.MediaGrantRepository) {
	argTest.Helper()

	ctx, database := openMediaSchemaDatabase(argTest)
	insertMediaSchemaUser(argTest, ctx, database, schemaOwnerID, "media_owner")
	insertMediaSchemaUser(argTest, ctx, database, schemaGranteeID, "media_grantee")
	insertMediaSchemaUser(argTest, ctx, database, secondSchemaGranteeID, "second_grantee")
	insertValidSchemaMedia(argTest, ctx, database)

	return ctx, sqlitestore.NewMediaGrantRepository(database)
}

func mediaGrantFixture(
	argTest *testing.T,
	argUserID string,
	argPermissions ...media.Permission,
) media.Grant {
	argTest.Helper()

	grant, err := media.NewGrant(
		schemaMediaID,
		argUserID,
		mediaPermissionSet(argTest, argPermissions...),
	)
	if err != nil {
		argTest.Fatalf("create media grant fixture: %v", err)
	}

	return grant
}

func mediaPermissionSet(
	argTest *testing.T,
	argPermissions ...media.Permission,
) media.PermissionSet {
	argTest.Helper()

	permissions, err := media.NewPermissionSet(argPermissions...)
	if err != nil {
		argTest.Fatalf("create permission set fixture: %v", err)
	}

	return permissions
}
