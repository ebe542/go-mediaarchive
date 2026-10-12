// Package auditing coordinates security audit directory operations.
package auditing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/google/uuid"
)

const (
	// DefaultPageLimit bounds ordinary audit directory responses.
	DefaultPageLimit = 50
	// MaximumPageLimit bounds database and response work per request.
	MaximumPageLimit = 100
)

var (
	// ErrInvalidFilter indicates unsupported audit selection criteria.
	ErrInvalidFilter = errors.New("invalid audit event filter")
	// ErrInvalidPageLimit indicates an audit page size outside the bounds.
	ErrInvalidPageLimit = errors.New("invalid audit event page limit")
	// ErrInvalidCursor indicates an unusable audit directory position.
	ErrInvalidCursor = errors.New("invalid audit event cursor")
)

// Filter selects audit events using exact or time-bound criteria.
type Filter struct {
	From    *time.Time
	To      *time.Time
	ActorID string
	Type    audit.Type
	Outcome audit.Outcome
}

// Cursor identifies the last event and binds the active query criteria.
type Cursor struct {
	OccurredAt time.Time
	ID         string
	FilterHash [sha256.Size]byte
	Limit      int
}

// ListInput selects one bounded audit directory page.
type ListInput struct {
	Filter Filter
	Limit  int
	Cursor *Cursor
}

// Page contains audit events and an optional continuation position.
type Page struct {
	Events     []audit.Event
	NextCursor *Cursor
}

// Repository retrieves immutable audit events.
type Repository interface {
	ListAuditEvents(context.Context, Filter, *Cursor, int) ([]audit.Event, error)
}

// EventIDGenerator creates canonical audit event identifiers.
type EventIDGenerator func() string

// Clock returns the current application time.
type Clock func() time.Time

// Service coordinates authorized audit directory access.
type Service struct {
	repository Repository
	audit      audit.Appender
	eventIDs   EventIDGenerator
	clock      Clock
}

// NewService creates an audit directory application service.
func NewService(
	repository Repository,
	auditAppender audit.Appender,
	eventIDGenerator EventIDGenerator,
	clock Clock,
) *Service {
	return &Service{
		repository: repository,
		audit:      auditAppender,
		eventIDs:   eventIDGenerator,
		clock:      clock,
	}
}

// ListEvents audits and retrieves one validated directory page.
func (service *Service) ListEvents(
	ctx context.Context,
	actor identity.User,
	input ListInput,
) (Page, error) {
	validated, filterHash, err := validateInput(input)
	if err != nil {
		return Page{}, err
	}

	event, err := audit.NewEvent(audit.Event{
		ID:            service.eventIDs(),
		OccurredAt:    service.clock().UTC(),
		Type:          audit.TypeAuditEventsListed,
		Outcome:       audit.OutcomeSuccess,
		ActorID:       actor.ID,
		ActorUsername: actor.Username,
		ActorRole:     string(actor.Role),
		TargetType:    audit.TargetAudit,
	})
	if err != nil {
		return Page{}, fmt.Errorf("create audit directory event: %w", err)
	}
	if err := service.audit.Append(ctx, event); err != nil {
		return Page{}, fmt.Errorf("record audit directory access: %w", err)
	}

	events, err := service.repository.ListAuditEvents(
		ctx,
		validated.Filter,
		validated.Cursor,
		validated.Limit+1,
	)
	if err != nil {
		return Page{}, fmt.Errorf("retrieve audit event page: %w", err)
	}

	page := Page{Events: events}
	if len(events) <= validated.Limit {
		return page, nil
	}

	page.Events = events[:validated.Limit]
	last := page.Events[len(page.Events)-1]
	page.NextCursor = &Cursor{
		OccurredAt: last.OccurredAt,
		ID:         last.ID,
		FilterHash: filterHash,
		Limit:      validated.Limit,
	}

	return page, nil
}

func validateInput(input ListInput) (ListInput, [sha256.Size]byte, error) {
	limit := input.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit < 1 || limit > MaximumPageLimit {
		return ListInput{}, [sha256.Size]byte{}, ErrInvalidPageLimit
	}
	filter, err := validateFilter(input.Filter)
	if err != nil {
		return ListInput{}, [sha256.Size]byte{}, err
	}
	filterHash, err := fingerprint(filter, limit)
	if err != nil {
		return ListInput{}, [sha256.Size]byte{}, err
	}
	input.Filter = filter
	input.Limit = limit
	if input.Cursor != nil {
		cursor, err := NewCursor(
			input.Cursor.OccurredAt,
			input.Cursor.ID,
			input.Cursor.FilterHash,
			input.Cursor.Limit,
		)
		if err != nil || cursor.FilterHash != filterHash || cursor.Limit != limit {
			return ListInput{}, [sha256.Size]byte{}, ErrInvalidCursor
		}
		input.Cursor = &cursor
	}

	return input, filterHash, nil
}

func validateFilter(filter Filter) (Filter, error) {
	if filter.From != nil {
		if filter.From.IsZero() {
			return Filter{}, ErrInvalidFilter
		}
		value := filter.From.UTC()
		filter.From = &value
	}
	if filter.To != nil {
		if filter.To.IsZero() {
			return Filter{}, ErrInvalidFilter
		}
		value := filter.To.UTC()
		filter.To = &value
	}
	if filter.From != nil && filter.To != nil && !filter.From.Before(*filter.To) {
		return Filter{}, ErrInvalidFilter
	}
	if filter.ActorID != "" {
		if err := identity.ValidateUserID(filter.ActorID); err != nil {
			return Filter{}, fmt.Errorf("%w: actor ID", ErrInvalidFilter)
		}
	}
	if filter.Type != "" && !filter.Type.Valid() {
		return Filter{}, fmt.Errorf("%w: event type", ErrInvalidFilter)
	}
	if filter.Outcome != "" && !filter.Outcome.Valid() {
		return Filter{}, fmt.Errorf("%w: outcome", ErrInvalidFilter)
	}

	return filter, nil
}

// NewCursor validates and creates an audit directory cursor.
func NewCursor(
	occurredAt time.Time,
	id string,
	filterHash [sha256.Size]byte,
	limit int,
) (Cursor, error) {
	parsedID, err := uuid.Parse(id)
	if occurredAt.IsZero() || err != nil || parsedID == uuid.Nil ||
		parsedID.String() != id || filterHash == ([sha256.Size]byte{}) ||
		limit < 1 || limit > MaximumPageLimit {
		return Cursor{}, ErrInvalidCursor
	}

	return Cursor{
		OccurredAt: occurredAt.UTC(),
		ID:         id,
		FilterHash: filterHash,
		Limit:      limit,
	}, nil
}

func fingerprint(filter Filter, limit int) ([sha256.Size]byte, error) {
	document := struct {
		From    string        `json:"from"`
		To      string        `json:"to"`
		ActorID string        `json:"actorId"`
		Type    audit.Type    `json:"type"`
		Outcome audit.Outcome `json:"outcome"`
		Limit   int           `json:"limit"`
	}{ActorID: filter.ActorID, Type: filter.Type, Outcome: filter.Outcome, Limit: limit}
	if filter.From != nil {
		document.From = filter.From.Format(time.RFC3339Nano)
	}
	if filter.To != nil {
		document.To = filter.To.Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode audit filter: %w", err)
	}

	return sha256.Sum256(encoded), nil
}
