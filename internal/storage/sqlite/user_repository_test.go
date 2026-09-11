package sqlite_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	appusers "github.com/ebe542/go-mediaarchive/internal/application/users"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestUserRepositoryDeletesUserAuthenticationRecords(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "repository.db")
	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	now := time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC)
	user, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"deletion_target",
		"Deletion Target",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}
	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("store user fixture: %v", err)
	}

	createdAt := now.Format(time.RFC3339Nano)
	expiresAt := now.Add(time.Hour).Format(time.RFC3339Nano)
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO password_credentials (user_id, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?)`,
		user.ID,
		"$argon2id$fixture",
		createdAt,
		createdAt,
	); err != nil {
		t.Fatalf("insert credential fixture: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO sessions
		 (token_hash, user_id, created_at, last_seen_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		bytes.Repeat([]byte{1}, 32),
		user.ID,
		createdAt,
		createdAt,
		expiresAt,
	); err != nil {
		t.Fatalf("insert session fixture: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO password_enrollments
		 (user_id, token_hash, created_at, expires_at)
		 VALUES (?, ?, ?, ?)`,
		user.ID,
		bytes.Repeat([]byte{2}, 32),
		createdAt,
		expiresAt,
	); err != nil {
		t.Fatalf("insert enrollment fixture: %v", err)
	}

	if err := repository.DeletePreservingLastAdministrator(ctx, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	for _, table := range []string{
		"password_enrollments",
		"sessions",
		"password_credentials",
		"users",
	} {
		var count int
		if err := database.QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM "+table+" WHERE "+userReferenceColumn(table)+" = ?",
			user.ID,
		).Scan(&count); err != nil {
			t.Fatalf("count %s records: %v", table, err)
		}
		if count != 0 {
			t.Errorf("expected no %s records, got %d", table, count)
		}
	}
}

func userReferenceColumn(argTable string) string {
	if argTable == "users" {
		return "id"
	}

	return "user_id"
}

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

func TestUserRepositoryProtectsLastAdministratorDeletion(t *testing.T) {
	ctx := context.Background()
	database, err := sqlitestore.Open(
		ctx,
		filepath.Join(t.TempDir(), "repository.db"),
	)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	administrator, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"last_admin",
		"Last Administrator",
		identity.RoleAdmin,
		time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("create administrator fixture: %v", err)
	}
	if err := repository.Create(ctx, administrator); err != nil {
		t.Fatalf("store administrator fixture: %v", err)
	}

	err = repository.DeletePreservingLastAdministrator(ctx, administrator.ID)
	if !errors.Is(err, identity.ErrLastAdministrator) {
		t.Fatalf("expected ErrLastAdministrator, got %v", err)
	}
	if _, err := repository.FindByID(ctx, administrator.ID); err != nil {
		t.Fatalf("expected protected administrator to remain: %v", err)
	}
}

func TestUserRepositorySerializesConcurrentAdministratorDeletion(t *testing.T) {
	ctx := context.Background()
	database, err := sqlitestore.Open(
		ctx,
		filepath.Join(t.TempDir(), "repository.db"),
	)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	administratorIDs := []string{
		"123e4567-e89b-12d3-a456-426614174000",
		"123e4567-e89b-12d3-a456-426614174001",
	}
	for index, id := range administratorIDs {
		administrator, err := identity.NewUser(
			id,
			fmt.Sprintf("delete_admin_%d", index),
			"Deletion Administrator",
			identity.RoleAdmin,
			time.Date(2026, time.September, 9, 10, 0, index, 0, time.UTC),
		)
		if err != nil {
			t.Fatalf("create administrator fixture: %v", err)
		}
		if err := repository.Create(ctx, administrator); err != nil {
			t.Fatalf("store administrator fixture: %v", err)
		}
	}

	results := make(chan error, len(administratorIDs))
	var waitGroup sync.WaitGroup
	for _, id := range administratorIDs {
		waitGroup.Add(1)
		go func(argID string) {
			defer waitGroup.Done()
			results <- repository.DeletePreservingLastAdministrator(ctx, argID)
		}(id)
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
			t.Fatalf("unexpected concurrent deletion error: %v", result)
		}
	}
	if successCount != 1 || protectedCount != 1 {
		t.Fatalf(
			"expected one deletion and one protected administrator, got %d and %d",
			successCount,
			protectedCount,
		)
	}
}

func TestUserRepositoryRollsBackRelatedDeletionOnUserFailure(t *testing.T) {
	ctx := context.Background()
	database, err := sqlitestore.Open(
		ctx,
		filepath.Join(t.TempDir(), "repository.db"),
	)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	repository := sqlitestore.NewUserRepository(database)
	user, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"rollback_user",
		"Rollback User",
		identity.RoleViewer,
		time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}
	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("store user fixture: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO password_credentials (user_id, password_hash, created_at, updated_at)
		 VALUES (?, '$argon2id$fixture', ?, ?)`,
		user.ID,
		user.CreatedAt.Format(time.RFC3339Nano),
		user.UpdatedAt.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert credential fixture: %v", err)
	}
	mediaOwner, err := identity.NewUser(
		"223e4567-e89b-12d3-a456-426614174000",
		"rollback_owner",
		"Rollback Owner",
		identity.RoleViewer,
		user.CreatedAt,
	)
	if err != nil {
		t.Fatalf("create media owner fixture: %v", err)
	}
	if err := repository.Create(ctx, mediaOwner); err != nil {
		t.Fatalf("store media owner fixture: %v", err)
	}
	insertDeletionTestMedia(
		t,
		ctx,
		database,
		"323e4567-e89b-12d3-a456-426614174000",
		mediaOwner.ID,
	)
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_grants (media_id, user_id, permissions) VALUES (?, ?, ?)`,
		"323e4567-e89b-12d3-a456-426614174000",
		user.ID,
		1,
	); err != nil {
		t.Fatalf("insert rollback grant fixture: %v", err)
	}
	if _, err := database.ExecContext(
		ctx,
		`CREATE TRIGGER reject_test_user_deletion
		 BEFORE DELETE ON users
		 BEGIN
		   SELECT RAISE(ABORT, 'synthetic deletion failure');
		 END`,
	); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if err := repository.DeletePreservingLastAdministrator(ctx, user.ID); err == nil {
		t.Fatal("expected deletion failure")
	}
	var credentialCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM password_credentials WHERE user_id = ?`,
		user.ID,
	).Scan(&credentialCount); err != nil {
		t.Fatalf("count rolled-back credentials: %v", err)
	}
	if credentialCount != 1 {
		t.Fatalf("expected credential rollback, got %d records", credentialCount)
	}
	assertTableRecordCount(t, ctx, database, "media_grants", "user_id", user.ID, 1)
	if _, err := repository.FindByID(ctx, user.ID); err != nil {
		t.Fatalf("expected user rollback: %v", err)
	}
}
