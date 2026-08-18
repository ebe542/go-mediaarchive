package users_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/application/users"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingUserRepository struct {
	createdUser       identity.User
	foundUser         identity.User
	findError         error
	requestedID       string
	requestedUsername string
	updatedUser       identity.User
	updateError       error
	createError       error
	createCalls       int
}

func (repository *recordingUserRepository) Create(
	argContext context.Context,
	argUser identity.User,
) error {
	repository.createCalls++
	repository.createdUser = argUser

	return repository.createError
}

func (repository *recordingUserRepository) FindByID(
	argContext context.Context,
	argID string,
) (identity.User, error) {
	repository.requestedID = argID

	return repository.foundUser, repository.findError
}

func (repository *recordingUserRepository) FindByUsername(
	argContext context.Context,
	argUsername string,
) (identity.User, error) {
	repository.requestedUsername = argUsername

	return repository.foundUser, repository.findError
}

func (repository *recordingUserRepository) Update(
	argContext context.Context,
	argUser identity.User,
) error {
	repository.updatedUser = argUser

	return repository.updateError
}

func TestServiceCreatesUserWithGeneratedValues(t *testing.T) {
	t.Parallel()

	repository := &recordingUserRepository{}
	generatedID := "0198b947-3ec7-7fa0-a024-bf64ed55c667"
	currentTime := time.Date(
		2026,
		time.August,
		18,
		16,
		0,
		0,
		0,
		time.FixedZone("test", 2*60*60),
	)

	service := users.NewService(
		repository,
		func() string {
			return generatedID
		},
		func() time.Time {
			return currentTime
		},
	)

	createdUser, err := service.CreateUser(
		context.Background(),
		users.CreateUserInput{
			Username:    "  Service_User ",
			DisplayName: " Service User ",
			Role:        identity.RoleEditor,
		},
	)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	expectedUser := identity.User{
		ID:          generatedID,
		Username:    "service_user",
		DisplayName: "Service User",
		Role:        identity.RoleEditor,
		Active:      true,
		CreatedAt:   currentTime.UTC(),
		UpdatedAt:   currentTime.UTC(),
	}

	if createdUser != expectedUser {
		t.Fatalf(
			"expected created user %#v, got %#v",
			expectedUser,
			createdUser,
		)
	}

	if repository.createdUser != expectedUser {
		t.Fatalf(
			"expected persisted user %#v, got %#v",
			expectedUser,
			repository.createdUser,
		)
	}
}

func TestServiceFindsUserByID(t *testing.T) {
	t.Parallel()

	expectedUser := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "service_user",
		DisplayName: "Service User",
		Role:        identity.RoleViewer,
		Active:      true,
		CreatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
	}

	repository := &recordingUserRepository{
		foundUser: expectedUser,
	}

	service := users.NewService(
		repository,
		func() string {
			return ""
		},
		func() time.Time {
			return time.Time{}
		},
	)

	storedUser, err := service.UserByID(
		context.Background(),
		expectedUser.ID,
	)
	if err != nil {
		t.Fatalf("find user by ID: %v", err)
	}

	if repository.requestedID != expectedUser.ID {
		t.Fatalf(
			"expected requested ID %q, got %q",
			expectedUser.ID,
			repository.requestedID,
		)
	}

	if storedUser != expectedUser {
		t.Fatalf(
			"expected user %#v, got %#v",
			expectedUser,
			storedUser,
		)
	}
}

func TestServiceFindsUserByNormalizedUsername(t *testing.T) {
	t.Parallel()

	expectedUser := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "service_user",
		DisplayName: "Service User",
		Role:        identity.RoleViewer,
		Active:      true,
		CreatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
	}

	repository := &recordingUserRepository{
		foundUser: expectedUser,
	}

	service := users.NewService(
		repository,
		func() string {
			return ""
		},
		func() time.Time {
			return time.Time{}
		},
	)

	storedUser, err := service.UserByUsername(
		context.Background(),
		"  Service_User ",
	)
	if err != nil {
		t.Fatalf("find user by username: %v", err)
	}

	if repository.requestedUsername != "service_user" {
		t.Fatalf(
			"expected normalized username %q, got %q",
			"service_user",
			repository.requestedUsername,
		)
	}

	if storedUser != expectedUser {
		t.Fatalf(
			"expected user %#v, got %#v",
			expectedUser,
			storedUser,
		)
	}
}

