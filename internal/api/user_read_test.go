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
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingUserReader struct {
	user        identity.User
	requestedID string
	readCalls   int
	readError   error
}

func (reader *recordingUserReader) UserByID(
	argContext context.Context,
	argID string,
) (identity.User, error) {
	reader.readCalls++
	reader.requestedID = argID

	return reader.user, reader.readError
}

func TestCurrentUserEndpointAllowsEveryActiveRole(t *testing.T) {
	testCases := map[string]identity.Role{
		"viewer": identity.RoleViewer,
		"editor": identity.RoleEditor,
		"admin":  identity.RoleAdmin,
	}

	for name, role := range testCases {
		t.Run(name, func(t *testing.T) {
			currentUser := identity.User{
				ID:          "3f74e74d-e237-4bd4-a9bb-3407c38dd16f",
				Username:    "archive_user",
				DisplayName: "Archive User",
				Role:        role,
				Active:      true,
			}
			resolver := &recordingSessionResolver{
				user: currentUser,
			}
			userReader := &recordingUserReader{}

			handler := api.NewHandler(
				api.WithUserReadAPI(
					resolver,
					userReader,
				),
			)

			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/users/me",
				nil,
			)
			request.Header.Set(
				"Authorization",
				"Bearer opaque-session-token",
			)

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf(
					"expected status %d, got %d",
					http.StatusOK,
					response.Code,
				)
			}
			if contentType := response.Header().Get(
				"Content-Type",
			); contentType != "application/json; charset=utf-8" {
				t.Fatalf(
					"expected JSON content type, got %q",
					contentType,
				)
			}
			if cacheControl := response.Header().Get(
				"Cache-Control",
			); cacheControl != "no-store" {
				t.Fatalf(
					"expected no-store cache control, got %q",
					cacheControl,
				)
			}

			var body struct {
				ID          string        `json:"id"`
				Username    string        `json:"username"`
				DisplayName string        `json:"displayName"`
				Role        identity.Role `json:"role"`
				Active      bool          `json:"active"`
			}

			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode current user response: %v", err)
			}

			if body.ID != currentUser.ID ||
				body.Username != currentUser.Username ||
				body.DisplayName != currentUser.DisplayName ||
				body.Role != currentUser.Role ||
				body.Active != currentUser.Active {
				t.Fatalf(
					"expected current user %+v, got %+v",
					currentUser,
					body,
				)
			}
			if userReader.readCalls != 0 {
				t.Fatalf(
					"expected no additional user lookup, got %d calls",
					userReader.readCalls,
				)
			}
		})
	}
}
func TestUserByIDEndpointAllowsAdministrator(t *testing.T) {
	targetUser := identity.User{
		ID:          "bc3516f0-a8e5-45b9-9004-b2f402880c97",
		Username:    "archive_viewer",
		DisplayName: "Archive Viewer",
		Role:        identity.RoleViewer,
		Active:      true,
	}
	resolver := &recordingSessionResolver{
		user: identity.User{
			ID:       "3f74e74d-e237-4bd4-a9bb-3407c38dd16f",
			Username: "archive_admin",
			Role:     identity.RoleAdmin,
			Active:   true,
		},
	}
	userReader := &recordingUserReader{
		user: targetUser,
	}

	handler := api.NewHandler(
		api.WithUserReadAPI(
			resolver,
			userReader,
		),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users/"+targetUser.ID,
		nil,
	)
	request.Header.Set(
		"Authorization",
		"Bearer admin-session-token",
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}
	if userReader.readCalls != 1 {
		t.Fatalf(
			"expected one user lookup, got %d",
			userReader.readCalls,
		)
	}
	if userReader.requestedID != targetUser.ID {
		t.Fatalf(
			"expected requested user ID %q, got %q",
			targetUser.ID,
			userReader.requestedID,
		)
	}

	var body struct {
		ID          string        `json:"id"`
		Username    string        `json:"username"`
		DisplayName string        `json:"displayName"`
		Role        identity.Role `json:"role"`
		Active      bool          `json:"active"`
	}

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode user response: %v", err)
	}

	if body.ID != targetUser.ID ||
		body.Username != targetUser.Username ||
		body.DisplayName != targetUser.DisplayName ||
		body.Role != targetUser.Role ||
		body.Active != targetUser.Active {
		t.Fatalf(
			"expected target user %+v, got %+v",
			targetUser,
			body,
		)
	}
}

