// Package authentication verifies user credentials.
package authentication

import (
	"context"
	"errors"
	"fmt"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// ErrInvalidCredentials is the generic authentication failure.
var ErrInvalidCredentials = errors.New("invalid username or password")

// PasswordVerifier compares a password with an encoded password hash.
type PasswordVerifier interface {
	Verify(
		argPassword []byte,
		argEncodedHash string,
	) (bool, error)
}

// Service verifies user identities and password credentials.
type Service struct {
	users       identity.UserRepository
	credentials credential.PasswordCredentialRepository
	verifier    PasswordVerifier
	dummyHash   string
}

// NewService creates an authentication service.
func NewService(
	argUsers identity.UserRepository,
	argCredentials credential.PasswordCredentialRepository,
	argVerifier PasswordVerifier,
	argDummyHash string,
) *Service {
	return &Service{
		users:       argUsers,
		credentials: argCredentials,
		verifier:    argVerifier,
		dummyHash:   argDummyHash,
	}
}

// Authenticate verifies a username and password without revealing account state.
func (service *Service) Authenticate(
	argContext context.Context,
	argUsername string,
	argPassword []byte,
) (identity.User, error) {
	normalizedUsername, err := identity.NormalizeUsername(argUsername)
	if err != nil {
		return identity.User{}, service.rejectWithDummyVerification(
			argPassword,
		)
	}

	user, err := service.users.FindByUsername(
		argContext,
		normalizedUsername,
	)
	if errors.Is(err, identity.ErrUserNotFound) {
		return identity.User{}, service.rejectWithDummyVerification(
			argPassword,
		)
	}
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve authentication user: %w",
			err,
		)
	}

	passwordCredential, err := service.credentials.FindByUserID(
		argContext,
		user.ID,
	)
	if errors.Is(
		err,
		credential.ErrPasswordCredentialNotFound,
	) {
		return identity.User{}, service.rejectWithDummyVerification(
			argPassword,
		)
	}
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve password credential: %w",
			err,
		)
	}

	matches, err := service.verifier.Verify(
		argPassword,
		passwordCredential.PasswordHash,
	)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"verify password credential: %w",
			err,
		)
	}

	if !matches || !user.Active {
		return identity.User{}, ErrInvalidCredentials
	}

	return user, nil
}

func (service *Service) rejectWithDummyVerification(
	argPassword []byte,
) error {
	if _, err := service.verifier.Verify(
		argPassword,
		service.dummyHash,
	); err != nil {
		return fmt.Errorf(
			"verify dummy password credential: %w",
			err,
		)
	}

	return ErrInvalidCredentials
}
