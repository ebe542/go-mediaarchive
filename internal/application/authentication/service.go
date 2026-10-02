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
		password []byte,
		encodedHash string,
	) (bool, error)
}

// UserFinder loads identities by normalized username for authentication.
type UserFinder interface {
	FindByUsername(
		ctx context.Context,
		username string,
	) (identity.User, error)
}

// Service verifies user identities and password credentials.
type Service struct {
	users       UserFinder
	credentials credential.PasswordCredentialRepository
	verifier    PasswordVerifier
	dummyHash   string
}

// NewService creates an authentication service.
func NewService(
	users UserFinder,
	credentials credential.PasswordCredentialRepository,
	verifier PasswordVerifier,
	dummyHash string,
) *Service {
	return &Service{
		users:       users,
		credentials: credentials,
		verifier:    verifier,
		dummyHash:   dummyHash,
	}
}

// Authenticate verifies a username and password without revealing account state.
func (service *Service) Authenticate(
	ctx context.Context,
	username string,
	password []byte,
) (identity.User, error) {
	normalizedUsername, err := identity.NormalizeUsername(username)
	if err != nil {
		return identity.User{}, service.rejectWithDummyVerification(
			password,
		)
	}

	user, err := service.users.FindByUsername(
		ctx,
		normalizedUsername,
	)
	if errors.Is(err, identity.ErrUserNotFound) {
		return identity.User{}, service.rejectWithDummyVerification(
			password,
		)
	}
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve authentication user: %w",
			err,
		)
	}

	passwordCredential, err := service.credentials.FindByUserID(
		ctx,
		user.ID,
	)
	if errors.Is(
		err,
		credential.ErrPasswordCredentialNotFound,
	) {
		return identity.User{}, service.rejectWithDummyVerification(
			password,
		)
	}
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve password credential: %w",
			err,
		)
	}

	matches, err := service.verifier.Verify(
		password,
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
	password []byte,
) error {
	if _, err := service.verifier.Verify(
		password,
		service.dummyHash,
	); err != nil {
		return fmt.Errorf(
			"verify dummy password credential: %w",
			err,
		)
	}

	return ErrInvalidCredentials
}