func TestUserByIDEndpointRejectsNonAdministratorRoles(
	t *testing.T,
) {
	testCases := map[string]identity.Role{
		"viewer": identity.RoleViewer,
		"editor": identity.RoleEditor,
	}

	for name, role := range testCases {
		t.Run(name, func(t *testing.T) {
			resolver := &recordingSessionResolver{
				user: identity.User{
					ID:       "22c3c390-b5f2-41d4-9292-0c988fbaba0b",
					Username: "archive_user",
					Role:     role,
					Active:   true,
				},
			}
			userReader := &recordingUserReader{}

			handler := api.NewHandler(
				api.WithUserReadAPI(
					resolver,
					userReader,
				),
			)

			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/users/bc3516f0-a8e5-45b9-9004-b2f402880c97",
				nil,
			)
			request.Header.Set(
				"Authorization",
				"Bearer non-admin-session-token",
			)

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf(
					"expected status %d, got %d",
					http.StatusForbidden,
					response.Code,
				)
			}
			if userReader.readCalls != 0 {
				t.Fatalf(
					"expected no user lookup, got %d calls",
					userReader.readCalls,
				)
			}
		})
	}
}

func TestUserByIDEndpointReturnsNotFoundForUnknownUser(
	t *testing.T,
) {
	resolver := &recordingSessionResolver{
		user: identity.User{
			ID:       "3f74e74d-e237-4bd4-a9bb-3407c38dd16f",
			Username: "archive_admin",
			Role:     identity.RoleAdmin,
			Active:   true,
		},
	}
	userReader := &recordingUserReader{
		readError: errors.Join(
			errors.New("retrieve user"),
			identity.ErrUserNotFound,
		),
	}

	handler := api.NewHandler(
		api.WithUserReadAPI(
			resolver,
			userReader,
		),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users/bc3516f0-a8e5-45b9-9004-b2f402880c97",
		nil,
	)
	request.Header.Set(
		"Authorization",
		"Bearer admin-session-token",
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusNotFound,
			response.Code,
		)
	}
	if strings.Contains(
		response.Body.String(),
		"retrieve user",
	) {
		t.Fatal("expected response not to expose storage details")
	}
}

func TestUserMutationEndpointsRemainUnavailable(t *testing.T) {
	userID := "bc3516f0-a8e5-45b9-9004-b2f402880c97"

	testCases := map[string]struct {
		method         string
		path           string
		expectedStatus int
	}{
		"create user": {
			method:         http.MethodPost,
			path:           "/api/v1/users",
			expectedStatus: http.StatusNotFound,
		},
		"update user": {
			method:         http.MethodPatch,
			path:           "/api/v1/users/" + userID,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		"delete user": {
			method:         http.MethodDelete,
			path:           "/api/v1/users/" + userID,
			expectedStatus: http.StatusMethodNotAllowed,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			resolver := &recordingSessionResolver{}
			userReader := &recordingUserReader{}

			handler := api.NewHandler(
				api.WithUserReadAPI(
					resolver,
					userReader,
				),
			)

			request := httptest.NewRequest(
				testCase.method,
				testCase.path,
				nil,
			)
			request.Header.Set(
				"Authorization",
				"Bearer opaque-session-token",
			)

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != testCase.expectedStatus {
				t.Fatalf(
					"expected status %d, got %d",
					testCase.expectedStatus,
					response.Code,
				)
			}
			if resolver.resolveCalls != 0 {
				t.Fatalf(
					"expected no session resolution, got %d calls",
					resolver.resolveCalls,
				)
			}
			if userReader.readCalls != 0 {
				t.Fatalf(
					"expected no user operation, got %d calls",
					userReader.readCalls,
				)
			}
		})
	}
}
