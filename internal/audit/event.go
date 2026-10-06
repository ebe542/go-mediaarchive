// Package audit defines immutable security audit events.
package audit

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maximumSnapshotLength = 200
	maximumTargetIDLength = 200
)

// Type identifies a stable security-relevant operation.
type Type string

const (
	TypeSessionCreated              Type = "session.created"
	TypeSessionCreateDenied         Type = "session.create_denied"
	TypeSessionRevoked              Type = "session.revoked"
	TypeUserCreated                 Type = "user.created"
	TypeUserUpdated                 Type = "user.updated"
	TypeUserActivated               Type = "user.activated"
	TypeUserDeactivated             Type = "user.deactivated"
	TypeUserDeleted                 Type = "user.deleted"
	TypePasswordEnrollmentIssued    Type = "password.enrollment_issued"
	TypePasswordEnrollmentCompleted Type = "password.enrollment_completed"
	TypePasswordChanged             Type = "password.changed"
	TypeMediaUploaded               Type = "media.uploaded"
	TypeMediaDeleted                Type = "media.deleted"
	TypeMediaGrantReplaced          Type = "media.grant_replaced"
	TypeMediaGrantRevoked           Type = "media.grant_revoked"
	TypeMediaContentRead            Type = "media.content_read"
	TypeAuditEventsListed           Type = "audit.events_listed"
)

// Valid reports whether the event type is part of the audit contract.
func (eventType Type) Valid() bool {
	switch eventType {
	case TypeSessionCreated, TypeSessionCreateDenied, TypeSessionRevoked,
		TypeUserCreated, TypeUserUpdated, TypeUserActivated,
		TypeUserDeactivated, TypeUserDeleted,
		TypePasswordEnrollmentIssued, TypePasswordEnrollmentCompleted,
		TypePasswordChanged, TypeMediaUploaded, TypeMediaDeleted,
		TypeMediaGrantReplaced, TypeMediaGrantRevoked,
		TypeMediaContentRead, TypeAuditEventsListed:
		return true
	default:
		return false
	}
}

// Outcome describes whether an audited operation succeeded, was denied, or failed.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeDenied  Outcome = "denied"
	OutcomeFailure Outcome = "failure"
)

// Valid reports whether the outcome is supported.
func (outcome Outcome) Valid() bool {
	switch outcome {
	case OutcomeSuccess, OutcomeDenied, OutcomeFailure:
		return true
	default:
		return false
	}
}

// TargetType classifies the object affected by an event.
type TargetType string

const (
	TargetSession TargetType = "session"
	TargetUser    TargetType = "user"
	TargetMedia   TargetType = "media"
	TargetGrant   TargetType = "grant"
	TargetAudit   TargetType = "audit"
)

// Valid reports whether the target category is supported.
func (targetType TargetType) Valid() bool {
	switch targetType {
	case TargetSession, TargetUser, TargetMedia, TargetGrant, TargetAudit:
		return true
	default:
		return false
	}
}

// Reason is an allowlisted internal result explanation.
type Reason string

const (
	ReasonInvalidCredentials     Reason = "invalid_credentials"
	ReasonInactiveUser           Reason = "inactive_user"
	ReasonMissingAuthentication  Reason = "missing_authentication"
	ReasonInsufficientRole       Reason = "insufficient_role"
	ReasonUnknownTarget          Reason = "unknown_target"
	ReasonMissingPermission      Reason = "missing_permission"
	ReasonMissingContentLocation Reason = "missing_content_location"
	ReasonMissingManagedFile     Reason = "missing_managed_file"
	ReasonInvalidRange           Reason = "invalid_range"
	ReasonInvalidInput           Reason = "invalid_input"
	ReasonResourceConflict       Reason = "resource_conflict"
	ReasonSelfLockout            Reason = "self_lockout"
	ReasonLastAdministrator      Reason = "last_administrator"
	ReasonOperationFailure       Reason = "operation_failure"
)

// Valid reports whether the reason is safe to persist.
func (reason Reason) Valid() bool {
	switch reason {
	case ReasonInvalidCredentials, ReasonInactiveUser,
		ReasonMissingAuthentication, ReasonInsufficientRole,
		ReasonUnknownTarget, ReasonMissingPermission,
		ReasonMissingContentLocation, ReasonMissingManagedFile,
		ReasonInvalidRange, ReasonInvalidInput, ReasonResourceConflict,
		ReasonSelfLockout, ReasonLastAdministrator, ReasonOperationFailure:
		return true
	default:
		return false
	}
}

