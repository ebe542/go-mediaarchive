package credential

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidEnrollmentTokenHash indicates a missing enrollment token hash.
var ErrInvalidEnrollmentTokenHash = errors.New(
	"invalid password enrollment token hash",
)

// ErrInvalidEnrollmentUserID indicates a non-canonical enrollment user ID.
var ErrInvalidEnrollmentUserID = errors.New(
	"invalid password enrollment user ID",
)

// ErrInvalidEnrollmentTimestamp indicates a missing enrollment timestamp.
var ErrInvalidEnrollmentTimestamp = errors.New(
	"invalid password enrollment timestamp",
)

// ErrInvalidEnrollmentLifetime indicates a non-positive enrollment lifetime.
var ErrInvalidEnrollmentLifetime = errors.New(
	"invalid password enrollment lifetime",
)

// PasswordEnrollment represents a stored one-time password setup token.
type PasswordEnrollment struct {
	UserID    string
	TokenHash [sha256.Size]byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewPasswordEnrollment validates and creates a password enrollment.
func NewPasswordEnrollment(
	argUserID string,
	argTokenHash [sha256.Size]byte,
	argNow time.Time,
	argLifetime time.Duration,
) (PasswordEnrollment, error) {
	parsedUserID, err := uuid.Parse(argUserID)
	if err != nil ||
		parsedUserID == uuid.Nil ||
		parsedUserID.String() != argUserID {
		return PasswordEnrollment{}, fmt.Errorf(
			"%w: expected a canonical lowercase UUID",
			ErrInvalidEnrollmentUserID,
		)
	}

	if argTokenHash == [sha256.Size]byte{} {
		return PasswordEnrollment{}, ErrInvalidEnrollmentTokenHash
	}

	if argNow.IsZero() {
		return PasswordEnrollment{}, fmt.Errorf(
			"%w: creation time must not be zero",
			ErrInvalidEnrollmentTimestamp,
		)
	}

	if argLifetime <= 0 {
		return PasswordEnrollment{}, fmt.Errorf(
			"%w: expected a positive duration",
			ErrInvalidEnrollmentLifetime,
		)
	}

	timestamp := argNow.UTC()

	return PasswordEnrollment{
		UserID:    argUserID,
		TokenHash: argTokenHash,
		CreatedAt: timestamp,
		ExpiresAt: timestamp.Add(argLifetime),
	}, nil
}

// IsValidAt reports whether the enrollment may create a credential.
func (enrollment PasswordEnrollment) IsValidAt(argNow time.Time) bool {
	if argNow.IsZero() {
		return false
	}

	timestamp := argNow.UTC()

	return !timestamp.Before(enrollment.CreatedAt) &&
		timestamp.Before(enrollment.ExpiresAt)
}
