package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/api"
	appusers "github.com/ebe542/go-mediaarchive/internal/application/users"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingUserWriter struct {
	createdUser identity.User
	updatedUser identity.User
	activeUser  identity.User
	createInput appusers.CreateUserInput
	updateInput appusers.UpdateUserInput
	actorID     string
	targetID    string
	active      bool
	createError error
	updateError error
	activeError error
	createCalls int
	updateCalls int
	activeCalls int
}

func (writer *recordingUserWriter) CreateUser(
	argContext context.Context,
	argInput appusers.CreateUserInput,
) (identity.User, error) {
	writer.createCalls++
	writer.createInput = argInput

	return writer.createdUser, writer.createError
}

func (writer *recordingUserWriter) UpdateUser(
	argContext context.Context,
	argActorID string,
	argID string,
	argInput appusers.UpdateUserInput,
) (identity.User, error) {
	writer.updateCalls++
	writer.actorID = argActorID
	writer.targetID = argID
	writer.updateInput = argInput

	return writer.updatedUser, writer.updateError
}

func (writer *recordingUserWriter) SetUserActive(
	argContext context.Context,
	argActorID string,
	argID string,
	argActive bool,
) (identity.User, error) {
	writer.activeCalls++
	writer.actorID = argActorID
	writer.targetID = argID
	writer.active = argActive

	return writer.activeUser, writer.activeError
}

func administratorResolver() *recordingSessionResolver {
	return &recordingSessionResolver{
		user: identity.User{
			ID:       "3f74e74d-e237-4bd4-a9bb-3407c38dd16f",
			Username: "archive_admin",
			Role:     identity.RoleAdmin,
			Active:   true,
		},
	}
}

func authenticatedJSONRequest(
	argMethod string,
	argPath string,
	argBody string,
) *http.Request {
	request := httptest.NewRequest(
		argMethod,
		argPath,
		strings.NewReader(argBody),
	)
	request.Header.Set("Authorization", "Bearer admin-session-token")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")

	return request
}

