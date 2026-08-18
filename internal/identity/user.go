// Package identity defines users and global security roles.
package identity

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Role represents a user's global security role.
type Role string

const (
	// RoleViewer permits access to explicitly allowed metadata.
	RoleViewer Role = "viewer"

	// RoleEditor permits creation and management of allowed media records.
	RoleEditor Role = "editor"

	// RoleAdmin permits identity, policy, and audit administration.
	RoleAdmin Role = "admin"
)

// Valid reports whether the role is supported by the application.
func (role Role) Valid() bool {
	switch role {
	case RoleViewer, RoleEditor, RoleAdmin:
		return true
	default:
		return false
	}
}

const (
	minimumUsernameLength    = 3
	maximumUsernameLength    = 32
	maximumDisplayNameLength = 100
)

// ErrInvalidUsername indicates that a username violates the domain rules.
var ErrInvalidUsername = errors.New("invalid username")

// ErrInvalidUserID indicates that a user ID is not a canonical UUID.
var ErrInvalidUserID = errors.New("invalid user ID")

// ErrInvalidDisplayName indicates that a display name violates the domain rules.
var ErrInvalidDisplayName = errors.New("invalid display name")

// ErrInvalidRole indicates that a role is not supported.
var ErrInvalidRole = errors.New("invalid role")

// ErrInvalidTimestamp indicates that a required timestamp is missing.
var ErrInvalidTimestamp = errors.New("invalid timestamp")

// NormalizeUsername trims, lowercases, and validates a username.
func NormalizeUsername(username string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(username))

	if len(normalized) < minimumUsernameLength ||
		len(normalized) > maximumUsernameLength {
		return "", fmt.Errorf(
			"%w: length must be between %d and %d characters",
			ErrInvalidUsername,
			minimumUsernameLength,
			maximumUsernameLength,
		)
	}

	if !isASCIIAlphanumeric(normalized[0]) {
		return "", fmt.Errorf(
			"%w: first character must be an ASCII letter or digit",
			ErrInvalidUsername,
		)
	}

	for index := 1; index < len(normalized); index++ {
		character := normalized[index]

		if isASCIIAlphanumeric(character) ||
			character == '-' ||
			character == '_' {
			continue
		}

		return "", fmt.Errorf(
			"%w: character at position %d is not allowed",
			ErrInvalidUsername,
			index+1,
		)
	}

	return normalized, nil
}

func isASCIIAlphanumeric(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9'
}

// User represents a persistent user identity.
type User struct {
	ID          string
	Username    string
	DisplayName string
	Role        Role
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewUser validates and creates an active user identity.
func NewUser(
	id string,
	username string,
	displayName string,
	role Role,
	now time.Time,
) (User, error) {
	parsedID, err := uuid.Parse(id)
	if err != nil ||
		parsedID == uuid.Nil ||
		parsedID.String() != id {
		return User{}, fmt.Errorf(
			"%w: expected a canonical lowercase UUID",
			ErrInvalidUserID,
		)
	}

	normalizedUsername, err := NormalizeUsername(username)
	if err != nil {
		return User{}, err
	}

	normalizedDisplayName := strings.TrimSpace(displayName)
	displayNameLength := utf8.RuneCountInString(normalizedDisplayName)

	if displayNameLength == 0 ||
		displayNameLength > maximumDisplayNameLength {
		return User{}, fmt.Errorf(
			"%w: length must be between 1 and %d characters",
			ErrInvalidDisplayName,
			maximumDisplayNameLength,
		)
	}

	if !role.Valid() {
		return User{}, fmt.Errorf(
			"%w: %q",
			ErrInvalidRole,
			role,
		)
	}

	if now.IsZero() {
		return User{}, fmt.Errorf(
			"%w: creation time must not be zero",
			ErrInvalidTimestamp,
		)
	}

	timestamp := now.UTC()

	return User{
		ID:          id,
		Username:    normalizedUsername,
		DisplayName: normalizedDisplayName,
		Role:        role,
		Active:      true,
		CreatedAt:   timestamp,
		UpdatedAt:   timestamp,
	}, nil
}
