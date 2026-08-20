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
	"github.com/ebe542/go-mediaarchive/internal/application/authentication"
	appsessions "github.com/ebe542/go-mediaarchive/internal/application/sessions"
)

type recordingSessionService struct {
	createdSession appsessions.Created
	username       string
	password       []byte
	createError    error
	createCalls    int
	revokedToken   string
	revokeCalls    int
	revokeError    error
}

func (service *recordingSessionService) Create(
	argContext context.Context,
	argUsername string,
	argPassword []byte,
) (appsessions.Created, error) {
	service.createCalls++
	service.username = argUsername
	service.password = append([]byte(nil), argPassword...)

	return service.createdSession, service.createError
}

func (service *recordingSessionService) Revoke(
	argContext context.Context,
	argAccessToken string,
) error {
	service.revokeCalls++
	service.revokedToken = argAccessToken

	return service.revokeError
}

type recordingAttemptLimiter struct {
	allowed            bool
	successfulUsername string
	sourceIP           string
	failedUsername     string
	failedSourceIP     string
	failureCalls       int
	canceledUsername   string
	canceledSourceIP   string
	cancelCalls        int
}

func (limiter *recordingAttemptLimiter) Allow(
	argUsername string,
	argSourceIP string,
	argNow time.Time,
) bool {
	limiter.sourceIP = argSourceIP

	return limiter.allowed
}

func (limiter *recordingAttemptLimiter) RecordFailure(
	argUsername string,
	argSourceIP string,
	argNow time.Time,
) {
	limiter.failureCalls++
	limiter.failedUsername = argUsername
	limiter.failedSourceIP = argSourceIP
}

func (limiter *recordingAttemptLimiter) RecordSuccess(
	argUsername string,
	argSourceIP string,
) {
	limiter.successfulUsername = argUsername
}

func (limiter *recordingAttemptLimiter) Cancel(
	argUsername string,
	argSourceIP string,
) {
	limiter.cancelCalls++
	limiter.canceledUsername = argUsername
	limiter.canceledSourceIP = argSourceIP
}