func TestCreateUserEndpointCreatesCredentiallessIdentity(t *testing.T) {
	createdUser := identity.User{
		ID:          "bc3516f0-a8e5-45b9-9004-b2f402880c97",
		Username:    "archive_editor",
		DisplayName: "Archive Editor",
		Role:        identity.RoleEditor,
		Active:      true,
	}
	writer := &recordingUserWriter{createdUser: createdUser}
	handler := api.NewHandler(
		api.WithUserManagementAPI(administratorResolver(), writer),
	)

	request := authenticatedJSONRequest(
		http.MethodPost,
		"/api/v1/users",
		`{"username":"archive_editor","displayName":"Archive Editor","role":"editor"}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, response.Code)
	}
	if writer.createCalls != 1 {
		t.Fatalf("expected one create call, got %d", writer.createCalls)
	}
	expectedInput := appusers.CreateUserInput{
		Username:    "archive_editor",
		DisplayName: "Archive Editor",
		Role:        identity.RoleEditor,
	}
	if writer.createInput != expectedInput {
		t.Fatalf("expected create input %#v, got %#v", expectedInput, writer.createInput)
	}
	if location := response.Header().Get("Location"); location != "/api/v1/users/"+createdUser.ID {
		t.Fatalf("expected created user location, got %q", location)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("expected no-store cache control, got %q", cacheControl)
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "password") {
		t.Fatal("expected response not to contain password data")
	}
}

func TestUpdateUserEndpointPassesActorAndMutableFields(t *testing.T) {
	updatedUser := identity.User{
		ID:          "bc3516f0-a8e5-45b9-9004-b2f402880c97",
		Username:    "updated_editor",
		DisplayName: "Updated Editor",
		Role:        identity.RoleEditor,
		Active:      true,
	}
	writer := &recordingUserWriter{updatedUser: updatedUser}
	resolver := administratorResolver()
	handler := api.NewHandler(api.WithUserManagementAPI(resolver, writer))

	request := authenticatedJSONRequest(
		http.MethodPut,
		"/api/v1/users/"+updatedUser.ID,
		`{"username":"updated_editor","displayName":"Updated Editor","role":"editor"}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if writer.updateCalls != 1 {
		t.Fatalf("expected one update call, got %d", writer.updateCalls)
	}
	if writer.actorID != resolver.user.ID || writer.targetID != updatedUser.ID {
		t.Fatalf("expected actor %q and target %q, got %q and %q", resolver.user.ID, updatedUser.ID, writer.actorID, writer.targetID)
	}
}

func TestSetUserActiveEndpointPassesExplicitState(t *testing.T) {
	targetID := "bc3516f0-a8e5-45b9-9004-b2f402880c97"
	writer := &recordingUserWriter{
		activeUser: identity.User{
			ID:       targetID,
			Username: "archive_viewer",
			Role:     identity.RoleViewer,
			Active:   false,
		},
	}
	resolver := administratorResolver()
	handler := api.NewHandler(api.WithUserManagementAPI(resolver, writer))

	request := authenticatedJSONRequest(
		http.MethodPut,
		"/api/v1/users/"+targetID+"/active",
		`{"active":false}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if writer.activeCalls != 1 || writer.active {
		t.Fatalf("expected one deactivation call, got %d calls with active=%t", writer.activeCalls, writer.active)
	}
	if writer.actorID != resolver.user.ID || writer.targetID != targetID {
		t.Fatal("expected authenticated actor and path target to reach user service")
	}
}

func TestUserMutationEndpointsRequireAdministrator(t *testing.T) {
	testCases := map[string]struct {
		method string
		path   string
		body   string
	}{
		"create":     {http.MethodPost, "/api/v1/users", `{"username":"new_user","displayName":"New User","role":"viewer"}`},
		"update":     {http.MethodPut, "/api/v1/users/bc3516f0-a8e5-45b9-9004-b2f402880c97", `{"username":"new_user","displayName":"New User","role":"viewer"}`},
		"activation": {http.MethodPut, "/api/v1/users/bc3516f0-a8e5-45b9-9004-b2f402880c97/active", `{"active":false}`},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			resolver := &recordingSessionResolver{user: identity.User{
				ID: "22c3c390-b5f2-41d4-9292-0c988fbaba0b", Role: identity.RoleEditor, Active: true,
			}}
			writer := &recordingUserWriter{}
			handler := api.NewHandler(api.WithUserManagementAPI(resolver, writer))
			request := authenticatedJSONRequest(testCase.method, testCase.path, testCase.body)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("expected status %d, got %d", http.StatusForbidden, response.Code)
			}
			if writer.createCalls+writer.updateCalls+writer.activeCalls != 0 {
				t.Fatal("expected forbidden request not to reach user service")
			}
		})
	}
}

func TestCreateUserEndpointRejectsInvalidJSONRequests(t *testing.T) {
	testCases := map[string]struct {
		contentType string
		body        string
	}{
		"missing content type": {"", `{}`},
		"unknown field":        {"application/json", `{"username":"new_user","displayName":"New User","role":"viewer","password":"secret"}`},
		"trailing value":       {"application/json", `{"username":"new_user","displayName":"New User","role":"viewer"}{}`},
		"malformed JSON":       {"application/json", `{"username":`},
		"oversized body":       {"application/json", strings.Repeat(" ", 64*1024) + `{}`},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			writer := &recordingUserWriter{}
			handler := api.NewHandler(api.WithUserManagementAPI(administratorResolver(), writer))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(testCase.body))
			request.Header.Set("Authorization", "Bearer admin-session-token")
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
			}
			if writer.createCalls != 0 {
				t.Fatal("expected invalid request not to reach user service")
			}
		})
	}
}

func TestSetUserActiveEndpointRequiresActiveField(t *testing.T) {
	writer := &recordingUserWriter{}
	handler := api.NewHandler(
		api.WithUserManagementAPI(administratorResolver(), writer),
	)
	request := authenticatedJSONRequest(
		http.MethodPut,
		"/api/v1/users/bc3516f0-a8e5-45b9-9004-b2f402880c97/active",
		`{}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusBadRequest,
			response.Code,
		)
	}
	if writer.activeCalls != 0 {
		t.Fatal("expected missing active field not to reach user service")
	}
}

func TestUserDeletionRemainsUnavailable(t *testing.T) {
	writer := &recordingUserWriter{}
	handler := api.NewHandler(
		api.WithUserManagementAPI(administratorResolver(), writer),
	)
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/users/bc3516f0-a8e5-45b9-9004-b2f402880c97",
		nil,
	)
	request.Header.Set("Authorization", "Bearer admin-session-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusMethodNotAllowed,
			response.Code,
		)
	}
	if writer.createCalls+writer.updateCalls+writer.activeCalls != 0 {
		t.Fatal("expected delete request not to reach user service")
	}
}

func TestUserMutationEndpointMapsApplicationErrors(t *testing.T) {
	testCases := map[string]struct {
		err       error
		status    int
		errorCode string
	}{
		"invalid input":      {identity.ErrInvalidRole, http.StatusBadRequest, "invalid_request"},
		"not found":          {identity.ErrUserNotFound, http.StatusNotFound, "not_found"},
		"conflict":           {identity.ErrUserConflict, http.StatusConflict, "conflict"},
		"self lockout":       {appusers.ErrSelfLockout, http.StatusConflict, "self_lockout"},
		"last administrator": {identity.ErrLastAdministrator, http.StatusConflict, "last_administrator"},
		"internal error":     {errors.New("database unavailable"), http.StatusInternalServerError, "internal_error"},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			writer := &recordingUserWriter{updateError: testCase.err}
			handler := api.NewHandler(api.WithUserManagementAPI(administratorResolver(), writer))
			request := authenticatedJSONRequest(
				http.MethodPut,
				"/api/v1/users/bc3516f0-a8e5-45b9-9004-b2f402880c97",
				`{"username":"updated_user","displayName":"Updated User","role":"editor"}`,
			)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != testCase.status {
				t.Fatalf("expected status %d, got %d", testCase.status, response.Code)
			}
			responseBody := response.Body.String()
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(responseBody), &body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if body.Error.Code != testCase.errorCode {
				t.Fatalf("expected error code %q, got %q", testCase.errorCode, body.Error.Code)
			}
			if strings.Contains(responseBody, "database unavailable") {
				t.Fatal("expected response not to expose internal error")
			}
		})
	}
}
