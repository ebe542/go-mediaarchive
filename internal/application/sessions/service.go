// Package sessions coordinates authenticated server-side sessions.
package sessions

import (
	"context"
	"crypto/sha256"
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
}

// NewService creates a server-side session service.
func NewService(
	argAuthenticator Authenticator,
	argRepository session.Repository,
	argTokenGenerator TokenGenerator,
	argClock Clock,
	argAbsoluteLifetime time.Duration,
	argIdleTimeout time.Duration,
) *Service {
	return &Service{
		authenticator:    argAuthenticator,
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
