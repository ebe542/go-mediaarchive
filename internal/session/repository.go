package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"
)

// ErrNotFound indicates that no session exists for a token hash.
var ErrNotFound = errors.New("session not found")

// Repository stores and retrieves server-side sessions.
type Repository interface {
	Create(
		argContext context.Context,
		argSession Session,
	) error

	FindByTokenHash(
		argContext context.Context,
		argTokenHash [sha256.Size]byte,
	) (Session, error)

	Touch(
		argContext context.Context,
		argTokenHash [sha256.Size]byte,
		argNow time.Time,
	) error

	Revoke(
		argContext context.Context,
		argTokenHash [sha256.Size]byte,
		argNow time.Time,
	) error
}
