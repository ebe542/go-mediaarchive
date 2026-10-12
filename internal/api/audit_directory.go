package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	appauditing "github.com/ebe542/go-mediaarchive/internal/application/auditing"
	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// AuditEventLister retrieves bounded pages from the audit directory.
type AuditEventLister interface {
	ListEvents(
		context.Context,
		identity.User,
		appauditing.ListInput,
	) (appauditing.Page, error)
}

// WithAuditDirectoryAPI enables administrator-only audit listing.
func WithAuditDirectoryAPI(
	resolver SessionResolver,
	lister AuditEventLister,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.auditDirectoryResolver = resolver
		configuration.auditEventLister = lister
	}
}

type auditDirectoryHandler struct {
	events AuditEventLister
}

type auditCursorDocument struct {
	Version    int    `json:"version"`
	OccurredAt string `json:"occurredAt"`
	ID         string `json:"id"`
	FilterHash string `json:"filterHash"`
	Limit      int    `json:"limit"`
}

type auditEventResponse struct {
	ID            string           `json:"id"`
	OccurredAt    string           `json:"occurredAt"`
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

func (handler *auditDirectoryHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	input, err := parseAuditDirectoryQuery(request.URL.Query())
	if err != nil {
		writeInvalidRequest(response)

		return
	}
	actor, exists := AuthenticatedUser(request.Context())
	if !exists {
		writeAuthenticationRequired(response)

		return
	}
	page, err := handler.events.ListEvents(request.Context(), actor, input)
	if errors.Is(err, appauditing.ErrInvalidFilter) ||
		errors.Is(err, appauditing.ErrInvalidPageLimit) ||
		errors.Is(err, appauditing.ErrInvalidCursor) {
		writeInvalidRequest(response)

		return
	}
	if err != nil {
		writeJSONError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	body := struct {
		Events     []auditEventResponse `json:"events"`
		NextCursor string               `json:"nextCursor,omitempty"`
	}{Events: make([]auditEventResponse, len(page.Events))}
	for index, event := range page.Events {
		body.Events[index] = newAuditEventResponse(event)
	}
	if page.NextCursor != nil {
		body.NextCursor, err = encodeAuditCursor(*page.NextCursor)
		if err != nil {
			writeJSONError(
				response,
				http.StatusInternalServerError,
				"internal_error",
				"Internal server error.",
			)

			return
		}
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(body)
}

func newAuditEventResponse(event audit.Event) auditEventResponse {
	result := auditEventResponse{
		ID:            event.ID,
		OccurredAt:    event.OccurredAt.Format(time.RFC3339Nano),
		Type:          event.Type,
		Outcome:       event.Outcome,
		ActorID:       event.ActorID,
		ActorUsername: event.ActorUsername,
		ActorRole:     event.ActorRole,
		TargetType:    event.TargetType,
		TargetID:      event.TargetID,
		TargetName:    event.TargetName,
		Reason:        event.Reason,
	}
	if event.Range != nil {
		start, end, total := event.Range.Start, event.Range.End, event.Range.Total
		result.RangeStart, result.RangeEnd, result.RangeTotal = &start, &end, &total
	}

	return result
}

func parseAuditDirectoryQuery(values url.Values) (appauditing.ListInput, error) {
	allowed := map[string]bool{
		"from": true, "to": true, "actorId": true, "type": true,
		"outcome": true, "limit": true, "cursor": true,
	}
	for name, entries := range values {
		if !allowed[name] || len(entries) != 1 || entries[0] == "" {
			return appauditing.ListInput{}, appauditing.ErrInvalidFilter
		}
	}
	input := appauditing.ListInput{}
	var err error
	if value := values.Get("from"); value != "" {
		input.Filter.From, err = parseCanonicalUTCTime(value)
		if err != nil {
			return appauditing.ListInput{}, err
		}
	}
	if value := values.Get("to"); value != "" {
		input.Filter.To, err = parseCanonicalUTCTime(value)
		if err != nil {
			return appauditing.ListInput{}, err
		}
	}
	input.Filter.ActorID = values.Get("actorId")
	input.Filter.Type = audit.Type(values.Get("type"))
	input.Filter.Outcome = audit.Outcome(values.Get("outcome"))
	if value := values.Get("limit"); value != "" {
		input.Limit, err = strconv.Atoi(value)
		if err != nil || input.Limit < 1 || input.Limit > appauditing.MaximumPageLimit {
			return appauditing.ListInput{}, appauditing.ErrInvalidPageLimit
		}
	}
	if value := values.Get("cursor"); value != "" {
		cursor, err := decodeAuditCursor(value)
		if err != nil {
			return appauditing.ListInput{}, err
		}
		input.Cursor = &cursor
	}

	return input, nil
}

func parseCanonicalUTCTime(value string) (*time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != value {
		return nil, appauditing.ErrInvalidFilter
	}

	return &parsed, nil
}

func encodeAuditCursor(cursor appauditing.Cursor) (string, error) {
	validated, err := appauditing.NewCursor(cursor.OccurredAt, cursor.ID, cursor.FilterHash, cursor.Limit)
	if err != nil {
		return "", err
	}
	document, err := json.Marshal(auditCursorDocument{
		Version:    1,
		OccurredAt: validated.OccurredAt.Format(time.RFC3339Nano),
		ID:         validated.ID,
		FilterHash: base64.RawURLEncoding.EncodeToString(validated.FilterHash[:]),
		Limit:      validated.Limit,
	})
	if err != nil {
		return "", fmt.Errorf("marshal audit cursor: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(document), nil
}

func decodeAuditCursor(encoded string) (appauditing.Cursor, error) {
	document, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return appauditing.Cursor{}, appauditing.ErrInvalidCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	var decoded auditCursorDocument
	if err := decoder.Decode(&decoded); err != nil {
		return appauditing.Cursor{}, appauditing.ErrInvalidCursor
	}
	if err := ensureJSONEnd(decoder); err != nil || decoded.Version != 1 {
		return appauditing.Cursor{}, appauditing.ErrInvalidCursor
	}
	occurredAt, err := parseCanonicalUTCTime(decoded.OccurredAt)
	if err != nil {
		return appauditing.Cursor{}, appauditing.ErrInvalidCursor
	}
	hashBytes, err := base64.RawURLEncoding.DecodeString(decoded.FilterHash)
	if err != nil || len(hashBytes) != sha256.Size {
		return appauditing.Cursor{}, appauditing.ErrInvalidCursor
	}
	var filterHash [sha256.Size]byte
	copy(filterHash[:], hashBytes)
	cursor, err := appauditing.NewCursor(*occurredAt, decoded.ID, filterHash, decoded.Limit)
	if err != nil {
		return appauditing.Cursor{}, err
	}

	canonical, err := encodeAuditCursor(cursor)
	if err != nil || canonical != encoded {
		return appauditing.Cursor{}, appauditing.ErrInvalidCursor
	}

	return cursor, nil
}
