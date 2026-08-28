package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/api"
	apppasswords "github.com/ebe542/go-mediaarchive/internal/application/passwords"
	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingPasswordEnrollmentService struct {
	issued            apppasswords.IssuedEnrollment
	issueError        error
	completedToken    string
	completedPassword []byte
	completeError     error
}

func (service *recordingPasswordEnrollmentService) IssueEnrollment(
	_ context.Context,
	_ string,
) (apppasswords.IssuedEnrollment, error) {
	return service.issued, service.issueError
}

func (service *recordingPasswordEnrollmentService) CompleteEnrollment(
	_ context.Context,
	argToken string,
	argPassword []byte,
) error {
	service.completedToken = argToken
	service.completedPassword = append([]byte(nil), argPassword...)

	return service.completeError
}

type recordingPasswordEnrollmentLimiter struct {
	allowed    bool
	allowedIP  string
	failureIP  string
	successIP  string
	canceledIP string
}

func (limiter *recordingPasswordEnrollmentLimiter) Allow(
	argSourceIP string,
	_ time.Time,
) bool {
	limiter.allowedIP = argSourceIP

	return limiter.allowed
}

func (limiter *recordingPasswordEnrollmentLimiter) RecordFailure(
	argSourceIP string,
	_ time.Time,
) {
	limiter.failureIP = argSourceIP
}

func (limiter *recordingPasswordEnrollmentLimiter) RecordSuccess(
	argSourceIP string,
) {
	limiter.successIP = argSourceIP
}

func (limiter *recordingPasswordEnrollmentLimiter) Cancel(
	argSourceIP string,
) {
	limiter.canceledIP = argSourceIP
}

type passwordEnrollmentSessionResolver struct {
	user identity.User
}

func (resolver passwordEnrollmentSessionResolver) Resolve(
	_ context.Context,
	_ string,
) (identity.User, error) {
	return resolver.user, nil
}

func TestPasswordEnrollmentIssueRequiresAdministrator(t *testing.T) {
	roles := []struct {
		name           string
		role           identity.Role
		expectedStatus int
	}{
		{name: "administrator", role: identity.RoleAdmin, expectedStatus: http.StatusCreated},
		{name: "editor", role: identity.RoleEditor, expectedStatus: http.StatusForbidden},
	}

	for _, testCase := range roles {
		t.Run(testCase.name, func(t *testing.T) {
			service := &recordingPasswordEnrollmentService{
				issued: apppasswords.IssuedEnrollment{
					Token:     "one-time-secret",
					ExpiresAt: time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC),
				},
			}
			handler := passwordEnrollmentTestHandler(
				passwordEnrollmentSessionResolver{
					user: identity.User{Role: testCase.role},
				},
				service,
				&recordingPasswordEnrollmentLimiter{allowed: true},
			)

			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/users/123e4567-e89b-12d3-a456-426614174000/password-enrollment",
				nil,
			)
			request.Header.Set("Authorization", "Bearer session-token")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != testCase.expectedStatus {
				t.Fatalf(
					"expected status %d, got %d: %s",
					testCase.expectedStatus,
					response.Code,
					response.Body.String(),
				)
			}
			if testCase.role == identity.RoleAdmin &&
				response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("expected enrollment token response not to be cached")
			}
		})
	}
}

func TestPasswordEnrollmentIssueMapsApplicationErrors(t *testing.T) {
	testCases := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
	}{
		{"missing user", identity.ErrUserNotFound, http.StatusNotFound, "not_found"},
		{"existing credential", credential.ErrPasswordCredentialExists, http.StatusConflict, "credential_exists"},
		{"unexpected error", errors.New("database unavailable"), http.StatusInternalServerError, "internal_error"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := passwordEnrollmentTestHandler(
				passwordEnrollmentSessionResolver{
					user: identity.User{Role: identity.RoleAdmin},
				},
				&recordingPasswordEnrollmentService{issueError: testCase.err},
				&recordingPasswordEnrollmentLimiter{allowed: true},
			)
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/users/123e4567-e89b-12d3-a456-426614174000/password-enrollment",
				nil,
			)
			request.Header.Set("Authorization", "Bearer session-token")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			assertPasswordEnrollmentError(
				t,
				response,
				testCase.expectedStatus,
				testCase.expectedCode,
			)
		})
	}
}

