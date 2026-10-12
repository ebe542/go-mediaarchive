package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/api"
	appauditing "github.com/ebe542/go-mediaarchive/internal/application/auditing"
	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingAuditLister struct {
	page  appauditing.Page
	input appauditing.ListInput
	actor identity.User
	calls int
}

func (lister *recordingAuditLister) ListEvents(
	_ context.Context,
	actor identity.User,
	input appauditing.ListInput,
) (appauditing.Page, error) {
	lister.calls++
	lister.actor = actor
	lister.input = input
	return lister.page, nil
}

func TestAuditDirectoryReturnsFilteredPage(t *testing.T) {
	admin := auditDirectoryUser(identity.RoleAdmin)
	filterHash := sha256.Sum256([]byte("filter"))
	lister := &recordingAuditLister{page: appauditing.Page{
		Events: []audit.Event{{
			ID:         "223e4567-e89b-12d3-a456-426614174000",
			OccurredAt: time.Date(2026, time.October, 12, 10, 0, 0, 0, time.UTC),
			Type:       audit.TypeMediaContentRead, Outcome: audit.OutcomeDenied,
			ActorID: admin.ID, ActorUsername: admin.Username,
			ActorRole: string(admin.Role), TargetType: audit.TargetMedia,
			TargetID: "323e4567-e89b-12d3-a456-426614174000",
			Reason:   audit.ReasonUnknownTarget,
		}},
		NextCursor: &appauditing.Cursor{
			OccurredAt: time.Date(2026, time.October, 12, 10, 0, 0, 0, time.UTC),
			ID:         "223e4567-e89b-12d3-a456-426614174000",
			FilterHash: filterHash, Limit: 25,
		},
	}}
	handler := api.NewHandler(api.WithAuditDirectoryAPI(
		&recordingSessionResolver{user: admin},
		lister,
	))
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/audit-events?outcome=denied&limit=25",
		nil,
	)
	request.Header.Set("Authorization", "Bearer audit-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if lister.calls != 1 || lister.actor.ID != admin.ID ||
		lister.input.Filter.Outcome != audit.OutcomeDenied || lister.input.Limit != 25 {
		t.Fatalf("unexpected audit list request: actor=%+v input=%+v", lister.actor, lister.input)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("expected audit response not to be cached")
	}
	var body struct {
		Events []struct {
			Type   audit.Type   `json:"type"`
			Reason audit.Reason `json:"reason"`
		} `json:"events"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode audit page: %v", err)
	}
	if len(body.Events) != 1 || body.Events[0].Type != audit.TypeMediaContentRead ||
		body.Events[0].Reason != audit.ReasonUnknownTarget || body.NextCursor == "" {
		t.Fatalf("unexpected audit response: %+v", body)
	}
}

func TestAuditDirectoryRejectsInvalidQueryBeforeListing(t *testing.T) {
	lister := &recordingAuditLister{}
	handler := api.NewHandler(api.WithAuditDirectoryAPI(
		&recordingSessionResolver{user: auditDirectoryUser(identity.RoleAdmin)},
		lister,
	))
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/audit-events?from=2026-10-12T10:00:00%2B02:00",
		nil,
	)
	request.Header.Set("Authorization", "Bearer audit-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || lister.calls != 0 ||
		!strings.Contains(response.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("unexpected invalid query response: %d %s", response.Code, response.Body.String())
	}
}

func TestAuditDirectoryRequiresAdministrator(t *testing.T) {
	lister := &recordingAuditLister{}
	handler := api.NewHandler(api.WithAuditDirectoryAPI(
		&recordingSessionResolver{user: auditDirectoryUser(identity.RoleViewer)},
		lister,
	))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit-events", nil)
	request.Header.Set("Authorization", "Bearer viewer-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || lister.calls != 0 {
		t.Fatalf("expected forbidden response, got %d", response.Code)
	}
}

func auditDirectoryUser(role identity.Role) identity.User {
	return identity.User{
		ID:       "123e4567-e89b-12d3-a456-426614174000",
		Username: "audit_user", DisplayName: "Audit User", Role: role, Active: true,
		CreatedAt: time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC),
	}
}
