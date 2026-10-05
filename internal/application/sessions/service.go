// Package sessions coordinates authenticated server-side sessions.
package sessions

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/application/authentication"
	"github.com/ebe542/go-mediaarchive/internal/audit"
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

// Repository stores sessions and atomically couples creation to auditing.
type Repository interface {
	session.Repository

	CreateWithAudit(
		ctx context.Context,
		storedSession session.Session,
		event audit.Event,
	) error
}

// EventIDGenerator creates canonical audit event IDs.
type EventIDGenerator func() string

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
	repository       Repository
	auditAppender    audit.Appender
	eventIDGenerator EventIDGenerator
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
	repository Repository,
	auditAppender audit.Appender,
	eventIDGenerator EventIDGenerator,
	tokenGenerator TokenGenerator,
	clock Clock,
	absoluteLifetime time.Duration,
	idleTimeout time.Duration,
) *Service {
	return &Service{
		authenticator:    authenticator,
		userFinder:       userFinder,
		repository:       repository,
		auditAppender:    auditAppender,
		eventIDGenerator: eventIDGenerator,
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
		if errors.Is(err, authentication.ErrInvalidCredentials) {
			if auditErr := service.recordDeniedCreation(ctx, username); auditErr != nil {
				return Created{}, auditErr
			}
		}
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

	event, err := audit.NewEvent(audit.Event{
		ID:            service.eventIDGenerator(),
		OccurredAt:    createdSession.CreatedAt,
		Type:          audit.TypeSessionCreated,
		Outcome:       audit.OutcomeSuccess,
		ActorID:       user.ID,
		ActorUsername: user.Username,
		ActorRole:     string(user.Role),
		TargetType:    audit.TargetSession,
	})
	if err != nil {
		return Created{}, fmt.Errorf("create session audit event: %w", err)
	}

	if err := service.repository.CreateWithAudit(
		ctx,
		createdSession,
		event,
	); err != nil {
		return Created{}, fmt.Errorf(
			"persist audited server-side session: %w",
			err,
		)
	}

	return Created{
		AccessToken: accessToken,
		ExpiresAt:   createdSession.ExpiresAt,
	}, nil
}

func (service *Service) recordDeniedCreation(
	ctx context.Context,
	username string,
) error {
	normalizedUsername, err := identity.NormalizeUsername(username)
	if err != nil {
		normalizedUsername = ""
	}
	event, err := audit.NewEvent(audit.Event{
		ID:         service.eventIDGenerator(),
		OccurredAt: service.currentTime().UTC(),
		Type:       audit.TypeSessionCreateDenied,
		Outcome:    audit.OutcomeDenied,
		TargetType: audit.TargetUser,
		TargetName: normalizedUsername,
		Reason:     audit.ReasonInvalidCredentials,
	})
	if err != nil {
		return fmt.Errorf("create denied session audit event: %w", err)
	}
	if err := service.auditAppender.Append(ctx, event); err != nil {
		return fmt.Errorf("record denied session creation: %w", err)
	}

	return nil
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