var (
	ErrInvalidEventID   = errors.New("invalid audit event ID")
	ErrInvalidTimestamp = errors.New("invalid audit timestamp")
	ErrInvalidType      = errors.New("invalid audit event type")
	ErrInvalidOutcome   = errors.New("invalid audit outcome")
	ErrInvalidActor     = errors.New("invalid audit actor")
	ErrInvalidTarget    = errors.New("invalid audit target")
	ErrInvalidReason    = errors.New("invalid audit reason")
	ErrInvalidRange     = errors.New("invalid audit range")
	ErrEventConflict    = errors.New("audit event already exists")
)

// ByteRange records an inclusive partial-content selection.
type ByteRange struct {
	Start int64
	End   int64
	Total int64
}

// Event is an immutable snapshot of one security-relevant operation.
type Event struct {
	ID            string
	OccurredAt    time.Time
	Type          Type
	Outcome       Outcome
	ActorID       string
	ActorUsername string
	ActorRole     string
	TargetType    TargetType
	TargetID      string
	TargetName    string
	Reason        Reason
	Range         *ByteRange
}

// NewEvent validates an immutable audit event and returns an owned copy.
func NewEvent(candidate Event) (Event, error) {
	parsedID, err := uuid.Parse(candidate.ID)
	if err != nil || parsedID == uuid.Nil || parsedID.String() != candidate.ID {
		return Event{}, fmt.Errorf("%w: expected a canonical lowercase UUID", ErrInvalidEventID)
	}
	if candidate.OccurredAt.IsZero() || candidate.OccurredAt.Location() != time.UTC {
		return Event{}, fmt.Errorf("%w: expected a non-zero UTC value", ErrInvalidTimestamp)
	}
	if !candidate.Type.Valid() {
		return Event{}, fmt.Errorf("%w: %q", ErrInvalidType, candidate.Type)
	}
	if !candidate.Outcome.Valid() {
		return Event{}, fmt.Errorf("%w: %q", ErrInvalidOutcome, candidate.Outcome)
	}
	if err := validateActor(candidate); err != nil {
		return Event{}, err
	}
	if err := validateTarget(candidate); err != nil {
		return Event{}, err
	}
	if candidate.Reason != "" && !candidate.Reason.Valid() {
		return Event{}, fmt.Errorf("%w: %q", ErrInvalidReason, candidate.Reason)
	}
	if candidate.Outcome == OutcomeSuccess && candidate.Reason != "" {
		return Event{}, fmt.Errorf("%w: successful events have no reason", ErrInvalidReason)
	}
	if candidate.Range != nil {
		if candidate.Range.Start < 0 || candidate.Range.End < candidate.Range.Start ||
			candidate.Range.Total <= candidate.Range.End {
			return Event{}, fmt.Errorf("%w: expected 0 <= start <= end < total", ErrInvalidRange)
		}
		value := *candidate.Range
		candidate.Range = &value
	}

	return candidate, nil
}

func validateActor(event Event) error {
	if event.ActorID == "" {
		if event.ActorUsername != "" || event.ActorRole != "" {
			return fmt.Errorf("%w: anonymous actors have no username or role", ErrInvalidActor)
		}
		return nil
	}
	parsedID, err := uuid.Parse(event.ActorID)
	if err != nil || parsedID == uuid.Nil || parsedID.String() != event.ActorID {
		return fmt.Errorf("%w: expected a canonical actor UUID", ErrInvalidActor)
	}
	if err := validateSnapshot(event.ActorUsername); err != nil {
		return fmt.Errorf("%w: username: %v", ErrInvalidActor, err)
	}
	switch event.ActorRole {
	case "viewer", "editor", "admin":
		return nil
	default:
		return fmt.Errorf("%w: unsupported role %q", ErrInvalidActor, event.ActorRole)
	}
}

func validateTarget(event Event) error {
	if !event.TargetType.Valid() {
		return fmt.Errorf("%w: unsupported type %q", ErrInvalidTarget, event.TargetType)
	}
	if utf8.RuneCountInString(event.TargetID) > maximumTargetIDLength ||
		containsUnsafeText(event.TargetID) {
		return fmt.Errorf("%w: unsafe target ID", ErrInvalidTarget)
	}
	if event.TargetName != "" {
		if err := validateSnapshot(event.TargetName); err != nil {
			return fmt.Errorf("%w: name: %v", ErrInvalidTarget, err)
		}
	}
	return nil
}

func validateSnapshot(value string) error {
	if value != strings.TrimSpace(value) || value == "" ||
		utf8.RuneCountInString(value) > maximumSnapshotLength || containsUnsafeText(value) {
		return errors.New("expected bounded, trimmed text without control characters")
	}
	return nil
}

func containsUnsafeText(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
