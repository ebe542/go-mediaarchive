package users_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/application/users"
	"github.com/ebe542/go-mediaarchive/internal/audit"
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
	protectedUpdates  int
	deletedID         string
	deletedEvent      audit.Event
	deleteError       error
	deleteCalls       int
	listedUsers       []identity.User
	listError         error
	listedCursor      *users.Cursor
	listedLimit       int
	createdEvent      audit.Event
	updatedEvent      audit.Event
}

func (repository *recordingUserRepository) CreateWithAudit(
	ctx context.Context,
	user identity.User,
	event audit.Event,
) error {
	repository.createdEvent = event

	return repository.Create(ctx, user)
}

func (repository *recordingUserRepository) UpdatePreservingLastAdministratorWithAudit(
	ctx context.Context,
	user identity.User,
	event audit.Event,
) error {
	repository.updatedEvent = event

	return repository.UpdatePreservingLastAdministrator(ctx, user)
}

type recordingAuditAppender struct {
	event audit.Event
	err   error
}

func (appender *recordingAuditAppender) Append(
	_ context.Context,
	event audit.Event,
) error {
	appender.event = event

	return appender.err
}

func newUserService(
	repository users.Repository,
	idGenerator users.IDGenerator,
	clock users.Clock,
) *users.Service {
	return users.NewService(
		repository,
		idGenerator,
		clock,
		&recordingAuditAppender{},
		func() string { return "823e4567-e89b-12d3-a456-426614174000" },
	)
}

func testAdministrator() identity.User {
	return identity.User{
		ID:       "723e4567-e89b-12d3-a456-426614174000",
		Username: "audit_admin",
		Role:     identity.RoleAdmin,
		Active:   true,
	}
}

func (repository *recordingUserRepository) DeletePreservingLastAdministrator(
	_ context.Context,
	id string,
) error {
	repository.deleteCalls++
	repository.deletedID = id

	return repository.deleteError
}

func (repository *recordingUserRepository) DeletePreservingLastAdministratorWithAudit(
	_ context.Context,
	id string,
	eventFactory users.UserDeletionEventFactory,
) error {
	repository.deleteCalls++
	repository.deletedID = id
	if errors.Is(repository.deleteError, identity.ErrUserNotFound) {
		return repository.deleteError
	}

	event, err := eventFactory(repository.foundUser)
	if err != nil {
		return err
	}
	repository.deletedEvent = event

	return repository.deleteError
}

func (repository *recordingUserRepository) ListUsers(
	_ context.Context,
	cursor *users.Cursor,
	limit int,
) ([]identity.User, error) {
	repository.listedCursor = cursor
	repository.listedLimit = limit

	return repository.listedUsers, repository.listError
}

func (repository *recordingUserRepository) Create(
	ctx context.Context,
	user identity.User,
) error {
	repository.createCalls++
	repository.createdUser = user

	return repository.createError
}

func (repository *recordingUserRepository) FindByID(
	ctx context.Context,
	id string,
) (identity.User, error) {
	repository.requestedID = id

	return repository.foundUser, repository.findError
}

func (repository *recordingUserRepository) FindByUsername(
	ctx context.Context,
	username string,
) (identity.User, error) {
	repository.requestedUsername = username

	return repository.foundUser, repository.findError
}

func (repository *recordingUserRepository) Update(
	ctx context.Context,
	user identity.User,
) error {
	repository.updatedUser = user

	return repository.updateError
}