func TestCreateSessionEndpointReturnsOpaqueToken(t *testing.T) {
	now := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(8 * time.Hour)

	sessionService := &recordingSessionService{
		createdSession: appsessions.Created{
			AccessToken: "opaque-session-token",
			ExpiresAt:   expiresAt,
		},
	}
	limiter := &recordingAttemptLimiter{
		allowed: true,
	}

	handler := api.NewHandler(
		api.WithAuthentication(
			sessionService,
			limiter,
			func() time.Time {
				return now
			},
		),
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/sessions",
		strings.NewReader(
			`{"username":"archive_admin","password":"synthetic passphrase"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.RemoteAddr = "192.0.2.10:12345"

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusCreated,
			response.Code,
			response.Body.String(),
		)
	}

	if cacheControl := response.Header().Get(
		"Cache-Control",
	); cacheControl != "no-store" {
		t.Fatalf(
			"expected Cache-Control %q, got %q",
			"no-store",
			cacheControl,
		)
	}

	const expectedContentType = "application/json; charset=utf-8"
	if contentType := response.Header().Get(
		"Content-Type",
	); contentType != expectedContentType {
		t.Fatalf(
			"expected Content-Type %q, got %q",
			expectedContentType,
			contentType,
		)
	}

	var body struct {
		AccessToken string    `json:"accessToken"`
		TokenType   string    `json:"tokenType"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if body.AccessToken != "opaque-session-token" {
		t.Fatal("expected opaque access token")
	}
	if body.TokenType != "Bearer" {
		t.Fatalf(
			"expected token type %q, got %q",
			"Bearer",
			body.TokenType,
		)
	}
	if !body.ExpiresAt.Equal(expiresAt) {
		t.Fatalf(
			"expected expiration %v, got %v",
			expiresAt,
			body.ExpiresAt,
		)
	}

	if sessionService.username != "archive_admin" {
		t.Fatalf(
			"expected username %q, got %q",
			"archive_admin",
			sessionService.username,
		)
	}
	if string(sessionService.password) != "synthetic passphrase" {
		t.Fatal("expected password to reach session service")
	}

	if limiter.successfulUsername != "archive_admin" {
		t.Fatal("expected successful username limit to be cleared")
	}
}

func TestCreateSessionEndpointReturnsGenericAuthenticationFailure(
	t *testing.T,
) {
	sessionService := &recordingSessionService{
		createError: errors.Join(
			errors.New("authentication failed"),
			authentication.ErrInvalidCredentials,
		),
	}
	limiter := &recordingAttemptLimiter{
		allowed: true,
	}

	handler := api.NewHandler(
		api.WithAuthentication(
			sessionService,
			limiter,
			func() time.Time {
				return time.Date(
					2026,
					time.August,
					20,
					10,
					0,
					0,
					0,
					time.UTC,
				)
			},
		),
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/sessions",
		strings.NewReader(
			`{"username":"unknown_user","password":"synthetic passphrase"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.10:12345"

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			response.Code,
		)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if body.Error.Code != "invalid_credentials" {
		t.Fatalf(
			"expected error code %q, got %q",
			"invalid_credentials",
			body.Error.Code,
		)
	}
	if body.Error.Message != "Invalid username or password." {
		t.Fatalf(
			"expected generic message, got %q",
			body.Error.Message,
		)
	}

	if limiter.failureCalls != 1 {
		t.Fatalf(
			"expected one recorded failure, got %d",
			limiter.failureCalls,
		)
	}
	if limiter.failedUsername != "unknown_user" {
		t.Fatal("expected failed username to be recorded")
	}
	if limiter.failedSourceIP != "192.0.2.10" {
		t.Fatalf(
			"expected source IP %q, got %q",
			"192.0.2.10",
			limiter.failedSourceIP,
		)
	}

	combinedOutput := response.Body.String()
	if strings.Contains(combinedOutput, "synthetic passphrase") {
		t.Fatal("expected response not to contain the password")
	}
}

func TestCreateSessionEndpointAcceptsJSONMediaTypeParameters(
	t *testing.T,
) {
	sessionService := &recordingSessionService{
		createdSession: appsessions.Created{
			AccessToken: "opaque-session-token",
			ExpiresAt: time.Date(
				2026,
				time.August,
				20,
				18,
				0,
				0,
				0,
				time.UTC,
			),
		},
	}
	limiter := &recordingAttemptLimiter{
		allowed: true,
	}

	handler := api.NewHandler(
		api.WithAuthentication(
			sessionService,
			limiter,
			func() time.Time {
				return time.Date(
					2026,
					time.August,
					20,
					10,
					0,
					0,
					0,
					time.UTC,
				)
			},
		),
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/sessions",
		strings.NewReader(
			`{"username":"archive_admin","password":"synthetic passphrase"}`,
		),
	)
	request.Header.Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	request.RemoteAddr = "192.0.2.10:12345"

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusCreated,
			response.Code,
			response.Body.String(),
		)
	}
}

func TestCreateSessionEndpointRejectsInvalidRequests(t *testing.T) {
	oversizedPassword := strings.Repeat("a", 65*1024)

	testCases := map[string]string{
		"malformed JSON": `{`,
		"unknown field": `{
			"username":"archive_admin",
			"password":"synthetic passphrase",
			"unexpected":true
		}`,
		"multiple JSON values": `{
			"username":"archive_admin",
			"password":"synthetic passphrase"
		} {}`,
		"missing username": `{
			"password":"synthetic passphrase"
		}`,
		"missing password": `{
			"username":"archive_admin"
		}`,
		"oversized body": `{
			"username":"archive_admin",
			"password":"` + oversizedPassword + `"
		}`,
	}

	for name, requestBody := range testCases {
		t.Run(name, func(t *testing.T) {
			sessionService := &recordingSessionService{
				createdSession: appsessions.Created{
					AccessToken: "must-not-be-returned",
				},
			}
			limiter := &recordingAttemptLimiter{
				allowed: true,
			}

			handler := api.NewHandler(
				api.WithAuthentication(
					sessionService,
					limiter,
					func() time.Time {
						return time.Date(
							2026,
							time.August,
							20,
							10,
							0,
							0,
							0,
							time.UTC,
						)
					},
				),
			)

			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/auth/sessions",
				strings.NewReader(requestBody),
			)
			request.Header.Set(
				"Content-Type",
				"application/json",
			)
			request.RemoteAddr = "192.0.2.10:12345"

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

			if sessionService.createCalls != 0 {
				t.Fatalf(
					"expected no session creation, got %d calls",
					sessionService.createCalls,
				)
			}

			if strings.Contains(
				response.Body.String(),
				oversizedPassword,
			) {
				t.Fatal("expected response not to echo request data")
			}
		})
	}
}

func TestCreateSessionEndpointRejectsLimitedAttempt(t *testing.T) {
	sessionService := &recordingSessionService{}
	limiter := &recordingAttemptLimiter{
		allowed: false,
	}

	handler := api.NewHandler(
		api.WithAuthentication(
			sessionService,
			limiter,
			func() time.Time {
				return time.Date(
					2026,
					time.August,
					20,
					10,
					0,
					0,
					0,
					time.UTC,
				)
			},
		),
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/sessions",
		strings.NewReader(
			`{"username":"archive_admin","password":"synthetic passphrase"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.10:12345"

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusTooManyRequests,
			response.Code,
		)
	}

	if sessionService.createCalls != 0 {
		t.Fatalf(
			"expected no session creation, got %d calls",
			sessionService.createCalls,
		)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if body.Error.Code != "too_many_requests" {
		t.Fatalf(
			"expected error code %q, got %q",
			"too_many_requests",
			body.Error.Code,
		)
	}

	if strings.Contains(body.Error.Message, "username") ||
		strings.Contains(body.Error.Message, "IP") {
		t.Fatal("expected response not to identify the limiting bucket")
	}
}

func TestRevokeCurrentSessionEndpointIsIdempotent(t *testing.T) {
	sessionService := &recordingSessionService{}
	limiter := &recordingAttemptLimiter{
		allowed: true,
	}

	handler := api.NewHandler(
		api.WithAuthentication(
			sessionService,
			limiter,
			func() time.Time {
				return time.Date(
					2026,
					time.August,
					20,
					10,
					0,
					0,
					0,
					time.UTC,
				)
			},
		),
	)

	for range 2 {
		request := httptest.NewRequest(
			http.MethodDelete,
			"/api/v1/auth/sessions/current",
			nil,
		)
		request.Header.Set(
			"Authorization",
			"Bearer opaque-session-token",
		)

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

		if response.Body.Len() != 0 {
			t.Fatalf(
				"expected empty response body, got %q",
				response.Body.String(),
			)
		}

		if cacheControl := response.Header().Get(
			"Cache-Control",
		); cacheControl != "no-store" {
			t.Fatalf(
				"expected Cache-Control %q, got %q",
				"no-store",
				cacheControl,
			)
		}
	}

	if sessionService.revokeCalls != 2 {
		t.Fatalf(
			"expected two revoke calls, got %d",
			sessionService.revokeCalls,
		)
	}
	if sessionService.revokedToken != "opaque-session-token" {
		t.Fatal("expected bearer token to be revoked")
	}
}

func TestRevokeCurrentSessionEndpointRejectsInvalidAuthorization(
	t *testing.T,
) {
	testCases := map[string][]string{
		"missing header": nil,
		"wrong scheme": {
			"Basic opaque-session-token",
		},
		"missing token": {
			"Bearer",
		},
		"extra value": {
			"Bearer opaque-session-token extra",
		},
		"multiple headers": {
			"Bearer first-token",
			"Bearer second-token",
		},
	}

	for name, authorizationHeaders := range testCases {
		t.Run(name, func(t *testing.T) {
			sessionService := &recordingSessionService{}
			limiter := &recordingAttemptLimiter{
				allowed: true,
			}

			handler := api.NewHandler(
				api.WithAuthentication(
					sessionService,
					limiter,
					time.Now,
				),
			)

			request := httptest.NewRequest(
				http.MethodDelete,
				"/api/v1/auth/sessions/current",
				nil,
			)
			for _, header := range authorizationHeaders {
				request.Header.Add("Authorization", header)
			}

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf(
					"expected status %d, got %d",
					http.StatusUnauthorized,
					response.Code,
				)
			}

			if sessionService.revokeCalls != 0 {
				t.Fatalf(
					"expected no revoke call, got %d",
					sessionService.revokeCalls,
				)
			}

			responseText := response.Body.String()
			for _, header := range authorizationHeaders {
				if strings.Contains(
					responseText,
					header,
				) {
					t.Fatal(
						"expected response not to contain authorization data",
					)
				}
			}
		})
	}
}

func TestCreateSessionEndpointCancelsAttemptAfterInternalError(
	t *testing.T,
) {
	sessionService := &recordingSessionService{
		createError: errors.New("database unavailable"),
	}
	limiter := &recordingAttemptLimiter{
		allowed: true,
	}

	handler := api.NewHandler(
		api.WithAuthentication(
			sessionService,
			limiter,
			func() time.Time {
				return time.Date(
					2026,
					time.August,
					20,
					10,
					0,
					0,
					0,
					time.UTC,
				)
			},
		),
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/sessions",
		strings.NewReader(
			`{"username":"archive_admin","password":"synthetic passphrase"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.10:12345"

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusInternalServerError,
			response.Code,
		)
	}

	if limiter.cancelCalls != 1 {
		t.Fatalf(
			"expected one canceled attempt, got %d",
			limiter.cancelCalls,
		)
	}
	if limiter.canceledUsername != "archive_admin" {
		t.Fatalf(
			"expected canceled username %q, got %q",
			"archive_admin",
			limiter.canceledUsername,
		)
	}
	if limiter.canceledSourceIP != "192.0.2.10" {
		t.Fatalf(
			"expected canceled source IP %q, got %q",
			"192.0.2.10",
			limiter.canceledSourceIP,
		)
	}
}
