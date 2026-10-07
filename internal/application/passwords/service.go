// Package passwords coordinates password enrollment and credential lifecycle.
package passwords

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

// ErrInvalidEnrollment is the generic failure for unusable enrollment tokens.
var ErrInvalidEnrollment = errors.New("invalid password enrollment")

// UserFinder retrieves the identity receiving an initial credential.
type UserFinder interface {
	FindByID(
		ctx context.Context,
		id string,
	) (identity.User, error)
}

// EnrollmentTokenGenerator creates a token and its storage hash.
type EnrollmentTokenGenerator interface {
	Generate() (string, [sha256.Size]byte, error)
}

// PasswordHasher creates an encoded password hash.
type PasswordHasher interface {
	Hash(password []byte) (string, error)
}

// Clock returns the current application time.
type Clock func() time.Time

// EventIDGenerator creates canonical audit event identifiers.
type EventIDGenerator func() string

// EnrollmentStore persists enrollment state and required audit events.
type EnrollmentStore interface {
	credential.PasswordEnrollmentRepository

	SaveForCredentiallessUserWithAudit(
		ctx context.Context,
		enrollment credential.PasswordEnrollment,
		event audit.Event,
	) error
}

// IssuedEnrollment contains the one-time secret returned to an administrator.
type IssuedEnrollment struct {
	Token     string
	ExpiresAt time.Time
}

// Service coordinates password enrollment operations.
type Service struct {
	users       UserFinder
	enrollments EnrollmentStore
	tokens      EnrollmentTokenGenerator
	hasher      PasswordHasher
	audit       audit.Appender
	eventIDs    EventIDGenerator
	currentTime Clock
	lifetime    time.Duration
}

// NewService creates a password enrollment service with explicit dependencies.
func NewService(
	users UserFinder,
	enrollments EnrollmentStore,
	tokens EnrollmentTokenGenerator,
	hasher PasswordHasher,
	clock Clock,
	lifetime time.Duration,
	auditAppender audit.Appender,
	eventIDGenerator EventIDGenerator,
) *Service {
	return &Service{
		users:       users,
		enrollments: enrollments,
		tokens:      tokens,
		hasher:      hasher,
		audit:       auditAppender,
		eventIDs:    eventIDGenerator,
		currentTime: clock,
		lifetime:    lifetime,
	}
}

// IssueEnrollment creates or replaces a user's one-time enrollment token.
func (service *Service) IssueEnrollment(
	ctx context.Context,
	actor identity.User,
	userID string,
) (IssuedEnrollment, error) {
	now := service.currentTime()
	if err := identity.ValidateUserID(userID); err != nil {
		return IssuedEnrollment{}, service.recordEnrollmentIssueDenial(
			ctx,
			actor,
			safeEnrollmentTarget(userID),
			audit.ReasonInvalidInput,
			now,
			err,
		)
	}
	user, err := service.users.FindByID(ctx, userID)
	if err != nil {
		operationErr := fmt.Errorf(
			"retrieve password enrollment user: %w",
			err,
		)
		reason := enrollmentIssueDenialReason(err)
		if reason != "" {
			return IssuedEnrollment{}, service.recordEnrollmentIssueDenial(
				ctx,
				actor,
				safeEnrollmentTarget(userID),
				reason,
				now,
				operationErr,
			)
		}

		return IssuedEnrollment{}, operationErr
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
		now,
		service.lifetime,
	)
	if err != nil {
		return IssuedEnrollment{}, fmt.Errorf(
			"create password enrollment: %w",
			err,
		)
	}

	event, err := service.newEnrollmentIssueEvent(
		actor,
		user,
		audit.OutcomeSuccess,
		"",
		now,
	)
	if err != nil {
		return IssuedEnrollment{}, err
	}
	if err := service.enrollments.SaveForCredentiallessUserWithAudit(
		ctx,
		enrollment,
		event,
	); err != nil {
		operationErr := fmt.Errorf(
			"persist password enrollment: %w",
			err,
		)
		if errors.Is(err, credential.ErrPasswordCredentialExists) {
			return IssuedEnrollment{}, service.recordEnrollmentIssueDenial(
				ctx,
				actor,
				user,
				audit.ReasonResourceConflict,
				now,
				operationErr,
			)
		}

		return IssuedEnrollment{}, operationErr
	}

	return IssuedEnrollment{
		Token:     token,
		ExpiresAt: enrollment.ExpiresAt,
	}, nil
}

func (service *Service) recordEnrollmentIssueDenial(
	ctx context.Context,
	actor identity.User,
	target identity.User,
	reason audit.Reason,
	now time.Time,
	operationErr error,
) error {
	event, err := service.newEnrollmentIssueEvent(
		actor,
		target,
		audit.OutcomeDenied,
		reason,
		now,
	)
	if err != nil {
		return err
	}
	if err := service.audit.Append(ctx, event); err != nil {
		return fmt.Errorf("record denied password enrollment issue: %w", err)
	}

	return operationErr
}

func (service *Service) newEnrollmentIssueEvent(
	actor identity.User,
	target identity.User,
	outcome audit.Outcome,
	reason audit.Reason,
	now time.Time,
) (audit.Event, error) {
	event, err := audit.NewEvent(audit.Event{
		ID:            service.eventIDs(),
		OccurredAt:    now.UTC(),
		Type:          audit.TypePasswordEnrollmentIssued,
		Outcome:       outcome,
		ActorID:       actor.ID,
		ActorUsername: actor.Username,
		ActorRole:     string(actor.Role),
		TargetType:    audit.TargetUser,
		TargetID:      target.ID,
		TargetName:    target.Username,
		Reason:        reason,
	})
	if err != nil {
		return audit.Event{}, fmt.Errorf(
			"create password enrollment issue audit event: %w",
			err,
		)
	}

	return event, nil
}

func safeEnrollmentTarget(userID string) identity.User {
	if identity.ValidateUserID(userID) != nil {
		return identity.User{}
	}

	return identity.User{ID: userID}
}

func enrollmentIssueDenialReason(inputError error) audit.Reason {
	switch {
	case errors.Is(inputError, identity.ErrInvalidUserID):
		return audit.ReasonInvalidInput
	case errors.Is(inputError, identity.ErrUserNotFound):
		return audit.ReasonUnknownTarget
	default:
		return ""
	}
}

// CompleteEnrollment creates an initial credential from a valid one-time token.
func (service *Service) CompleteEnrollment(
	ctx context.Context,
	token string,
	passwordValue []byte,
) error {
	tokenHash := credential.HashEnrollmentToken(token)
	enrollment, err := service.enrollments.FindByTokenHash(
		ctx,
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

	if err := password.Validate(passwordValue); err != nil {
		return fmt.Errorf("validate enrolled password: %w", err)
	}

	encodedHash, err := service.hasher.Hash(passwordValue)
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
		ctx,
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