func TestPasswordEnrollmentCompletionUsesOnlySourceIPForLimiting(t *testing.T) {
	service := &recordingPasswordEnrollmentService{}
	limiter := &recordingPasswordEnrollmentLimiter{allowed: true}
	handler := passwordEnrollmentTestHandler(
		passwordEnrollmentSessionResolver{},
		service,
		limiter,
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/password-enrollments",
		strings.NewReader(
			`{"token":"attacker-controlled-token","password":"synthetic passphrase"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.10:54321"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusNoContent,
			response.Code,
			response.Body.String(),
		)
	}
	if limiter.allowedIP != "192.0.2.10" ||
		limiter.successIP != "192.0.2.10" {
		t.Fatalf("expected source-IP limiter calls, got %+v", limiter)
	}
	if service.completedToken != "attacker-controlled-token" ||
		string(service.completedPassword) != "synthetic passphrase" {
		t.Fatal("expected exact enrollment input to reach the service")
	}
}

func TestPasswordEnrollmentCompletionRecordsInvalidToken(t *testing.T) {
	service := &recordingPasswordEnrollmentService{
		completeError: apppasswords.ErrInvalidEnrollment,
	}
	limiter := &recordingPasswordEnrollmentLimiter{allowed: true}
	handler := passwordEnrollmentTestHandler(
		passwordEnrollmentSessionResolver{},
		service,
		limiter,
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/password-enrollments",
		strings.NewReader(
			`{"token":"invalid-token","password":"synthetic passphrase"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.10:54321"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	assertPasswordEnrollmentError(
		t,
		response,
		http.StatusUnauthorized,
		"invalid_enrollment",
	)
	if limiter.failureIP != "192.0.2.10" {
		t.Fatalf("expected failed source IP to be recorded, got %q", limiter.failureIP)
	}
}

func TestPasswordEnrollmentCompletionRejectsLimitedSource(t *testing.T) {
	service := &recordingPasswordEnrollmentService{}
	limiter := &recordingPasswordEnrollmentLimiter{allowed: false}
	handler := passwordEnrollmentTestHandler(
		passwordEnrollmentSessionResolver{},
		service,
		limiter,
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/password-enrollments",
		strings.NewReader(
			`{"token":"unexamined-token","password":"synthetic passphrase"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.10:54321"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	assertPasswordEnrollmentError(
		t,
		response,
		http.StatusTooManyRequests,
		"too_many_requests",
	)
	if service.completedToken != "" {
		t.Fatal("expected limited request not to reach the application service")
	}
}

func passwordEnrollmentTestHandler(
	argResolver api.SessionResolver,
	argService api.PasswordEnrollmentService,
	argLimiter api.PasswordEnrollmentAttemptLimiter,
) http.Handler {
	return api.NewHandler(
		api.WithPasswordEnrollmentAPI(
			argResolver,
			argService,
			argLimiter,
			func() time.Time {
				return time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
			},
		),
	)
}

func assertPasswordEnrollmentError(
	t *testing.T,
	argResponse *httptest.ResponseRecorder,
	argExpectedStatus int,
	argExpectedCode string,
) {
	t.Helper()

	if argResponse.Code != argExpectedStatus {
		t.Fatalf(
			"expected status %d, got %d: %s",
			argExpectedStatus,
			argResponse.Code,
			argResponse.Body.String(),
		)
	}

	var responseBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(argResponse.Body).Decode(&responseBody); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if responseBody.Error.Code != argExpectedCode {
		t.Fatalf(
			"expected error code %q, got %q",
			argExpectedCode,
			responseBody.Error.Code,
		)
	}
}
