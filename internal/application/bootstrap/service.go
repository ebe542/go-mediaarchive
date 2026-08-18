// Package bootstrap coordinates initial administrator creation.
package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

// PasswordHasher creates an encoded password hash.
type PasswordHasher interface {
	Hash(argPassword []byte) (string, error)
}

// IDGenerator creates a stable user identifier.
type IDGenerator func() string

// Clock returns the current application time.
type Clock func() time.Time

// Input contains values required for initial administrator creation.
type Input struct {
	Username    string
	DisplayName string
	Password    []byte
}

// Service coordinates initial administrator and credential creation.
type Service struct {
	bootstrapper credential.AdminBootstrapper
	hasher       PasswordHasher
	generateID   IDGenerator
	currentTime  Clock
}

// NewService creates an administrator bootstrap service.
func NewService(
	argBootstrapper credential.AdminBootstrapper,
	argHasher PasswordHasher,
	argIDGenerator IDGenerator,
	argClock Clock,
) *Service {
	return &Service{
		bootstrapper: argBootstrapper,
		hasher:       argHasher,
		generateID:   argIDGenerator,
		currentTime:  argClock,
	}
}

// BootstrapAdmin validates and atomically stores the first administrator.
func (service *Service) BootstrapAdmin(
	argContext context.Context,
	argInput Input,
) (identity.User, error) {
	currentTime := service.currentTime()

	adminUser, err := identity.NewUser(
		service.generateID(),
		argInput.Username,
		argInput.DisplayName,
		identity.RoleAdmin,
		currentTime,
	)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"create bootstrap administrator: %w",
			err,
		)
	}

	if err := password.Validate(argInput.Password); err != nil {
		return identity.User{}, fmt.Errorf(
			"validate bootstrap password: %w",
			err,
		)
	}

	encodedHash, err := service.hasher.Hash(argInput.Password)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"hash bootstrap password: %w",
			err,
		)
	}

	passwordCredential, err := credential.NewPasswordCredential(
		adminUser.ID,
		encodedHash,
		currentTime,
	)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"create bootstrap credential: %w",
			err,
		)
	}

	if err := service.bootstrapper.BootstrapAdmin(
		argContext,
		adminUser,
		passwordCredential,
	); err != nil {
		return identity.User{}, fmt.Errorf(
			"persist bootstrap administrator: %w",
			err,
		)
	}

	return adminUser, nil
}
