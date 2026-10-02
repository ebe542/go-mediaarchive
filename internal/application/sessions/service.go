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
		ctx context.Context,
		username string,
		password []byte,
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
		ctx context.Context,
		id string,
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
	authenticator Authenticator,
	userFinder UserFinder,
	repository session.Repository,
	tokenGenerator TokenGenerator,
	clock Clock,
	absoluteLifetime time.Duration,
	idleTimeout time.Duration,
) *Service {
	return &Service{
		authenticator:    authenticator,
		userFinder:       userFinder,
		repository:       repository,
		tokenGenerator:   tokenGenerator,
		currentTime:      clock,
		absoluteLifetime: absoluteLifetime,
		idleTimeout:      idleTimeout,
	}
}

// Create authenticates a user and persists a new server-side session.
func (service *Service) Create(
	ctx context.Context,
	username string,
	password []byte,
) (Created, error) {
	user, err := service.authenticator.Authenticate(
		ctx,
		username,
		password,
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
		ctx,
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
	ctx context.Context,
	accessToken string,
) (identity.User, error) {
	tokenHash := session.HashToken(accessToken)

	storedSession, err := service.repository.FindByTokenHash(
		ctx,
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
		ctx,
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
		ctx,
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
	ctx context.Context,
	accessToken string,
) error {
	tokenHash := session.HashToken(accessToken)
	currentTime := service.currentTime().UTC()

	if err := service.repository.Revoke(
		ctx,
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