func (repository *recordingUserRepository) UpdatePreservingLastAdministrator(
	ctx context.Context,
	user identity.User,
) error {
	repository.protectedUpdates++
	repository.updatedUser = user

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

	service := newUserService(
		repository,
		func() string {
			return generatedID
		},
		func() time.Time {
			return currentTime
		})

	createdUser, err := service.CreateUser(
		context.Background(),
		testAdministrator(),
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
	if repository.createdEvent.Type != audit.TypeUserCreated ||
		repository.createdEvent.Outcome != audit.OutcomeSuccess ||
		repository.createdEvent.ActorID != testAdministrator().ID ||
		repository.createdEvent.TargetID != expectedUser.ID ||
		repository.createdEvent.TargetName != expectedUser.Username {
		t.Fatalf("unexpected user creation audit event: %+v", repository.createdEvent)
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

	service := newUserService(
		repository,
		func() string {
			return ""
		},
		func() time.Time {
			return time.Time{}
		})

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

	service := newUserService(
		repository,
		func() string {
			return ""
		},
		func() time.Time {
			return time.Time{}
		})

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

	service := newUserService(
		repository,
		func() string {
			return ""
		},
		func() time.Time {
			return updatedAt
		})

	updatedUser, err := service.UpdateUser(
		context.Background(),
		testAdministrator(),
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
	if repository.updatedEvent.Type != audit.TypeUserUpdated ||
		repository.updatedEvent.Outcome != audit.OutcomeSuccess ||
		repository.updatedEvent.TargetID != expectedUser.ID ||
		repository.updatedEvent.TargetName != expectedUser.Username {
		t.Fatalf("unexpected user update audit event: %+v", repository.updatedEvent)
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

	service := newUserService(
		repository,
		func() string {
			return ""
		},
		func() time.Time {
			return updatedAt
		})

	deactivatedUser, err := service.SetUserActive(
		context.Background(),
		testAdministrator(),
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
	if repository.updatedEvent.Type != audit.TypeUserDeactivated ||
		repository.updatedEvent.Outcome != audit.OutcomeSuccess ||
		repository.updatedEvent.TargetID != expectedUser.ID {
		t.Fatalf("unexpected user deactivation audit event: %+v", repository.updatedEvent)
	}
}

func TestServicePreservesRepositoryConflict(t *testing.T) {
	t.Parallel()

	repository := &recordingUserRepository{
		createError: identity.ErrUserConflict,
	}

	service := newUserService(
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
		})

	_, err := service.CreateUser(
		context.Background(),
		testAdministrator(),
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

	service := newUserService(
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
		})

	_, err := service.CreateUser(
		context.Background(),
		testAdministrator(),
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

func TestServiceRejectsAdministratorSelfDemotion(t *testing.T) {
	t.Parallel()

	administrator := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "archive_admin",
		DisplayName: "Archive Administrator",
		Role:        identity.RoleAdmin,
		Active:      true,
		CreatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
	}
	repository := &recordingUserRepository{foundUser: administrator}
	service := newUserService(
		repository,
		func() string { return "" },
		func() time.Time { return administrator.UpdatedAt.Add(time.Hour) })

	_, err := service.UpdateUser(
		context.Background(),
		administrator,
		administrator.ID,
		users.UpdateUserInput{
			Username:    administrator.Username,
			DisplayName: administrator.DisplayName,
			Role:        identity.RoleEditor,
		},
	)
	if !errors.Is(err, users.ErrSelfLockout) {
		t.Fatalf("expected ErrSelfLockout, got %v", err)
	}
	if repository.protectedUpdates != 0 {
		t.Fatalf(
			"expected no protected update, got %d",
			repository.protectedUpdates,
		)
	}
}

func TestServiceRejectsAdministratorSelfDeactivation(t *testing.T) {
	t.Parallel()

	administrator := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "archive_admin",
		DisplayName: "Archive Administrator",
		Role:        identity.RoleAdmin,
		Active:      true,
		CreatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
	}
	repository := &recordingUserRepository{foundUser: administrator}
	service := newUserService(
		repository,
		func() string { return "" },
		func() time.Time { return administrator.UpdatedAt.Add(time.Hour) })

	_, err := service.SetUserActive(
		context.Background(),
		administrator,
		administrator.ID,
		false,
	)
	if !errors.Is(err, users.ErrSelfLockout) {
		t.Fatalf("expected ErrSelfLockout, got %v", err)
	}
	if repository.protectedUpdates != 0 {
		t.Fatalf(
			"expected no protected update, got %d",
			repository.protectedUpdates,
		)
	}
}

func TestServiceAuditsDeniedAdministratorSelfLockout(t *testing.T) {
	t.Parallel()

	administrator := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "archive_admin",
		DisplayName: "Archive Administrator",
		Role:        identity.RoleAdmin,
		Active:      true,
		CreatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
	}
	repository := &recordingUserRepository{foundUser: administrator}
	appender := &recordingAuditAppender{}
	service := users.NewService(
		repository,
		func() string { return "" },
		func() time.Time { return administrator.UpdatedAt.Add(time.Hour) },
		appender,
		func() string { return "823e4567-e89b-12d3-a456-426614174000" },
	)

	_, err := service.UpdateUser(
		context.Background(),
		administrator,
		administrator.ID,
		users.UpdateUserInput{
			Username:    administrator.Username,
			DisplayName: administrator.DisplayName,
			Role:        identity.RoleEditor,
		},
	)
	if !errors.Is(err, users.ErrSelfLockout) {
		t.Fatalf("expected ErrSelfLockout, got %v", err)
	}
	if appender.event.Type != audit.TypeUserUpdated ||
		appender.event.Outcome != audit.OutcomeDenied ||
		appender.event.Reason != audit.ReasonSelfLockout ||
		appender.event.ActorID != administrator.ID ||
		appender.event.TargetID != administrator.ID {
		t.Fatalf("unexpected denied user audit event: %#v", appender.event)
	}
}

func TestServiceFailsClosedWhenDeniedUserAuditFails(t *testing.T) {
	t.Parallel()

	administrator := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "archive_admin",
		DisplayName: "Archive Administrator",
		Role:        identity.RoleAdmin,
		Active:      true,
		CreatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
	}
	repository := &recordingUserRepository{foundUser: administrator}
	auditError := errors.New("audit storage unavailable")
	service := users.NewService(
		repository,
		func() string { return "" },
		func() time.Time { return administrator.UpdatedAt.Add(time.Hour) },
		&recordingAuditAppender{err: auditError},
		func() string { return "823e4567-e89b-12d3-a456-426614174000" },
	)

	_, err := service.SetUserActive(
		context.Background(),
		administrator,
		administrator.ID,
		false,
	)
	if !errors.Is(err, auditError) {
		t.Fatalf("expected audit error, got %v", err)
	}
	if errors.Is(err, users.ErrSelfLockout) {
		t.Fatalf("expected audit failure to replace operation error, got %v", err)
	}
}

func TestServiceKeepsActivationUpdateIdempotent(t *testing.T) {
	t.Parallel()

	existingUser := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "archive_viewer",
		DisplayName: "Archive Viewer",
		Role:        identity.RoleViewer,
		Active:      true,
		CreatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
	}
	repository := &recordingUserRepository{foundUser: existingUser}
	service := newUserService(
		repository,
		func() string { return "" },
		func() time.Time { return existingUser.UpdatedAt.Add(time.Hour) })

	unchangedUser, err := service.SetUserActive(
		context.Background(),
		testAdministrator(),
		existingUser.ID,
		true,
	)
	if err != nil {
		t.Fatalf("keep user active: %v", err)
	}
	if unchangedUser != existingUser {
		t.Fatalf("expected unchanged user %#v, got %#v", existingUser, unchangedUser)
	}
	if repository.protectedUpdates != 0 {
		t.Fatalf(
			"expected no protected update, got %d",
			repository.protectedUpdates,
		)
	}
}

func TestServicePreservesLastAdministratorProtection(t *testing.T) {
	t.Parallel()

	administrator := identity.User{
		ID:          "0198b947-3ec7-7fa0-a024-bf64ed55c667",
		Username:    "archive_admin",
		DisplayName: "Archive Administrator",
		Role:        identity.RoleAdmin,
		Active:      true,
		CreatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
	}
	repository := &recordingUserRepository{
		foundUser:   administrator,
		updateError: identity.ErrLastAdministrator,
	}
	service := newUserService(
		repository,
		func() string { return "" },
		func() time.Time { return administrator.UpdatedAt.Add(time.Hour) })

	_, err := service.SetUserActive(
		context.Background(),
		testAdministrator(),
		administrator.ID,
		false,
	)
	if !errors.Is(err, identity.ErrLastAdministrator) {
		t.Fatalf("expected ErrLastAdministrator, got %v", err)
	}
}

func TestServiceDeletesAnotherUser(t *testing.T) {
	now := time.Date(2026, time.October, 6, 9, 0, 0, 0, time.UTC)
	target, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"deletion_target",
		"Deletion Target",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create deletion target: %v", err)
	}
	repository := &recordingUserRepository{foundUser: target}
	service := newUserService(
		repository,
		func() string { return "" },
		func() time.Time { return now })

	if err := service.DeleteUser(
		context.Background(),
		testAdministrator(),
		target.ID,
	); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if repository.deleteCalls != 1 || repository.deletedID != target.ID {
		t.Errorf(
			"expected deletion of %q, got calls=%d ID=%q",
			target.ID,
			repository.deleteCalls,
			repository.deletedID,
		)
	}
	if repository.deletedEvent.Type != audit.TypeUserDeleted ||
		repository.deletedEvent.Outcome != audit.OutcomeSuccess ||
		repository.deletedEvent.ActorID != testAdministrator().ID ||
		repository.deletedEvent.TargetID != target.ID ||
		repository.deletedEvent.TargetName != target.Username {
		t.Fatalf("unexpected user deletion audit event: %#v", repository.deletedEvent)
	}
}

