// Package users coordinates user identity application operations.
package users

import (
	"context"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

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
	repository  identity.UserRepository
	generateID  IDGenerator
	currentTime Clock
}

// NewService creates a user application service.
func NewService(
	argRepository identity.UserRepository,
	argIDGenerator IDGenerator,
	argClock Clock,
) *Service {
	return &Service{
		repository:  argRepository,
		generateID:  argIDGenerator,
		currentTime: argClock,
	}
}

// CreateUser validates and persists a new user identity.
func (service *Service) CreateUser(
	argContext context.Context,
	argInput CreateUserInput,
) (identity.User, error) {
	user, err := identity.NewUser(
		service.generateID(),
		argInput.Username,
		argInput.DisplayName,
		argInput.Role,
		service.currentTime(),
	)
	if err != nil {
		return identity.User{}, fmt.Errorf("create user identity: %w", err)
	}

	if err := service.repository.Create(argContext, user); err != nil {
		return identity.User{}, fmt.Errorf("persist user identity: %w", err)
	}

	return user, nil
}

// UserByID retrieves a user identity by its stable ID.
func (service *Service) UserByID(
	argContext context.Context,
	argID string,
) (identity.User, error) {
	user, err := service.repository.FindByID(argContext, argID)
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
	argContext context.Context,
	argUsername string,
) (identity.User, error) {
	normalizedUsername, err := identity.NormalizeUsername(argUsername)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"normalize username: %w",
			err,
		)
	}

	user, err := service.repository.FindByUsername(
		argContext,
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
	argContext context.Context,
	argID string,
	argInput UpdateUserInput,
) (identity.User, error) {
	existingUser, err := service.repository.FindByID(argContext, argID)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve user for update: %w",
			err,
		)
	}

	updatedUser, err := existingUser.UpdateDetails(
		argInput.Username,
		argInput.DisplayName,
		argInput.Role,
		service.currentTime(),
	)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"update user identity: %w",
			err,
		)
	}

	if err := service.repository.Update(argContext, updatedUser); err != nil {
		return identity.User{}, fmt.Errorf(
			"persist updated user identity: %w",
			err,
		)
	}

	return updatedUser, nil
}

// SetUserActive changes and persists a user's activation state.
func (service *Service) SetUserActive(
	argContext context.Context,
	argID string,
	argActive bool,
) (identity.User, error) {
	existingUser, err := service.repository.FindByID(argContext, argID)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve user for activation update: %w",
			err,
		)
	}

	updatedUser, err := existingUser.SetActive(
		argActive,
		service.currentTime(),
	)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"set user activation state: %w",
			err,
		)
	}

	if err := service.repository.Update(argContext, updatedUser); err != nil {
		return identity.User{}, fmt.Errorf(
			"persist user activation state: %w",
			err,
		)
	}

	return updatedUser, nil
}
