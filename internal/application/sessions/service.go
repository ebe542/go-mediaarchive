// Package sessions coordinates authenticated server-side sessions.
package sessions

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/session"
)

// Authenticator verifies a username and password.
type Authenticator interface {
	Authenticate(
		argContext context.Context,
		argUsername string,
		argPassword []byte,
	) (identity.User, error)
}

// TokenGenerator creates a raw token and its storage hash.
type TokenGenerator interface {
	Generate() (
		string,
		[sha256.Size]byte,
		error,
	)
}

// UserFinder loads the current user state for a session.
type UserFinder interface {
	FindByID(
		argContext context.Context,
		argID string,
	) (identity.User, error)
}

// ErrUnauthenticated is the generic failure for unusable session tokens.
var ErrUnauthenticated = errors.New("authentication required")

// Clock returns the current application time.
type Clock func() time.Time

// Created contains the one-time session token and absolute expiration.
type Created struct {
	AccessToken string
	ExpiresAt   time.Time
}

// Service coordinates session creation, resolution, and revocation.
type Service struct {
	authenticator    Authenticator
	repository       session.Repository
	tokenGenerator   TokenGenerator
	currentTime      Clock
	absoluteLifetime time.Duration
	idleTimeout      time.Duration
	userFinder       UserFinder
}

// NewService creates a server-side session service.
func NewService(
	argAuthenticator Authenticator,
	argUserFinder UserFinder,
	argRepository session.Repository,
	argTokenGenerator TokenGenerator,
	argClock Clock,
	argAbsoluteLifetime time.Duration,
	argIdleTimeout time.Duration,
) *Service {
	return &Service{
		authenticator:    argAuthenticator,
		userFinder:       argUserFinder,
		repository:       argRepository,
		tokenGenerator:   argTokenGenerator,
		currentTime:      argClock,
		absoluteLifetime: argAbsoluteLifetime,
		idleTimeout:      argIdleTimeout,
	}
}

// Create authenticates a user and persists a new server-side session.
func (service *Service) Create(
	argContext context.Context,
	argUsername string,
	argPassword []byte,
) (Created, error) {
	user, err := service.authenticator.Authenticate(
		argContext,
		argUsername,
		argPassword,
	)
	if err != nil {
		return Created{}, fmt.Errorf(
			"authenticate session user: %w",
			err,
		)
	}

	accessToken, tokenHash, err := service.tokenGenerator.Generate()
	if err != nil {
		return Created{}, fmt.Errorf(
			"generate session token: %w",
			err,
		)
	}

	createdSession, err := session.New(
		tokenHash,
		user.ID,
		service.currentTime(),
		service.absoluteLifetime,
	)
	if err != nil {
		return Created{}, fmt.Errorf(
			"create server-side session: %w",
			err,
		)
	}

	if err := service.repository.Create(
		argContext,
		createdSession,
	); err != nil {
		return Created{}, fmt.Errorf(
			"persist server-side session: %w",
			err,
		)
	}

	return Created{
		AccessToken: accessToken,
		ExpiresAt:   createdSession.ExpiresAt,
	}, nil
}

// Resolve authenticates an active session and records recent use.
func (service *Service) Resolve(
	argContext context.Context,
	argAccessToken string,
) (identity.User, error) {
	tokenHash := session.HashToken(argAccessToken)

	storedSession, err := service.repository.FindByTokenHash(
		argContext,
		tokenHash,
	)
	if errors.Is(err, session.ErrNotFound) {
		return identity.User{}, ErrUnauthenticated
	}
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve server-side session: %w",
			err,
		)
	}

	user, err := service.userFinder.FindByID(
		argContext,
		storedSession.UserID,
	)
	if errors.Is(err, identity.ErrUserNotFound) {
		return identity.User{}, ErrUnauthenticated
	}
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve session user: %w",
			err,
		)
	}

	currentTime := service.currentTime().UTC()

	if !storedSession.IsValidAt(
		currentTime,
		service.idleTimeout,
		user.Active,
	) {
		return identity.User{}, ErrUnauthenticated
	}

	if err := service.repository.Touch(
		argContext,
		tokenHash,
		currentTime,
	); errors.Is(err, session.ErrNotFound) {
		return identity.User{}, ErrUnauthenticated
	} else if err != nil {
		return identity.User{}, fmt.Errorf(
			"touch server-side session: %w",
			err,
		)
	}

	return user, nil
}

// Revoke idempotently invalidates a presented session token.
func (service *Service) Revoke(
	argContext context.Context,
	argAccessToken string,
) error {
	tokenHash := session.HashToken(argAccessToken)
	currentTime := service.currentTime().UTC()

	if err := service.repository.Revoke(
		argContext,
		tokenHash,
		currentTime,
	); err != nil {
		return fmt.Errorf(
			"revoke server-side session: %w",
			err,
		)
	}

	return nil
}
