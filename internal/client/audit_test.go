package client_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/client"
)

const (
	clientAuditActorID = "123e4567-e89b-12d3-a456-426614174000"
	clientAuditEventID = "223e4567-e89b-12d3-a456-426614174000"
	clientAuditMediaID = "323e4567-e89b-12d3-a456-426614174000"
)

func TestListAuditEventsUsesFiltersAndDecodesPage(t *testing.T) {
	location := time.FixedZone("test", 2*60*60)
	from := time.Date(2026, time.October, 12, 10, 0, 0, 0, location)
	to := from.Add(time.Hour)
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodGet, "/api/v1/audit-events", "access-token")
		query := request.URL.Query()
		if query.Get("from") != "2026-10-12T08:00:00Z" ||
			query.Get("to") != "2026-10-12T09:00:00Z" ||
			query.Get("actorId") != clientAuditActorID ||
			query.Get("type") != string(audit.TypeMediaContentRead) ||
			query.Get("outcome") != string(audit.OutcomeSuccess) ||
			query.Get("limit") != "25" || query.Get("cursor") != "opaque_cursor" {
			t.Fatalf("unexpected audit query: %q", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(validAuditPageJSON()))
	})
	defer server.Close()

	page, err := client.New(server.URL, server.Client()).ListAuditEvents(
		context.Background(),
		"access-token",
		client.AuditEventFilter{
			From: &from, To: &to, ActorID: clientAuditActorID,
			Type: audit.TypeMediaContentRead, Outcome: audit.OutcomeSuccess,
			Limit: 25, Cursor: "opaque_cursor",
		},
	)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(page.Events) != 1 || page.NextCursor != "next_cursor" {
		t.Fatalf("unexpected audit page: %+v", page)
	}
	event := page.Events[0]
	if event.ID != clientAuditEventID || event.ActorID != clientAuditActorID ||
		event.TargetID != clientAuditMediaID || event.Range == nil ||
		event.Range.Start != 100 || event.Range.End != 199 || event.Range.Total != 1000 {
		t.Fatalf("unexpected audit event: %+v", event)
	}
}

func TestListAuditEventsAcceptsEmptyFinalPage(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodGet, "/api/v1/audit-events", "access-token")
		if request.URL.RawQuery != "" {
			t.Fatalf("expected no audit query, got %q", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"events":[]}`))
	})
	defer server.Close()

	page, err := client.New(server.URL, server.Client()).ListAuditEvents(
		context.Background(), "access-token", client.AuditEventFilter{},
	)
	if err != nil {
		t.Fatalf("list empty audit page: %v", err)
	}
	if page.Events == nil || len(page.Events) != 0 || page.NextCursor != "" {
		t.Fatalf("unexpected empty audit page: %+v", page)
	}
}

func TestListAuditEventsRejectsInvalidFiltersBeforeRequest(t *testing.T) {
	now := time.Date(2026, time.October, 12, 10, 0, 0, 0, time.UTC)
	zero := time.Time{}
	for name, filter := range map[string]client.AuditEventFilter{
		"zero from":      {From: &zero},
		"reversed range": {From: &now, To: &now},
		"actor ID":       {ActorID: "not-a-uuid"},
		"event type":     {Type: "custom.event"},
		"outcome":        {Outcome: "unknown"},
		"negative limit": {Limit: -1},
		"large limit":    {Limit: 101},
		"cursor space":   {Cursor: "unsafe cursor"},
		"cursor size":    {Cursor: strings.Repeat("a", 2049)},
	} {
		t.Run(name, func(t *testing.T) {
			server := newJSONServer(t, func(http.ResponseWriter, *http.Request) {
				t.Fatal("expected invalid filter not to send a request")
			})
			defer server.Close()

			_, err := client.New(server.URL, server.Client()).ListAuditEvents(
				context.Background(), "access-token", filter,
			)
			if err == nil {
				t.Fatal("expected invalid audit filter error")
			}
		})
	}
}

func TestListAuditEventsRejectsInvalidResponses(t *testing.T) {
	valid := validAuditPageJSON()
	for name, responseBody := range map[string]string{
		"missing events": `{}`,
		"null events":    `{"events":null}`,
		"event ID":       strings.Replace(valid, clientAuditEventID, "not-a-uuid", 1),
		"timestamp":      strings.Replace(valid, "2026-10-12T10:00:00Z", "2026-10-12T12:00:00+02:00", 1),
		"range":          strings.Replace(valid, `,"rangeTotal":1000`, "", 1),
		"cursor":         strings.Replace(valid, "next_cursor", "unsafe cursor", 1),
		"unknown field":  strings.Replace(valid, `"nextCursor"`, `"unknown":true,"nextCursor"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			server := newJSONServer(t, func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(responseBody))
			})
			defer server.Close()

			_, err := client.New(server.URL, server.Client()).ListAuditEvents(
				context.Background(), "access-token", client.AuditEventFilter{},
			)
			if err == nil {
				t.Fatal("expected invalid audit response error")
			}
		})
	}
}

func TestListAuditEventsReturnsSafeAPIError(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte(
			`{"error":{"code":"forbidden","message":"Access forbidden."}}`,
		))
	})
	defer server.Close()

	_, err := client.New(server.URL, server.Client()).ListAuditEvents(
		context.Background(), "access-token", client.AuditEventFilter{},
	)
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden ||
		apiErr.Code != "forbidden" {
		t.Fatalf("expected safe forbidden API error, got %v", err)
	}
}

func validAuditPageJSON() string {
	return `{"events":[{` +
		`"id":"` + clientAuditEventID + `",` +
		`"occurredAt":"2026-10-12T10:00:00Z",` +
		`"type":"media.content_read","outcome":"success",` +
		`"actorId":"` + clientAuditActorID + `",` +
		`"actorUsername":"archive_admin","actorRole":"admin",` +
		`"targetType":"media","targetId":"` + clientAuditMediaID + `",` +
		`"targetName":"Security Book",` +
		`"rangeStart":100,"rangeEnd":199,"rangeTotal":1000` +
		`}],"nextCursor":"next_cursor"}`
}
