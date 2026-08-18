package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestUserRepositoryCreatesAndFindsUserByID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "repository.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)

	createdAt := time.Date(
		2026,
		time.August,
		18,
		10,
		30,
		0,
		0,
		time.FixedZone("test", 2*60*60),
	)

	user, err := identity.NewUser(
		"0198b947-3ec7-7fa0-a024-bf64ed55c667",
		"Test_User",
		"Test User",
		identity.RoleEditor,
		createdAt,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("store user: %v", err)
	}

	storedUser, err := repository.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("find user by ID: %v", err)
	}

	if storedUser != user {
		t.Fatalf(
			"expected stored user %#v, got %#v",
			user,
			storedUser,
		)
	}
}

func TestUserRepositoryReturnsNotFoundForUnknownID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "repository.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)

	_, err = repository.FindByID(
		ctx,
		"0198b947-3ec7-7fa0-a024-bf64ed55c667",
	)
	if !errors.Is(err, identity.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserRepositoryReturnsConflictForDuplicateUsername(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "repository.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)

	firstUser, err := identity.NewUser(
		"0198b947-3ec7-7fa0-a024-bf64ed55c667",
		"duplicate_user",
		"First User",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create first user fixture: %v", err)
	}

	secondUser, err := identity.NewUser(
		"0198b947-3ec7-7fa0-a024-bf64ed55c668",
		"duplicate_user",
		"Second User",
		identity.RoleEditor,
		now,
	)
	if err != nil {
		t.Fatalf("create second user fixture: %v", err)
	}

	if err := repository.Create(ctx, firstUser); err != nil {
		t.Fatalf("store first user: %v", err)
	}

	err = repository.Create(ctx, secondUser)
	if !errors.Is(err, identity.ErrUserConflict) {
		t.Fatalf("expected ErrUserConflict, got %v", err)
	}
}

func TestUserRepositoryFindsUserByNormalizedUsername(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "repository.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	now := time.Date(2026, time.August, 18, 13, 0, 0, 0, time.UTC)

	user, err := identity.NewUser(
		"0198b947-3ec7-7fa0-a024-bf64ed55c667",
		"archive_user",
		"Archive User",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("store user: %v", err)
	}

	storedUser, err := repository.FindByUsername(ctx, "  Archive_User ")
	if err != nil {
		t.Fatalf("find user by username: %v", err)
	}

	if storedUser != user {
		t.Fatalf(
			"expected stored user %#v, got %#v",
			user,
			storedUser,
		)
	}
}

func TestUserRepositoryUpdatesUserAndPreservesCreationTime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "repository.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	createdAt := time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC)

	user, err := identity.NewUser(
		"0198b947-3ec7-7fa0-a024-bf64ed55c667",
		"update_user",
		"Original Name",
		identity.RoleViewer,
		createdAt,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("store user: %v", err)
	}

	updatedAt := createdAt.Add(time.Hour)
	updatedUser := user
	updatedUser.DisplayName = "Updated Name"
	updatedUser.Role = identity.RoleAdmin
	updatedUser.Active = false
	updatedUser.CreatedAt = updatedAt
	updatedUser.UpdatedAt = updatedAt

	if err := repository.Update(ctx, updatedUser); err != nil {
		t.Fatalf("update user: %v", err)
	}

	storedUser, err := repository.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("find updated user: %v", err)
	}

	expectedUser := updatedUser
	expectedUser.CreatedAt = createdAt

	if storedUser != expectedUser {
		t.Fatalf(
			"expected updated user %#v, got %#v",
			expectedUser,
			storedUser,
		)
	}
}

func TestUserRepositoryReturnsConflictForDuplicateID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "repository.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	now := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	userID := "0198b947-3ec7-7fa0-a024-bf64ed55c667"

	firstUser, err := identity.NewUser(
		userID,
		"first_user",
		"First User",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create first user fixture: %v", err)
	}

	secondUser, err := identity.NewUser(
		userID,
		"second_user",
		"Second User",
		identity.RoleEditor,
		now,
	)
	if err != nil {
		t.Fatalf("create second user fixture: %v", err)
	}

	if err := repository.Create(ctx, firstUser); err != nil {
		t.Fatalf("store first user: %v", err)
	}

	err = repository.Create(ctx, secondUser)
	if !errors.Is(err, identity.ErrUserConflict) {
		t.Fatalf("expected ErrUserConflict, got %v", err)
	}
}
