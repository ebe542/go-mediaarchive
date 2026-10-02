// Package users coordinates user identity application operations.
package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// ErrSelfLockout indicates that an administrator tried to remove their own
// administrative access.
var ErrSelfLockout = errors.New("administrator cannot remove own access")

// ErrSelfDeletion indicates that an administrator tried to delete their own
// identity.
var ErrSelfDeletion = errors.New("administrator cannot delete own identity")

// IDGenerator creates stable user identifiers.
type IDGenerator func() string

// Clock returns the current application time.
type Clock func() time.Time

// CreateUserInput contains caller-controlled values for a new user.
type CreateUserInput struct {
	Username    string
	DisplayName string
	Role        identity.Role
}

// UpdateUserInput contains caller-controlled mutable user details.
type UpdateUserInput struct {
	Username    string
	DisplayName string
	Role        identity.Role
}

// Service coordinates user identity use cases.
type Service struct {
	repository  Repository
	generateID  IDGenerator
	currentTime Clock
}

// NewService creates a user application service.
func NewService(
	repository Repository,
	idGenerator IDGenerator,
	clock Clock,
) *Service {
	return &Service{
		repository:  repository,
		generateID:  idGenerator,
		currentTime: clock,
	}
}

// CreateUser validates and persists a new user identity.
func (service *Service) CreateUser(
	ctx context.Context,
	input CreateUserInput,
) (identity.User, error) {
	user, err := identity.NewUser(
		service.generateID(),
		input.Username,
		input.DisplayName,
		input.Role,
		service.currentTime(),
	)
	if err != nil {
		return identity.User{}, fmt.Errorf("create user identity: %w", err)
	}

	if err := service.repository.Create(ctx, user); err != nil {
		return identity.User{}, fmt.Errorf("persist user identity: %w", err)
	}

	return user, nil
}

// UserByID retrieves a user identity by its stable ID.
func (service *Service) UserByID(
	ctx context.Context,
	id string,
) (identity.User, error) {
	user, err := service.repository.FindByID(ctx, id)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve user by ID: %w",
			err,
		)
	}

	return user, nil
}

// UserByUsername retrieves a user by its normalized username.
func (service *Service) UserByUsername(
	ctx context.Context,
	username string,
) (identity.User, error) {
	normalizedUsername, err := identity.NormalizeUsername(username)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"normalize username: %w",
			err,
		)
	}

	user, err := service.repository.FindByUsername(
		ctx,
		normalizedUsername,
	)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve user by username: %w",
			err,
		)
	}

	return user, nil
}

// UpdateUser validates and persists mutable user details.
func (service *Service) UpdateUser(
	ctx context.Context,
	actorID string,
	id string,
	input UpdateUserInput,
) (identity.User, error) {
	if err := identity.ValidateUserID(id); err != nil {
		return identity.User{}, err
	}

	existingUser, err := service.repository.FindByID(ctx, id)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve user for update: %w",
			err,
		)
	}

	updatedUser, err := existingUser.UpdateDetails(
		input.Username,
		input.DisplayName,
		input.Role,
		service.currentTime(),
	)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"update user identity: %w",
			err,
		)
	}

	if actorID == existingUser.ID &&
		existingUser.Role == identity.RoleAdmin &&
		updatedUser.Role != identity.RoleAdmin {
		return identity.User{}, ErrSelfLockout
	}

	if err := service.repository.UpdatePreservingLastAdministrator(
		ctx,
		updatedUser,
	); err != nil {
		return identity.User{}, fmt.Errorf(
			"persist updated user identity: %w",
			err,
		)
	}

	return updatedUser, nil
}

// SetUserActive changes and persists a user's activation state.
func (service *Service) SetUserActive(
	ctx context.Context,
	actorID string,
	id string,
	active bool,
) (identity.User, error) {
	if err := identity.ValidateUserID(id); err != nil {
		return identity.User{}, err
	}

	existingUser, err := service.repository.FindByID(ctx, id)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve user for activation update: %w",
			err,
		)
	}

	if actorID == existingUser.ID &&
		existingUser.Role == identity.RoleAdmin &&
		!active {
		return identity.User{}, ErrSelfLockout
	}

	if existingUser.Active == active {
		return existingUser, nil
	}

	updatedUser, err := existingUser.SetActive(
		active,
		service.currentTime(),
	)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"set user activation state: %w",
			err,
		)
	}

	if err := service.repository.UpdatePreservingLastAdministrator(
		ctx,
		updatedUser,
	); err != nil {
		return identity.User{}, fmt.Errorf(
			"persist user activation state: %w",
			err,
		)
	}

	return updatedUser, nil
}

// DeleteUser permanently removes another user and their authentication data.
func (service *Service) DeleteUser(
	ctx context.Context,
	actorID string,
	id string,
) error {
	if err := identity.ValidateUserID(id); err != nil {
		return err
	}
	if actorID == id {
		return ErrSelfDeletion
	}

	if err := service.repository.DeletePreservingLastAdministrator(
		ctx,
		id,
	); err != nil {
		return fmt.Errorf("delete user identity: %w", err)
	}

	return nil
}
