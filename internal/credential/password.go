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
	argUserID string,
	argPasswordHash string,
	argNow time.Time,
) (PasswordCredential, error) {
	parsedUserID, err := uuid.Parse(argUserID)
	if err != nil ||
		parsedUserID == uuid.Nil ||
		parsedUserID.String() != argUserID {
		return PasswordCredential{}, fmt.Errorf(
			"%w: expected a canonical lowercase UUID",
			ErrInvalidUserID,
		)
	}

	if !strings.HasPrefix(argPasswordHash, "$argon2id$") {
		return PasswordCredential{}, fmt.Errorf(
			"%w: expected an Argon2id encoding",
			ErrInvalidPasswordHash,
		)
	}

	if argNow.IsZero() {
		return PasswordCredential{}, fmt.Errorf(
			"%w: creation time must not be zero",
			ErrInvalidTimestamp,
		)
	}

	timestamp := argNow.UTC()

	return PasswordCredential{
		UserID:       argUserID,
		PasswordHash: argPasswordHash,
		CreatedAt:    timestamp,
		UpdatedAt:    timestamp,
	}, nil
}
