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
	argTokenHash [sha256.Size]byte,
	argUserID string,
	argNow time.Time,
	argLifetime time.Duration,
) (Session, error) {
	if argTokenHash == [sha256.Size]byte{} {
		return Session{}, ErrInvalidTokenHash
	}

	parsedUserID, err := uuid.Parse(argUserID)
	if err != nil ||
		parsedUserID == uuid.Nil ||
		parsedUserID.String() != argUserID {
		return Session{}, fmt.Errorf(
			"%w: expected a canonical lowercase UUID",
			ErrInvalidUserID,
		)
	}

	if argNow.IsZero() {
		return Session{}, fmt.Errorf(
			"%w: creation time must not be zero",
			ErrInvalidTimestamp,
		)
	}

	if argLifetime <= 0 {
		return Session{}, fmt.Errorf(
			"%w: expected a positive duration",
			ErrInvalidLifetime,
		)
	}

	timestamp := argNow.UTC()

	return Session{
		TokenHash:  argTokenHash,
		UserID:     argUserID,
		CreatedAt:  timestamp,
		LastSeenAt: timestamp,
		ExpiresAt:  timestamp.Add(argLifetime),
	}, nil
}

// IsValidAt reports whether a session may authenticate an active user.
func (session Session) IsValidAt(
	argNow time.Time,
	argIdleTimeout time.Duration,
	argUserActive bool,
) bool {
	if argNow.IsZero() ||
		argIdleTimeout <= 0 ||
		!argUserActive ||
		!session.RevokedAt.IsZero() {
		return false
	}

	timestamp := argNow.UTC()

	if timestamp.Before(session.CreatedAt) {
		return false
	}

	if !timestamp.Before(session.ExpiresAt) {
		return false
	}

	idleExpiration := session.LastSeenAt.Add(argIdleTimeout)
	if !timestamp.Before(idleExpiration) {
		return false
	}

	return true
}
