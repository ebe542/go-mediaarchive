package session

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidTokenHash indicates a missing session token hash.
var ErrInvalidTokenHash = errors.New("invalid session token hash")

// ErrInvalidUserID indicates a non-canonical session user ID.
var ErrInvalidUserID = errors.New("invalid session user ID")

// ErrInvalidTimestamp indicates a missing session timestamp.
var ErrInvalidTimestamp = errors.New("invalid session timestamp")

// ErrInvalidLifetime indicates a non-positive absolute session lifetime.
var ErrInvalidLifetime = errors.New("invalid session lifetime")

// Session represents server-side authentication state.
type Session struct {
	TokenHash  [sha256.Size]byte
	UserID     string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	RevokedAt  time.Time
}

// New validates and creates an active server-side session.
func New(
	tokenHash [sha256.Size]byte,
	userID string,
	now time.Time,
	lifetime time.Duration,
) (Session, error) {
	if tokenHash == [sha256.Size]byte{} {
		return Session{}, ErrInvalidTokenHash
	}

	parsedUserID, err := uuid.Parse(userID)
	if err != nil ||
		parsedUserID == uuid.Nil ||
		parsedUserID.String() != userID {
		return Session{}, fmt.Errorf(
			"%w: expected a canonical lowercase UUID",
			ErrInvalidUserID,
		)
	}

	if now.IsZero() {
		return Session{}, fmt.Errorf(
			"%w: creation time must not be zero",
			ErrInvalidTimestamp,
		)
	}

	if lifetime <= 0 {
		return Session{}, fmt.Errorf(
			"%w: expected a positive duration",
			ErrInvalidLifetime,
		)
	}

	timestamp := now.UTC()

	return Session{
		TokenHash:  tokenHash,
		UserID:     userID,
		CreatedAt:  timestamp,
		LastSeenAt: timestamp,
		ExpiresAt:  timestamp.Add(lifetime),
	}, nil
}

// IsValidAt reports whether a session may authenticate an active user.
func (session Session) IsValidAt(
	now time.Time,
	idleTimeout time.Duration,
	userActive bool,
) bool {
	if now.IsZero() ||
		idleTimeout <= 0 ||
		!userActive ||
		!session.RevokedAt.IsZero() {
		return false
	}

	timestamp := now.UTC()

	if timestamp.Before(session.CreatedAt) {
		return false
	}

	if !timestamp.Before(session.ExpiresAt) {
		return false
	}

	idleExpiration := session.LastSeenAt.Add(idleTimeout)
	if !timestamp.Before(idleExpiration) {
		return false
	}

	return true
}
