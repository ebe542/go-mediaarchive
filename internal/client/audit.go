package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

const (
	maximumAuditCursorLength = 2048
	maximumAuditPageLimit    = 100
)

// AuditEventFilter selects one bounded audit directory page.
type AuditEventFilter struct {
	From    *time.Time
	To      *time.Time
	ActorID string
	Type    audit.Type
	Outcome audit.Outcome
	Limit   int
	Cursor  string
}

// AuditRange describes an inclusive byte range in an audit snapshot.
type AuditRange struct {
	Start int64
	End   int64
	Total int64
}

// AuditEvent is a validated immutable security-event snapshot.
type AuditEvent struct {
	ID            string
	OccurredAt    time.Time
	Type          audit.Type
	Outcome       audit.Outcome
	ActorID       string
	ActorUsername string
	ActorRole     string
	TargetType    audit.TargetType
	TargetID      string
	TargetName    string
	Reason        audit.Reason
	Range         *AuditRange
}

// AuditEventPage contains one audit directory page and its continuation token.
type AuditEventPage struct {
	Events     []AuditEvent
	NextCursor string
}

type auditEventResponse struct {
	ID            string           `json:"id"`
	OccurredAt    time.Time        `json:"occurredAt"`
	Type          audit.Type       `json:"type"`
	Outcome       audit.Outcome    `json:"outcome"`
	ActorID       string           `json:"actorId,omitempty"`
	ActorUsername string           `json:"actorUsername,omitempty"`
	ActorRole     string           `json:"actorRole,omitempty"`
	TargetType    audit.TargetType `json:"targetType"`
	TargetID      string           `json:"targetId,omitempty"`
	TargetName    string           `json:"targetName,omitempty"`
	Reason        audit.Reason     `json:"reason,omitempty"`
	RangeStart    *int64           `json:"rangeStart,omitempty"`
	RangeEnd      *int64           `json:"rangeEnd,omitempty"`
	RangeTotal    *int64           `json:"rangeTotal,omitempty"`
}

type auditEventPageResponse struct {
	Events     []auditEventResponse `json:"events"`
	NextCursor string               `json:"nextCursor,omitempty"`
}

// ListAuditEvents returns one administrator-only audit directory page.
func (client *Client) ListAuditEvents(
	ctx context.Context,
	accessToken string,
	filter AuditEventFilter,
) (AuditEventPage, error) {
	query, err := auditFilterQuery(filter)
	if err != nil {
		return AuditEventPage{}, err
	}
	path := "/api/v1/audit-events"
	if encodedQuery := query.Encode(); encodedQuery != "" {
		path += "?" + encodedQuery
	}

	var response auditEventPageResponse
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		path,
		accessToken,
		nil,
		http.StatusOK,
		&response,
		"audit directory",
	); err != nil {
		return AuditEventPage{}, err
	}

	return decodeAuditEventPage(response)
}

func auditFilterQuery(filter AuditEventFilter) (url.Values, error) {
	query := url.Values{}
	if filter.From != nil {
		if filter.From.IsZero() {
			return nil, errors.New("validate audit filter: from time is required")
		}
		query.Set("from", filter.From.UTC().Format(time.RFC3339Nano))
	}
	if filter.To != nil {
		if filter.To.IsZero() {
			return nil, errors.New("validate audit filter: to time is required")
		}
		query.Set("to", filter.To.UTC().Format(time.RFC3339Nano))
	}
	if filter.From != nil && filter.To != nil && !filter.From.Before(*filter.To) {
		return nil, errors.New("validate audit filter: from must be before to")
	}
	if filter.ActorID != "" {
		if err := identity.ValidateUserID(filter.ActorID); err != nil {
			return nil, fmt.Errorf("validate audit filter actor ID: %w", err)
		}
		query.Set("actorId", filter.ActorID)
	}
	if filter.Type != "" {
		if !filter.Type.Valid() {
			return nil, errors.New("validate audit filter: unsupported event type")
		}
		query.Set("type", string(filter.Type))
	}
	if filter.Outcome != "" {
		if !filter.Outcome.Valid() {
			return nil, errors.New("validate audit filter: unsupported outcome")
		}
		query.Set("outcome", string(filter.Outcome))
	}
	if filter.Limit != 0 {
		if filter.Limit < 1 || filter.Limit > maximumAuditPageLimit {
			return nil, fmt.Errorf(
				"validate audit filter: limit must be from 1 through %d",
				maximumAuditPageLimit,
			)
		}
		query.Set("limit", strconv.Itoa(filter.Limit))
	}
	if filter.Cursor != "" {
		if err := validateAuditCursor(filter.Cursor); err != nil {
			return nil, err
		}
		query.Set("cursor", filter.Cursor)
	}

	return query, nil
}

