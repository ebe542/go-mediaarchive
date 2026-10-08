package passwords

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
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
		ctx context.Context,
		userID string,
	) (credential.PasswordCredential, error)

	ChangePasswordAndRevokeSessionsWithAudit(
		ctx context.Context,
		credential credential.PasswordCredential,
		event audit.Event,
	) error
}

// PasswordVerifierHasher verifies and creates encoded password hashes.
type PasswordVerifierHasher interface {
	Verify(password []byte, encodedHash string) (bool, error)
	Hash(password []byte) (string, error)
}

// ChangeService coordinates authenticated password replacements.
type ChangeService struct {
	credentials PasswordChangeRepository
	hasher      PasswordVerifierHasher
	audit       audit.Appender
	eventIDs    EventIDGenerator
	currentTime Clock
}

// NewChangeService creates a password change service with explicit dependencies.
func NewChangeService(
	credentials PasswordChangeRepository,
	hasher PasswordVerifierHasher,
	auditAppender audit.Appender,
	eventIDGenerator EventIDGenerator,
	clock Clock,
) *ChangeService {
	return &ChangeService{
		credentials: credentials,
		hasher:      hasher,
		audit:       auditAppender,
		eventIDs:    eventIDGenerator,
		currentTime: clock,
	}
}

// ChangePassword verifies the old password and atomically invalidates sessions.
func (service *ChangeService) ChangePassword(
	ctx context.Context,
	actor identity.User,
	currentPassword []byte,
	newPassword []byte,
) error {
	now := service.currentTime()
	storedCredential, err := service.credentials.FindByUserID(
		ctx,
		actor.ID,
	)
	if err != nil {
		return fmt.Errorf("retrieve changed password credential: %w", err)
	}

	matches, err := service.hasher.Verify(
		currentPassword,
		storedCredential.PasswordHash,
	)
	if err != nil {
		return fmt.Errorf("verify current password: %w", err)
	}
	if !matches {
		return service.recordPasswordChangeDenial(
			ctx,
			actor,
			audit.ReasonInvalidCredentials,
			now,
			ErrInvalidCurrentPassword,
		)
	}

	if err := password.Validate(newPassword); err != nil {
		return service.recordPasswordChangeDenial(
			ctx,
			actor,
			audit.ReasonInvalidInput,
			now,
			fmt.Errorf("validate new password: %w", err),
		)
	}
	if bytes.Equal(currentPassword, newPassword) {
		return service.recordPasswordChangeDenial(
			ctx,
			actor,
			audit.ReasonResourceConflict,
			now,
			ErrPasswordUnchanged,
		)
	}

	encodedHash, err := service.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	updatedCredential, err := storedCredential.WithPasswordHash(
		encodedHash,
		now,
	)
	if err != nil {
		return fmt.Errorf("update password credential: %w", err)
	}

	event, err := service.newPasswordChangeEvent(
		actor,
		audit.OutcomeSuccess,
		"",
		now,
	)
	if err != nil {
		return err
	}

	if err := service.credentials.ChangePasswordAndRevokeSessionsWithAudit(
		ctx,
		updatedCredential,
		event,
	); err != nil {
		return fmt.Errorf("persist password change: %w", err)
	}

	return nil
}

func (service *ChangeService) recordPasswordChangeDenial(
	ctx context.Context,
	actor identity.User,
	reason audit.Reason,
	now time.Time,
	operationErr error,
) error {
	event, err := service.newPasswordChangeEvent(
		actor,
		audit.OutcomeDenied,
		reason,
		now,
	)
	if err != nil {
		return err
	}
	if err := service.audit.Append(ctx, event); err != nil {
		return fmt.Errorf("record denied password change: %w", err)
	}

	return operationErr
}

func (service *ChangeService) newPasswordChangeEvent(
	actor identity.User,
	outcome audit.Outcome,
	reason audit.Reason,
	now time.Time,
) (audit.Event, error) {
	event, err := audit.NewEvent(audit.Event{
		ID:            service.eventIDs(),
		OccurredAt:    now.UTC(),
		Type:          audit.TypePasswordChanged,
		Outcome:       outcome,
		ActorID:       actor.ID,
		ActorUsername: actor.Username,
		ActorRole:     string(actor.Role),
		TargetType:    audit.TargetUser,
		TargetID:      actor.ID,
		TargetName:    actor.Username,
		Reason:        reason,
	})
	if err != nil {
		return audit.Event{}, fmt.Errorf(
			"create password change audit event: %w",
			err,
		)
	}

	return event, nil
}
