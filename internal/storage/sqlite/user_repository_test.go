package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	appusers "github.com/ebe542/go-mediaarchive/internal/application/users"
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

func TestUserRepositoryListsUsersWithStableKeysetPagination(t *testing.T) {
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
	firstTime := time.Date(2026, time.August, 29, 9, 0, 0, 0, time.UTC)
	userFixtures := []struct {
		id        string
		username  string
		createdAt time.Time
	}{
		{
			id:        "123e4567-e89b-12d3-a456-426614174002",
			username:  "third_user",
			createdAt: firstTime.Add(time.Minute),
		},
		{
			id:        "123e4567-e89b-12d3-a456-426614174001",
			username:  "second_user",
			createdAt: firstTime,
		},
		{
			id:        "123e4567-e89b-12d3-a456-426614174000",
			username:  "first_user",
			createdAt: firstTime,
		},
	}

	for _, fixture := range userFixtures {
		user, err := identity.NewUser(
			fixture.id,
			fixture.username,
			"Paginated User",
			identity.RoleViewer,
			fixture.createdAt,
		)
		if err != nil {
			t.Fatalf("create user fixture: %v", err)
		}
		if err := repository.Create(ctx, user); err != nil {
			t.Fatalf("store user fixture: %v", err)
		}
	}

	firstPage, err := repository.ListUsers(ctx, nil, 2)
	if err != nil {
		t.Fatalf("list first user page: %v", err)
	}
	if len(firstPage) != 2 ||
		firstPage[0].ID != "123e4567-e89b-12d3-a456-426614174000" ||
		firstPage[1].ID != "123e4567-e89b-12d3-a456-426614174001" {
		t.Fatalf("unexpected first page order: %+v", firstPage)
	}

	cursor, err := appusers.NewCursor(
		firstPage[1].CreatedAt,
		firstPage[1].ID,
	)
	if err != nil {
		t.Fatalf("create continuation cursor: %v", err)
	}
	secondPage, err := repository.ListUsers(ctx, &cursor, 2)
	if err != nil {
		t.Fatalf("list second user page: %v", err)
	}
	if len(secondPage) != 1 ||
		secondPage[0].ID != "123e4567-e89b-12d3-a456-426614174002" {
		t.Fatalf("unexpected second page: %+v", secondPage)
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

func TestUserRepositoryProtectsLastActiveAdministrator(t *testing.T) {
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
	now := time.Date(2026, time.August, 27, 10, 0, 0, 0, time.UTC)
	firstAdministrator, err := identity.NewUser(
		"0198b947-3ec7-7fa0-a024-bf64ed55c667",
		"first_admin",
		"First Administrator",
		identity.RoleAdmin,
		now,
	)
	if err != nil {
		t.Fatalf("create first administrator fixture: %v", err)
	}
	if err := repository.Create(ctx, firstAdministrator); err != nil {
		t.Fatalf("store first administrator: %v", err)
	}

	demotedAdministrator, err := firstAdministrator.UpdateDetails(
		firstAdministrator.Username,
		firstAdministrator.DisplayName,
		identity.RoleEditor,
		now.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("create demoted administrator fixture: %v", err)
	}

	err = repository.UpdatePreservingLastAdministrator(
		ctx,
		demotedAdministrator,
	)
	if !errors.Is(err, identity.ErrLastAdministrator) {
		t.Fatalf("expected ErrLastAdministrator, got %v", err)
	}

	deactivatedAdministrator, err := firstAdministrator.SetActive(
		false,
		now.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("create deactivated administrator fixture: %v", err)
	}
	err = repository.UpdatePreservingLastAdministrator(
		ctx,
		deactivatedAdministrator,
	)
	if !errors.Is(err, identity.ErrLastAdministrator) {
		t.Fatalf("expected ErrLastAdministrator, got %v", err)
	}

	secondAdministrator, err := identity.NewUser(
		"0298b947-3ec7-7fa0-a024-bf64ed55c667",
		"second_admin",
		"Second Administrator",
		identity.RoleAdmin,
		now,
	)
	if err != nil {
		t.Fatalf("create second administrator fixture: %v", err)
	}
	if err := repository.Create(ctx, secondAdministrator); err != nil {
		t.Fatalf("store second administrator: %v", err)
	}

	if err := repository.UpdatePreservingLastAdministrator(
		ctx,
		demotedAdministrator,
	); err != nil {
		t.Fatalf("demote administrator with replacement: %v", err)
	}
}

func TestUserRepositorySerializesConcurrentAdministratorRemoval(
	t *testing.T,
) {
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
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	administratorIDs := []string{
		"0198b947-3ec7-7fa0-a024-bf64ed55c667",
		"0298b947-3ec7-7fa0-a024-bf64ed55c667",
	}
	demotedAdministrators := make([]identity.User, 0, len(administratorIDs))

	for index, administratorID := range administratorIDs {
		administrator, err := identity.NewUser(
			administratorID,
			"concurrent_admin_"+string(rune('a'+index)),
			"Concurrent Administrator",
			identity.RoleAdmin,
			now,
		)
		if err != nil {
			t.Fatalf("create administrator fixture: %v", err)
		}
		if err := repository.Create(ctx, administrator); err != nil {
			t.Fatalf("store administrator fixture: %v", err)
		}

		demotedAdministrator, err := administrator.UpdateDetails(
			administrator.Username,
			administrator.DisplayName,
			identity.RoleEditor,
			now.Add(time.Hour),
		)
		if err != nil {
			t.Fatalf("create demoted administrator fixture: %v", err)
		}
		demotedAdministrators = append(
			demotedAdministrators,
			demotedAdministrator,
		)
	}

	results := make(chan error, len(demotedAdministrators))
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(demotedAdministrators))

	for _, administrator := range demotedAdministrators {
		go func(argAdministrator identity.User) {
			defer waitGroup.Done()

			results <- repository.UpdatePreservingLastAdministrator(
				ctx,
				argAdministrator,
			)
		}(administrator)
	}

	waitGroup.Wait()
	close(results)

	successCount := 0
	protectedCount := 0
	for result := range results {
		switch {
		case result == nil:
			successCount++
		case errors.Is(result, identity.ErrLastAdministrator):
			protectedCount++
		default:
			t.Fatalf("unexpected concurrent update error: %v", result)
		}
	}

	if successCount != 1 || protectedCount != 1 {
		t.Fatalf(
			"expected one success and one protected update, got %d and %d",
			successCount,
			protectedCount,
		)
	}
}
