package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestPasswordCredentialRepositoryFindsCredentialByUserID(
	t *testing.T,
) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	adminUser, err := identity.NewUser(
		userID,
		"archive_admin",
		"Archive Administrator",
		identity.RoleAdmin,
		now,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	expectedCredential, err := credential.NewPasswordCredential(
		userID,
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		now,
	)
	if err != nil {
		t.Fatalf("create credential fixture: %v", err)
	}

	if err := sqlitestore.NewAdminBootstrapRepository(
		database,
	).BootstrapAdmin(
		ctx,
		adminUser,
		expectedCredential,
	); err != nil {
		t.Fatalf("store credential fixture: %v", err)
	}

	repository := sqlitestore.NewPasswordCredentialRepository(database)

	storedCredential, err := repository.FindByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("find password credential: %v", err)
	}

	if storedCredential != expectedCredential {
		t.Fatalf(
			"expected credential %#v, got %#v",
			expectedCredential,
			storedCredential,
		)
	}
}

func TestPasswordCredentialRepositoryReturnsNotFoundForUserWithoutCredential(
	t *testing.T,
) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	user, err := identity.NewUser(
		userID,
		"credentialless_user",
		"Credentialless User",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	if err := sqlitestore.NewUserRepository(database).Create(
		ctx,
		user,
	); err != nil {
		t.Fatalf("store user fixture: %v", err)
	}

	repository := sqlitestore.NewPasswordCredentialRepository(database)

	_, err = repository.FindByUserID(ctx, userID)
	if !errors.Is(
		err,
		credential.ErrPasswordCredentialNotFound,
	) {
		t.Fatalf(
			"expected ErrPasswordCredentialNotFound, got %v",
			err,
		)
	}
}
