// Package passwords coordinates password enrollment and credential lifecycle.
package passwords

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

// ErrInvalidEnrollment is the generic failure for unusable enrollment tokens.
var ErrInvalidEnrollment = errors.New("invalid password enrollment")

// UserFinder retrieves the identity receiving an initial credential.
type UserFinder interface {
	FindByID(
		argContext context.Context,
		argID string,
	) (identity.User, error)
}

// EnrollmentTokenGenerator creates a token and its storage hash.
type EnrollmentTokenGenerator interface {
	Generate() (string, [sha256.Size]byte, error)
}

// PasswordHasher creates an encoded password hash.
type PasswordHasher interface {
	Hash(argPassword []byte) (string, error)
}

// Clock returns the current application time.
type Clock func() time.Time

// IssuedEnrollment contains the one-time secret returned to an administrator.
type IssuedEnrollment struct {
	Token     string
	ExpiresAt time.Time
}

// Service coordinates password enrollment operations.
type Service struct {
	users       UserFinder
	enrollments credential.PasswordEnrollmentRepository
	tokens      EnrollmentTokenGenerator
	hasher      PasswordHasher
	currentTime Clock
	lifetime    time.Duration
}

// NewService creates a password enrollment service with explicit dependencies.
func NewService(
	argUsers UserFinder,
	argEnrollments credential.PasswordEnrollmentRepository,
	argTokens EnrollmentTokenGenerator,
	argHasher PasswordHasher,
	argClock Clock,
	argLifetime time.Duration,
) *Service {
	return &Service{
		users:       argUsers,
		enrollments: argEnrollments,
		tokens:      argTokens,
		hasher:      argHasher,
		currentTime: argClock,
		lifetime:    argLifetime,
	}
}

// IssueEnrollment creates or replaces a user's one-time enrollment token.
func (service *Service) IssueEnrollment(
	argContext context.Context,
	argUserID string,
) (IssuedEnrollment, error) {
	user, err := service.users.FindByID(argContext, argUserID)
	if err != nil {
		return IssuedEnrollment{}, fmt.Errorf(
			"retrieve password enrollment user: %w",
			err,
		)
	}

	token, tokenHash, err := service.tokens.Generate()
	if err != nil {
		return IssuedEnrollment{}, fmt.Errorf(
			"generate password enrollment token: %w",
			err,
		)
	}

	enrollment, err := credential.NewPasswordEnrollment(
		user.ID,
		tokenHash,
		service.currentTime(),
		service.lifetime,
	)
	if err != nil {
		return IssuedEnrollment{}, fmt.Errorf(
			"create password enrollment: %w",
			err,
		)
	}

	if err := service.enrollments.SaveForCredentiallessUser(
		argContext,
		enrollment,
	); err != nil {
		return IssuedEnrollment{}, fmt.Errorf(
			"persist password enrollment: %w",
			err,
		)
	}

	return IssuedEnrollment{
		Token:     token,
		ExpiresAt: enrollment.ExpiresAt,
	}, nil
}

// CompleteEnrollment creates an initial credential from a valid one-time token.
func (service *Service) CompleteEnrollment(
	argContext context.Context,
	argToken string,
	argPassword []byte,
) error {
	tokenHash := credential.HashEnrollmentToken(argToken)
	enrollment, err := service.enrollments.FindByTokenHash(
		argContext,
		tokenHash,
	)
	if errors.Is(err, credential.ErrPasswordEnrollmentNotFound) {
		return ErrInvalidEnrollment
	}
	if err != nil {
		return fmt.Errorf("retrieve password enrollment: %w", err)
	}

	currentTime := service.currentTime()
	if !enrollment.IsValidAt(currentTime) {
		return ErrInvalidEnrollment
	}

	if err := password.Validate(argPassword); err != nil {
		return fmt.Errorf("validate enrolled password: %w", err)
	}

	encodedHash, err := service.hasher.Hash(argPassword)
	if err != nil {
		return fmt.Errorf("hash enrolled password: %w", err)
	}

	passwordCredential, err := credential.NewPasswordCredential(
		enrollment.UserID,
		encodedHash,
		currentTime,
	)
	if err != nil {
		return fmt.Errorf("create enrolled password credential: %w", err)
	}

	if err := service.enrollments.CreateCredentialAndConsume(
		argContext,
		tokenHash,
		passwordCredential,
		currentTime,
	); errors.Is(err, credential.ErrPasswordEnrollmentNotFound) ||
		errors.Is(err, credential.ErrPasswordCredentialExists) {
		return ErrInvalidEnrollment
	} else if err != nil {
		return fmt.Errorf("complete password enrollment: %w", err)
	}

	return nil
}
