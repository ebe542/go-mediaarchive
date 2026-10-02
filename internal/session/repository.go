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
		ctx context.Context,
		session Session,
	) error

	FindByTokenHash(
		ctx context.Context,
		tokenHash [sha256.Size]byte,
	) (Session, error)

	Touch(
		ctx context.Context,
		tokenHash [sha256.Size]byte,
		now time.Time,
	) error

	Revoke(
		ctx context.Context,
		tokenHash [sha256.Size]byte,
		now time.Time,
	) error
}
