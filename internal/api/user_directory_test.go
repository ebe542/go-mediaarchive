package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/api"
	appusers "github.com/ebe542/go-mediaarchive/internal/application/users"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingUserLister struct {
	page      appusers.UserPage
	err       error
	input     appusers.ListUsersInput
	listCalls int
}

func (lister *recordingUserLister) ListUsers(
	_ context.Context,
	argInput appusers.ListUsersInput,
) (appusers.UserPage, error) {
	lister.listCalls++
	lister.input = argInput

	return lister.page, lister.err
}

func TestUserDirectoryReturnsPublicPageAndContinuation(t *testing.T) {
	createdAt := time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	listedUser := userDirectoryFixture(t, createdAt)
	nextCursor, err := appusers.NewCursor(listedUser.CreatedAt, listedUser.ID)
	if err != nil {
		t.Fatalf("create next cursor: %v", err)
	}
	lister := &recordingUserLister{
		page: appusers.UserPage{
			Users:      []identity.User{listedUser},
			NextCursor: &nextCursor,
		},
	}
	handler := userDirectoryTestHandler(identity.RoleAdmin, lister)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users?limit=1",
		nil,
	)
	request.Header.Set("Authorization", "Bearer admin-session-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusOK,
			response.Code,
			response.Body.String(),
		)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("expected user directory response not to be cached")
	}
	if lister.input.Limit != 1 || lister.input.Cursor != nil {
		t.Fatalf("unexpected first-page input: %+v", lister.input)
	}

	var body struct {
		Users []struct {
			ID          string        `json:"id"`
			Username    string        `json:"username"`
			DisplayName string        `json:"displayName"`
			Role        identity.Role `json:"role"`
			Active      bool          `json:"active"`
			CreatedAt   time.Time     `json:"createdAt"`
			UpdatedAt   time.Time     `json:"updatedAt"`
		} `json:"users"`
		NextCursor string `json:"nextCursor"`
	}
	responseDocument := append([]byte(nil), response.Body.Bytes()...)
	if err := json.NewDecoder(bytes.NewReader(responseDocument)).Decode(&body); err != nil {
		t.Fatalf("decode user page: %v", err)
	}
	if len(body.Users) != 1 ||
		body.Users[0].ID != listedUser.ID ||
		body.Users[0].CreatedAt != listedUser.CreatedAt ||
		body.Users[0].UpdatedAt != listedUser.UpdatedAt {
		t.Fatalf("unexpected public user page: %+v", body.Users)
	}
	if body.NextCursor == "" {
		t.Fatal("expected continuation cursor")
	}
	if strings.Contains(string(responseDocument), "password") ||
		strings.Contains(string(responseDocument), "accessToken") {
		t.Fatal("expected no credential or session data")
	}

	continuationRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users?limit=1&cursor="+body.NextCursor,
		nil,
	)
	continuationRequest.Header.Set(
		"Authorization",
		"Bearer admin-session-token",
	)
	continuationResponse := httptest.NewRecorder()
	handler.ServeHTTP(continuationResponse, continuationRequest)

	if continuationResponse.Code != http.StatusOK {
		t.Fatalf(
			"expected continuation status %d, got %d: %s",
			http.StatusOK,
			continuationResponse.Code,
			continuationResponse.Body.String(),
		)
	}
	if lister.input.Cursor == nil || *lister.input.Cursor != nextCursor {
		t.Fatalf("unexpected decoded cursor: %+v", lister.input.Cursor)
	}
}

func TestUserDirectoryOmitsCursorOnFinalPage(t *testing.T) {
	lister := &recordingUserLister{
		page: appusers.UserPage{Users: []identity.User{}},
	}
	handler := userDirectoryTestHandler(identity.RoleAdmin, lister)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	request.Header.Set("Authorization", "Bearer admin-session-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if strings.Contains(response.Body.String(), "nextCursor") {
		t.Fatalf("expected final page to omit cursor: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"users":[]`) {
		t.Fatalf("expected empty JSON array, got %s", response.Body.String())
	}
}

func TestUserDirectoryRejectsNonAdministrator(t *testing.T) {
	lister := &recordingUserLister{}
	handler := userDirectoryTestHandler(identity.RoleEditor, lister)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	request.Header.Set("Authorization", "Bearer editor-session-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, response.Code)
	}
	if lister.listCalls != 0 {
		t.Fatal("expected forbidden request not to list users")
	}
}

func TestUserDirectoryRejectsInvalidQueries(t *testing.T) {
	testCases := []string{
		"limit=0",
		"limit=101",
		"limit=not-a-number",
		"limit=1&limit=2",
		"cursor=",
		"cursor=invalid",
		"cursor=first&cursor=second",
		"unknown=value",
	}

	for _, query := range testCases {
		t.Run(query, func(t *testing.T) {
			lister := &recordingUserLister{}
			handler := userDirectoryTestHandler(identity.RoleAdmin, lister)
			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/users?"+query,
				nil,
			)
			request.Header.Set("Authorization", "Bearer admin-session-token")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf(
					"expected status %d, got %d: %s",
					http.StatusBadRequest,
					response.Code,
					response.Body.String(),
				)
			}
			if lister.listCalls != 0 {
				t.Fatal("expected invalid query not to list users")
			}
		})
	}
}

func TestUserDirectoryHidesApplicationFailures(t *testing.T) {
	lister := &recordingUserLister{
		err: errors.New("database unavailable"),
	}
	handler := userDirectoryTestHandler(identity.RoleAdmin, lister)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	request.Header.Set("Authorization", "Bearer admin-session-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	assertPasswordEnrollmentError(
		t,
		response,
		http.StatusInternalServerError,
		"internal_error",
	)
	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Fatal("expected response not to expose storage failure")
	}
}

func userDirectoryTestHandler(
	argRole identity.Role,
	argLister api.UserLister,
) http.Handler {
	return api.NewHandler(
		api.WithUserDirectoryAPI(
			&recordingSessionResolver{
				user: identity.User{
					ID:     "223e4567-e89b-12d3-a456-426614174000",
					Role:   argRole,
					Active: true,
				},
			},
			argLister,
		),
	)
}

func userDirectoryFixture(
	t *testing.T,
	argCreatedAt time.Time,
) identity.User {
	t.Helper()

	user, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"archive_viewer",
		"Archive Viewer",
		identity.RoleViewer,
		argCreatedAt,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	return user
}
