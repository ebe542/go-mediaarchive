// Package users coordinates user identity application operations.
package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// ErrSelfLockout indicates that an administrator tried to remove their own
// administrative access.
var ErrSelfLockout = errors.New("administrator cannot remove own access")

// ErrSelfDeletion indicates that an administrator tried to delete their own
// identity.
var ErrSelfDeletion = errors.New("administrator cannot delete own identity")

// IDGenerator creates stable user identifiers.
type IDGenerator func() string

// EventIDGenerator creates canonical audit event identifiers.
type EventIDGenerator func() string

// Clock returns the current application time.
type Clock func() time.Time

// CreateUserInput contains caller-controlled values for a new user.
type CreateUserInput struct {
	Username    string
	DisplayName string
	Role        identity.Role
}

// UpdateUserInput contains caller-controlled mutable user details.
type UpdateUserInput struct {
	Username    string
	DisplayName string
	Role        identity.Role
}

// Service coordinates user identity use cases.
type Service struct {
	repository      Repository
	audit           audit.Appender
	generateID      IDGenerator
	generateEventID EventIDGenerator
	currentTime     Clock
}

// NewService creates a user application service.
func NewService(
	repository Repository,
	idGenerator IDGenerator,
	clock Clock,
	auditAppender audit.Appender,
	eventIDGenerator EventIDGenerator,
) *Service {
	return &Service{
		repository:      repository,
		audit:           auditAppender,
		generateID:      idGenerator,
		generateEventID: eventIDGenerator,
		currentTime:     clock,
	}
}

// CreateUser validates and persists a new user identity.
func (service *Service) CreateUser(
	ctx context.Context,
	actor identity.User,
	input CreateUserInput,
) (identity.User, error) {
	userID := service.generateID()
	now := service.currentTime()
	user, err := identity.NewUser(
		userID,
		input.Username,
		input.DisplayName,
		input.Role,
		now,
	)
	if err != nil {
		operationErr := fmt.Errorf("create user identity: %w", err)
		return identity.User{}, service.recordDeniedChange(
			ctx,
			actor,
			audit.TypeUserCreated,
			safeUserTarget(userID, input.Username),
			audit.ReasonInvalidInput,
			now,
			operationErr,
		)
	}

	event, err := service.newUserEvent(
		actor,
		user,
		audit.TypeUserCreated,
		audit.OutcomeSuccess,
		"",
		user.CreatedAt,
	)
	if err != nil {
		return identity.User{}, err
	}
	if err := service.repository.CreateWithAudit(ctx, user, event); err != nil {
		operationErr := fmt.Errorf("persist user identity: %w", err)
		if errors.Is(err, identity.ErrUserConflict) {
			return identity.User{}, service.recordDeniedChange(
				ctx,
				actor,
				audit.TypeUserCreated,
				user,
				audit.ReasonResourceConflict,
				now,
				operationErr,
			)
		}

		return identity.User{}, operationErr
	}

	return user, nil
}

// UserByID retrieves a user identity by its stable ID.
func (service *Service) UserByID(
	ctx context.Context,
	id string,
) (identity.User, error) {
	user, err := service.repository.FindByID(ctx, id)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve user by ID: %w",
			err,
		)
	}

	return user, nil
}

// UserByUsername retrieves a user by its normalized username.
func (service *Service) UserByUsername(
	ctx context.Context,
	username string,
) (identity.User, error) {
	normalizedUsername, err := identity.NormalizeUsername(username)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"normalize username: %w",
			err,
		)
	}

	user, err := service.repository.FindByUsername(
		ctx,
		normalizedUsername,
	)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"retrieve user by username: %w",
			err,
		)
	}

	return user, nil
}

// UpdateUser validates and persists mutable user details.
func (service *Service) UpdateUser(
	ctx context.Context,
	actor identity.User,
	id string,
	input UpdateUserInput,
) (identity.User, error) {
	now := service.currentTime()
	if err := identity.ValidateUserID(id); err != nil {
		return identity.User{}, service.recordDeniedChange(
			ctx,
			actor,
			audit.TypeUserUpdated,
			safeUserTarget(id, input.Username),
			audit.ReasonInvalidInput,
			now,
			err,
		)
	}

	existingUser, err := service.repository.FindByID(ctx, id)
	if err != nil {
		operationErr := fmt.Errorf(
			"retrieve user for update: %w",
			err,
		)
		if errors.Is(err, identity.ErrUserNotFound) {
			return identity.User{}, service.recordDeniedChange(
				ctx,
				actor,
				audit.TypeUserUpdated,
				safeUserTarget(id, input.Username),
				audit.ReasonUnknownTarget,
				now,
				operationErr,
			)
		}

		return identity.User{}, operationErr
	}

	updatedUser, err := existingUser.UpdateDetails(
		input.Username,
		input.DisplayName,
		input.Role,
		now,
	)
	if err != nil {
		operationErr := fmt.Errorf(
			"update user identity: %w",
			err,
		)
		return identity.User{}, service.recordDeniedChange(
			ctx,
			actor,
			audit.TypeUserUpdated,
			existingUser,
			audit.ReasonInvalidInput,
			now,
			operationErr,
		)
	}

	if actor.ID == existingUser.ID &&
		existingUser.Role == identity.RoleAdmin &&
		updatedUser.Role != identity.RoleAdmin {
		return identity.User{}, service.recordDeniedChange(
			ctx,
			actor,
			audit.TypeUserUpdated,
			existingUser,
			audit.ReasonSelfLockout,
			now,
			ErrSelfLockout,
		)
	}

	event, err := service.newUserEvent(
		actor,
		updatedUser,
		audit.TypeUserUpdated,
		audit.OutcomeSuccess,
		"",
		updatedUser.UpdatedAt,
	)
	if err != nil {
		return identity.User{}, err
	}
	if err := service.repository.UpdatePreservingLastAdministratorWithAudit(
		ctx,
		updatedUser,
		event,
	); err != nil {
		operationErr := fmt.Errorf(
			"persist updated user identity: %w",
			err,
		)
		reason := userChangeDenialReason(err)
		if reason != "" {
			return identity.User{}, service.recordDeniedChange(
				ctx,
				actor,
				audit.TypeUserUpdated,
				existingUser,
				reason,
				now,
				operationErr,
			)
		}

		return identity.User{}, operationErr
	}

	return updatedUser, nil
}