func TestServiceRejectsSelfDeletion(t *testing.T) {
	repository := &recordingUserRepository{}
	now := time.Date(2026, time.October, 6, 9, 30, 0, 0, time.UTC)
	service := newUserService(
		repository,
		func() string { return "" },
		func() time.Time { return now })
	actor := testAdministrator()

	err := service.DeleteUser(context.Background(), actor, actor.ID)
	if !errors.Is(err, users.ErrSelfDeletion) {
		t.Fatalf("expected ErrSelfDeletion, got %v", err)
	}
	if repository.deleteCalls != 0 {
		t.Errorf("expected no repository deletion, got %d", repository.deleteCalls)
	}
}

func TestServiceRejectsInvalidDeletionID(t *testing.T) {
	repository := &recordingUserRepository{}
	now := time.Date(2026, time.October, 6, 10, 0, 0, 0, time.UTC)
	service := newUserService(
		repository,
		func() string { return "" },
		func() time.Time { return now })

	err := service.DeleteUser(
		context.Background(),
		testAdministrator(),
		"not-a-user-id",
	)
	if !errors.Is(err, identity.ErrInvalidUserID) {
		t.Fatalf("expected ErrInvalidUserID, got %v", err)
	}
	if repository.deleteCalls != 0 {
		t.Errorf("expected no repository deletion, got %d", repository.deleteCalls)
	}
}

func TestServicePreservesDeletionRepositoryErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"unknown user", identity.ErrUserNotFound},
		{"last administrator", identity.ErrLastAdministrator},
		{"owned media", identity.ErrUserOwnsMedia},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.October, 6, 10, 30, 0, 0, time.UTC)
			target, err := identity.NewUser(
				"123e4567-e89b-12d3-a456-426614174000",
				"deletion_target",
				"Deletion Target",
				identity.RoleViewer,
				now,
			)
			if err != nil {
				t.Fatalf("create deletion target: %v", err)
			}
			repository := &recordingUserRepository{
				foundUser:   target,
				deleteError: testCase.err,
			}
			service := newUserService(
				repository,
				func() string { return "" },
				func() time.Time { return now })

			err = service.DeleteUser(
				context.Background(),
				testAdministrator(),
				target.ID,
			)
			if !errors.Is(err, testCase.err) {
				t.Fatalf("expected %v, got %v", testCase.err, err)
			}
		})
	}
}

func TestServiceAuditsDeniedUserDeletion(t *testing.T) {
	now := time.Date(2026, time.October, 6, 11, 0, 0, 0, time.UTC)
	target, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"deletion_target",
		"Deletion Target",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create deletion target: %v", err)
	}

	testCases := map[string]struct {
		id             string
		deleteError    error
		expectedError  error
		expectedReason audit.Reason
		expectedTarget string
	}{
		"invalid ID": {
			id:             "not-a-user-id",
			expectedError:  identity.ErrInvalidUserID,
			expectedReason: audit.ReasonInvalidInput,
		},
		"unknown user": {
			id:             target.ID,
			deleteError:    identity.ErrUserNotFound,
			expectedError:  identity.ErrUserNotFound,
			expectedReason: audit.ReasonUnknownTarget,
			expectedTarget: target.ID,
		},
		"last administrator": {
			id:             target.ID,
			deleteError:    identity.ErrLastAdministrator,
			expectedError:  identity.ErrLastAdministrator,
			expectedReason: audit.ReasonLastAdministrator,
			expectedTarget: target.ID,
		},
		"owned media": {
			id:             target.ID,
			deleteError:    identity.ErrUserOwnsMedia,
			expectedError:  identity.ErrUserOwnsMedia,
			expectedReason: audit.ReasonUserOwnsMedia,
			expectedTarget: target.ID,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			repository := &recordingUserRepository{
				foundUser:   target,
				deleteError: testCase.deleteError,
			}
			appender := &recordingAuditAppender{}
			service := users.NewService(
				repository,
				func() string { return "" },
				func() time.Time { return now },
				appender,
				func() string { return "823e4567-e89b-12d3-a456-426614174000" },
			)

			err := service.DeleteUser(
				context.Background(),
				testAdministrator(),
				testCase.id,
			)
			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("expected %v, got %v", testCase.expectedError, err)
			}
			if appender.event.Type != audit.TypeUserDeleted ||
				appender.event.Outcome != audit.OutcomeDenied ||
				appender.event.Reason != testCase.expectedReason ||
				appender.event.TargetID != testCase.expectedTarget {
				t.Fatalf("unexpected denied deletion event: %#v", appender.event)
			}
		})
	}
}