func TestServiceUpdatesUserAndPreservesImmutableValues(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(
		2026,
		time.August,
		18,
		14,
		0,
		0,
		0,
		time.UTC,
	)
	updatedAt := createdAt.Add(2 * time.Hour)

	existingUser := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "original_user",
		DisplayName: "Original User",
		Role:        identity.RoleViewer,
		Active:      false,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}

	repository := &recordingUserRepository{
		foundUser: existingUser,
	}

	service := users.NewService(
		repository,
		func() string {
			return ""
		},
		func() time.Time {
			return updatedAt
		},
	)

	updatedUser, err := service.UpdateUser(
		context.Background(),
		existingUser.ID,
		users.UpdateUserInput{
			Username:    " Updated_User ",
			DisplayName: " Updated User ",
			Role:        identity.RoleAdmin,
		},
	)
	if err != nil {
		t.Fatalf("update user: %v", err)
	}

	expectedUser := identity.User{
		ID:          existingUser.ID,
		Username:    "updated_user",
		DisplayName: "Updated User",
		Role:        identity.RoleAdmin,
		Active:      false,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	if repository.requestedID != existingUser.ID {
		t.Fatalf(
			"expected requested ID %q, got %q",
			existingUser.ID,
			repository.requestedID,
		)
	}

	if updatedUser != expectedUser {
		t.Fatalf(
			"expected updated user %#v, got %#v",
			expectedUser,
			updatedUser,
		)
	}

	if repository.updatedUser != expectedUser {
		t.Fatalf(
			"expected persisted user %#v, got %#v",
			expectedUser,
			repository.updatedUser,
		)
	}
}

func TestServiceSetsUserActiveState(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(
		2026,
		time.August,
		18,
		14,
		0,
		0,
		0,
		time.UTC,
	)
	updatedAt := createdAt.Add(3 * time.Hour)

	existingUser := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "active_user",
		DisplayName: "Active User",
		Role:        identity.RoleEditor,
		Active:      true,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}

	repository := &recordingUserRepository{
		foundUser: existingUser,
	}

	service := users.NewService(
		repository,
		func() string {
			return ""
		},
		func() time.Time {
			return updatedAt
		},
	)

	deactivatedUser, err := service.SetUserActive(
		context.Background(),
		existingUser.ID,
		false,
	)
	if err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	expectedUser := existingUser
	expectedUser.Active = false
	expectedUser.UpdatedAt = updatedAt

	if deactivatedUser != expectedUser {
		t.Fatalf(
			"expected deactivated user %#v, got %#v",
			expectedUser,
			deactivatedUser,
		)
	}

	if repository.updatedUser != expectedUser {
		t.Fatalf(
			"expected persisted user %#v, got %#v",
			expectedUser,
			repository.updatedUser,
		)
	}
}

func TestServicePreservesRepositoryConflict(t *testing.T) {
	t.Parallel()

	repository := &recordingUserRepository{
		createError: identity.ErrUserConflict,
	}

	service := users.NewService(
		repository,
		func() string {
			return "0198b947-3ec7-7fa0-a024-bf64ed55c667"
		},
		func() time.Time {
			return time.Date(
				2026,
				time.August,
				18,
				18,
				0,
				0,
				0,
				time.UTC,
			)
		},
	)

	_, err := service.CreateUser(
		context.Background(),
		users.CreateUserInput{
			Username:    "conflict_user",
			DisplayName: "Conflict User",
			Role:        identity.RoleViewer,
		},
	)
	if !errors.Is(err, identity.ErrUserConflict) {
		t.Fatalf("expected ErrUserConflict, got %v", err)
	}
}

func TestServiceRejectsInvalidGeneratedIDBeforePersistence(t *testing.T) {
	t.Parallel()

	repository := &recordingUserRepository{}

	service := users.NewService(
		repository,
		func() string {
			return "not-a-uuid"
		},
		func() time.Time {
			return time.Date(
				2026,
				time.August,
				18,
				18,
				0,
				0,
				0,
				time.UTC,
			)
		},
	)

	_, err := service.CreateUser(
		context.Background(),
		users.CreateUserInput{
			Username:    "valid_user",
			DisplayName: "Valid User",
			Role:        identity.RoleViewer,
		},
	)
	if !errors.Is(err, identity.ErrInvalidUserID) {
		t.Fatalf("expected ErrInvalidUserID, got %v", err)
	}

	if repository.createCalls != 0 {
		t.Fatalf(
			"expected no persistence call, got %d",
			repository.createCalls,
		)
	}
}