// SetUserActive changes and persists a user's activation state.
func (service *Service) SetUserActive(
	ctx context.Context,
	actor identity.User,
	id string,
	active bool,
) (identity.User, error) {
	now := service.currentTime()
	eventType := audit.TypeUserDeactivated
	if active {
		eventType = audit.TypeUserActivated
	}
	if err := identity.ValidateUserID(id); err != nil {
		return identity.User{}, service.recordDeniedChange(
			ctx, actor, eventType, safeUserTarget(id, ""),
			audit.ReasonInvalidInput, now, err,
		)
	}

	existingUser, err := service.repository.FindByID(ctx, id)
	if err != nil {
		operationErr := fmt.Errorf(
			"retrieve user for activation update: %w",
			err,
		)
		if errors.Is(err, identity.ErrUserNotFound) {
			return identity.User{}, service.recordDeniedChange(
				ctx, actor, eventType, safeUserTarget(id, ""),
				audit.ReasonUnknownTarget, now, operationErr,
			)
		}

		return identity.User{}, operationErr
	}

	if actor.ID == existingUser.ID &&
		existingUser.Role == identity.RoleAdmin &&
		!active {
		return identity.User{}, service.recordDeniedChange(
			ctx, actor, eventType, existingUser,
			audit.ReasonSelfLockout, now, ErrSelfLockout,
		)
	}

	if existingUser.Active == active {
		return existingUser, nil
	}

	updatedUser, err := existingUser.SetActive(
		active,
		now,
	)
	if err != nil {
		operationErr := fmt.Errorf(
			"set user activation state: %w",
			err,
		)
		return identity.User{}, service.recordDeniedChange(
			ctx, actor, eventType, existingUser,
			audit.ReasonInvalidInput, now, operationErr,
		)
	}

	event, err := service.newUserEvent(
		actor,
		updatedUser,
		eventType,
		audit.OutcomeSuccess,
		"",
		updatedUser.UpdatedAt,
	)
	if err != nil {
		return identity.User{}, err
	}
	if err := service.repository.UpdatePreservingLastAdministratorWithAudit(
		ctx,
		updatedUser,
		event,
	); err != nil {
		operationErr := fmt.Errorf(
			"persist user activation state: %w",
			err,
		)
		reason := userChangeDenialReason(err)
		if reason != "" {
			return identity.User{}, service.recordDeniedChange(
				ctx, actor, eventType, existingUser,
				reason, now, operationErr,
			)
		}

		return identity.User{}, operationErr
	}

	return updatedUser, nil
}

func (service *Service) recordDeniedChange(
	ctx context.Context,
	actor identity.User,
	eventType audit.Type,
	target identity.User,
	reason audit.Reason,
	now time.Time,
	operationErr error,
) error {
	event, err := service.newUserEvent(
		actor,
		target,
		eventType,
		audit.OutcomeDenied,
		reason,
		now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create denied user audit event: %w", err)
	}
	if err := service.audit.Append(ctx, event); err != nil {
		return fmt.Errorf("record denied user change: %w", err)
	}

	return operationErr
}

func (service *Service) newUserEvent(
	actor identity.User,
	target identity.User,
	eventType audit.Type,
	outcome audit.Outcome,
	reason audit.Reason,
	occurredAt time.Time,
) (audit.Event, error) {
	event, err := audit.NewEvent(audit.Event{
		ID:            service.generateEventID(),
		OccurredAt:    occurredAt.UTC(),
		Type:          eventType,
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
		return audit.Event{}, fmt.Errorf("create user audit event: %w", err)
	}

	return event, nil
}

func safeUserTarget(id string, username string) identity.User {
	target := identity.User{}
	if identity.ValidateUserID(id) == nil {
		target.ID = id
	}
	normalizedUsername, err := identity.NormalizeUsername(username)
	if err == nil {
		target.Username = normalizedUsername
	}

	return target
}

func userChangeDenialReason(inputError error) audit.Reason {
	switch {
	case errors.Is(inputError, identity.ErrUserConflict):
		return audit.ReasonResourceConflict
	case errors.Is(inputError, identity.ErrLastAdministrator):
		return audit.ReasonLastAdministrator
	default:
		return ""
	}
}

// DeleteUser permanently removes another user and their authentication data.
func (service *Service) DeleteUser(
	ctx context.Context,
	actorID string,
	id string,
) error {
	if err := identity.ValidateUserID(id); err != nil {
		return err
	}
	if actorID == id {
		return ErrSelfDeletion
	}

	if err := service.repository.DeletePreservingLastAdministrator(
		ctx,
		id,
	); err != nil {
		return fmt.Errorf("delete user identity: %w", err)
	}

	return nil
}