func TestServiceAuditsDeniedSelfDeletion(t *testing.T) {
	now := time.Date(2026, time.October, 6, 11, 30, 0, 0, time.UTC)
	actor := testAdministrator()
	appender := &recordingAuditAppender{}
	service := users.NewService(
		&recordingUserRepository{},
		func() string { return "" },
		func() time.Time { return now },
		appender,
		func() string { return "823e4567-e89b-12d3-a456-426614174000" },
	)

	err := service.DeleteUser(context.Background(), actor, actor.ID)
	if !errors.Is(err, users.ErrSelfDeletion) {
		t.Fatalf("expected ErrSelfDeletion, got %v", err)
	}
	if appender.event.Type != audit.TypeUserDeleted ||
		appender.event.Outcome != audit.OutcomeDenied ||
		appender.event.Reason != audit.ReasonSelfDeletion ||
		appender.event.ActorID != actor.ID ||
		appender.event.TargetID != actor.ID {
		t.Fatalf("unexpected denied self-deletion event: %#v", appender.event)
	}
}

func TestServiceFailsClosedWhenDeletionAuditFails(t *testing.T) {
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	actor := testAdministrator()
	auditError := errors.New("audit storage unavailable")
	service := users.NewService(
		&recordingUserRepository{},
		func() string { return "" },
		func() time.Time { return now },
		&recordingAuditAppender{err: auditError},
		func() string { return "823e4567-e89b-12d3-a456-426614174000" },
	)

	err := service.DeleteUser(context.Background(), actor, actor.ID)
	if !errors.Is(err, auditError) {
		t.Fatalf("expected audit error, got %v", err)
	}
	if errors.Is(err, users.ErrSelfDeletion) {
		t.Fatalf("expected audit failure to replace operation error, got %v", err)
	}
}