func decodeAuditEventPage(response auditEventPageResponse) (AuditEventPage, error) {
	if response.Events == nil {
		return AuditEventPage{}, errors.New(
			"validate audit directory response: events are required",
		)
	}
	if response.NextCursor != "" {
		if err := validateAuditCursor(response.NextCursor); err != nil {
			return AuditEventPage{}, fmt.Errorf(
				"validate audit directory response cursor: %w",
				err,
			)
		}
	}

	page := AuditEventPage{
		Events:     make([]AuditEvent, len(response.Events)),
		NextCursor: response.NextCursor,
	}
	seenIDs := make(map[string]struct{}, len(response.Events))
	for index, candidate := range response.Events {
		event, err := decodeAuditEvent(candidate)
		if err != nil {
			return AuditEventPage{}, fmt.Errorf(
				"validate audit directory event %d: %w",
				index,
				err,
			)
		}
		if _, exists := seenIDs[event.ID]; exists {
			return AuditEventPage{}, errors.New(
				"validate audit directory response: duplicate event ID",
			)
		}
		seenIDs[event.ID] = struct{}{}
		if index > 0 && !auditEventBefore(page.Events[index-1], event) {
			return AuditEventPage{}, errors.New(
				"validate audit directory response: events are not in descending order",
			)
		}
		page.Events[index] = event
	}

	return page, nil
}

func decodeAuditEvent(response auditEventResponse) (AuditEvent, error) {
	rangeValues := 0
	for _, value := range []*int64{response.RangeStart, response.RangeEnd, response.RangeTotal} {
		if value != nil {
			rangeValues++
		}
	}
	if rangeValues != 0 && rangeValues != 3 {
		return AuditEvent{}, errors.New("range fields must be complete")
	}
	var byteRange *audit.ByteRange
	if rangeValues == 3 {
		byteRange = &audit.ByteRange{
			Start: *response.RangeStart,
			End:   *response.RangeEnd,
			Total: *response.RangeTotal,
		}
	}
	validated, err := audit.NewEvent(audit.Event{
		ID:            response.ID,
		OccurredAt:    response.OccurredAt,
		Type:          response.Type,
		Outcome:       response.Outcome,
		ActorID:       response.ActorID,
		ActorUsername: response.ActorUsername,
		ActorRole:     response.ActorRole,
		TargetType:    response.TargetType,
		TargetID:      response.TargetID,
		TargetName:    response.TargetName,
		Reason:        response.Reason,
		Range:         byteRange,
	})
	if err != nil {
		return AuditEvent{}, err
	}
	event := AuditEvent{
		ID:            validated.ID,
		OccurredAt:    validated.OccurredAt,
		Type:          validated.Type,
		Outcome:       validated.Outcome,
		ActorID:       validated.ActorID,
		ActorUsername: validated.ActorUsername,
		ActorRole:     validated.ActorRole,
		TargetType:    validated.TargetType,
		TargetID:      validated.TargetID,
		TargetName:    validated.TargetName,
		Reason:        validated.Reason,
	}
	if validated.Range != nil {
		event.Range = &AuditRange{
			Start: validated.Range.Start,
			End:   validated.Range.End,
			Total: validated.Range.Total,
		}
	}

	return event, nil
}

func validateAuditCursor(cursor string) error {
	if len(cursor) > maximumAuditCursorLength || cursor != strings.TrimSpace(cursor) {
		return errors.New("validate audit cursor: expected bounded URL-safe text")
	}
	for _, character := range cursor {
		if !((character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_') {
			return errors.New("validate audit cursor: expected bounded URL-safe text")
		}
	}

	return nil
}

func auditEventBefore(previous AuditEvent, current AuditEvent) bool {
	if previous.OccurredAt.Equal(current.OccurredAt) {
		return previous.ID > current.ID
	}

	return previous.OccurredAt.After(current.OccurredAt)
}
