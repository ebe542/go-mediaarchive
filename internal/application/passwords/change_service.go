package passwords

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

var (
	// ErrInvalidCurrentPassword prevents disclosure of credential details.
	ErrInvalidCurrentPassword = errors.New("invalid current password")
	// ErrPasswordUnchanged rejects replacing a password with the same value.
	ErrPasswordUnchanged = errors.New("new password matches current password")
)

// PasswordChangeRepository atomically updates a credential and its sessions.
type PasswordChangeRepository interface {
	FindByUserID(
		argContext context.Context,
		argUserID string,
	) (credential.PasswordCredential, error)

	ChangePasswordAndRevokeSessions(
		argContext context.Context,
		argCredential credential.PasswordCredential,
	) error
}

// PasswordVerifierHasher verifies and creates encoded password hashes.
type PasswordVerifierHasher interface {
	Verify(argPassword []byte, argEncodedHash string) (bool, error)
	Hash(argPassword []byte) (string, error)
}

// ChangeService coordinates authenticated password replacements.
type ChangeService struct {
	credentials PasswordChangeRepository
	hasher      PasswordVerifierHasher
	currentTime Clock
}

// NewChangeService creates a password change service with explicit dependencies.
func NewChangeService(
	argCredentials PasswordChangeRepository,
	argHasher PasswordVerifierHasher,
	argClock Clock,
) *ChangeService {
	return &ChangeService{
		credentials: argCredentials,
		hasher:      argHasher,
		currentTime: argClock,
	}
}

// ChangePassword verifies the old password and atomically invalidates sessions.
func (service *ChangeService) ChangePassword(
	argContext context.Context,
	argUserID string,
	argCurrentPassword []byte,
	argNewPassword []byte,
) error {
	storedCredential, err := service.credentials.FindByUserID(
		argContext,
		argUserID,
	)
	if err != nil {
		return fmt.Errorf("retrieve changed password credential: %w", err)
	}

	matches, err := service.hasher.Verify(
		argCurrentPassword,
		storedCredential.PasswordHash,
	)
	if err != nil {
		return fmt.Errorf("verify current password: %w", err)
	}
	if !matches {
		return ErrInvalidCurrentPassword
	}

	if err := password.Validate(argNewPassword); err != nil {
		return fmt.Errorf("validate new password: %w", err)
	}
	if bytes.Equal(argCurrentPassword, argNewPassword) {
		return ErrPasswordUnchanged
	}

	encodedHash, err := service.hasher.Hash(argNewPassword)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	updatedCredential, err := storedCredential.WithPasswordHash(
		encodedHash,
		service.currentTime(),
	)
	if err != nil {
		return fmt.Errorf("update password credential: %w", err)
	}

	if err := service.credentials.ChangePasswordAndRevokeSessions(
		argContext,
		updatedCredential,
	); err != nil {
		return fmt.Errorf("persist password change: %w", err)
	}

	return nil
}
