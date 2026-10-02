// Package credential defines authentication credentials independently of users.
package credential

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidUserID indicates that a credential has no canonical user ID.
var ErrInvalidUserID = errors.New("invalid credential user ID")

// ErrInvalidPasswordHash indicates a missing or unsupported password hash.
var ErrInvalidPasswordHash = errors.New("invalid password hash")

// ErrInvalidTimestamp indicates that a credential timestamp is missing.
var ErrInvalidTimestamp = errors.New("invalid credential timestamp")

// PasswordCredential represents a persisted password hash for one user.
type PasswordCredential struct {
	UserID       string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewPasswordCredential validates and creates a password credential.
func NewPasswordCredential(
	userID string,
	passwordHash string,
	now time.Time,
) (PasswordCredential, error) {
	parsedUserID, err := uuid.Parse(userID)
	if err != nil ||
		parsedUserID == uuid.Nil ||
		parsedUserID.String() != userID {
		return PasswordCredential{}, fmt.Errorf(
			"%w: expected a canonical lowercase UUID",
			ErrInvalidUserID,
		)
	}

	if !strings.HasPrefix(passwordHash, "$argon2id$") {
		return PasswordCredential{}, fmt.Errorf(
			"%w: expected an Argon2id encoding",
			ErrInvalidPasswordHash,
		)
	}

	if now.IsZero() {
		return PasswordCredential{}, fmt.Errorf(
			"%w: creation time must not be zero",
			ErrInvalidTimestamp,
		)
	}

	timestamp := now.UTC()

	return PasswordCredential{
		UserID:       userID,
		PasswordHash: passwordHash,
		CreatedAt:    timestamp,
		UpdatedAt:    timestamp,
	}, nil
}

// WithPasswordHash returns a credential with a validated replacement hash.
func (credential PasswordCredential) WithPasswordHash(
	passwordHash string,
	now time.Time,
) (PasswordCredential, error) {
	if !strings.HasPrefix(passwordHash, "$argon2id$") {
		return PasswordCredential{}, fmt.Errorf(
			"%w: expected an Argon2id encoding",
			ErrInvalidPasswordHash,
		)
	}

	if credential.CreatedAt.IsZero() ||
		now.IsZero() ||
		now.UTC().Before(credential.CreatedAt) {
		return PasswordCredential{}, fmt.Errorf(
			"%w: update time must not precede creation time",
			ErrInvalidTimestamp,
		)
	}

	credential.PasswordHash = passwordHash
	credential.UpdatedAt = now.UTC()

	return credential, nil
}
