package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/api"
	appsessions "github.com/ebe542/go-mediaarchive/internal/application/sessions"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingSessionResolver struct {
	user         identity.User
	accessToken  string
	resolveCalls int
	resolveError error
}

func (resolver *recordingSessionResolver) Resolve(
	argContext context.Context,
	argAccessToken string,
) (identity.User, error) {
	resolver.resolveCalls++
	resolver.accessToken = argAccessToken

	return resolver.user, resolver.resolveError
}

func TestRequireAuthenticationProvidesCurrentUser(t *testing.T) {
	currentUser := identity.User{
		ID:          "3f74e74d-e237-4bd4-a9bb-3407c38dd16f",
		Username:    "archive_admin",
		DisplayName: "Archive Administrator",
		Role:        identity.RoleAdmin,
		Active:      true,
		CreatedAt:   time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC),
	}
	resolver := &recordingSessionResolver{
		user: currentUser,
	}

	nextCalled := false
	next := http.HandlerFunc(func(
		argResponse http.ResponseWriter,
		argRequest *http.Request,
	) {
		nextCalled = true

		authenticatedUser, exists := api.AuthenticatedUser(
			argRequest.Context(),
		)
		if !exists {
			t.Fatal("expected authenticated user in request context")
		}
		if authenticatedUser != currentUser {
			t.Fatalf(
				"expected authenticated user %+v, got %+v",
				currentUser,
				authenticatedUser,
			)
		}

		argResponse.WriteHeader(http.StatusNoContent)
	})

	handler := api.RequireAuthentication(resolver, next)

	request := httptest.NewRequest(
		http.MethodGet,
		"/protected",
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
			"expected status %d, got %d",
			http.StatusNoContent,
			response.Code,
		)
	}
	if !nextCalled {
		t.Fatal("expected protected handler to be called")
	}
	if resolver.resolveCalls != 1 {
		t.Fatalf(
			"expected one session resolution, got %d",
			resolver.resolveCalls,
		)
	}
	if resolver.accessToken != "opaque-session-token" {
		t.Fatal("expected bearer token to reach session resolver")
	}
}

func TestRequireAuthenticationRejectsUnusableSession(t *testing.T) {
	resolver := &recordingSessionResolver{
		resolveError: errors.Join(
			errors.New("resolve session"),
			appsessions.ErrUnauthenticated,
		),
	}

	nextCalled := false
	next := http.HandlerFunc(func(
		argResponse http.ResponseWriter,
		argRequest *http.Request,
	) {
		nextCalled = true
		argResponse.WriteHeader(http.StatusNoContent)
	})

	handler := api.RequireAuthentication(resolver, next)

	request := httptest.NewRequest(
		http.MethodGet,
		"/protected",
		nil,
	)
	request.Header.Set(
		"Authorization",
		"Bearer unusable-session-token",
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			response.Code,
		)
	}
	if nextCalled {
		t.Fatal("expected protected handler not to be called")
	}
	if challenge := response.Header().Get("WWW-Authenticate"); challenge != "Bearer" {
		t.Fatalf(
			"expected bearer authentication challenge, got %q",
			challenge,
		)
	}
	if strings.Contains(
		response.Body.String(),
		"unusable-session-token",
	) {
		t.Fatal("expected response not to expose bearer token")
	}
}

func TestRequireAuthenticationRejectsMalformedAuthorization(
	t *testing.T,
) {
	testCases := map[string][]string{
		"missing header": nil,
		"basic scheme": {
			"Basic opaque-session-token",
		},
		"missing token": {
			"Bearer",
		},
		"additional value": {
			"Bearer opaque-session-token extra",
		},
		"multiple headers": {
			"Bearer first-session-token",
			"Bearer second-session-token",
		},
	}

	for name, authorizationHeaders := range testCases {
		t.Run(name, func(t *testing.T) {
			resolver := &recordingSessionResolver{}

			nextCalled := false
			next := http.HandlerFunc(func(
				argResponse http.ResponseWriter,
				argRequest *http.Request,
			) {
				nextCalled = true
				argResponse.WriteHeader(http.StatusNoContent)
			})

			handler := api.RequireAuthentication(resolver, next)

			request := httptest.NewRequest(
				http.MethodGet,
				"/protected",
				nil,
			)
			for _, authorizationHeader := range authorizationHeaders {
				request.Header.Add(
					"Authorization",
					authorizationHeader,
				)
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
			if nextCalled {
				t.Fatal("expected protected handler not to be called")
			}
			if resolver.resolveCalls != 0 {
				t.Fatalf(
					"expected no session resolution, got %d calls",
					resolver.resolveCalls,
				)
			}
			if challenge := response.Header().Get(
				"WWW-Authenticate",
			); challenge != "Bearer" {
				t.Fatalf(
					"expected bearer authentication challenge, got %q",
					challenge,
				)
			}
		})
	}
}

func TestRequireAuthenticationPreservesInternalFailure(
	t *testing.T,
) {
	resolver := &recordingSessionResolver{
		resolveError: errors.New("database unavailable"),
	}

	nextCalled := false
	next := http.HandlerFunc(func(
		argResponse http.ResponseWriter,
		argRequest *http.Request,
	) {
		nextCalled = true
		argResponse.WriteHeader(http.StatusNoContent)
	})

	handler := api.RequireAuthentication(resolver, next)

	request := httptest.NewRequest(
		http.MethodGet,
		"/protected",
		nil,
	)
	request.Header.Set(
		"Authorization",
		"Bearer opaque-session-token",
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusInternalServerError,
			response.Code,
		)
	}
	if nextCalled {
		t.Fatal("expected protected handler not to be called")
	}
	if challenge := response.Header().Get("WWW-Authenticate"); challenge != "" {
		t.Fatalf(
			"expected no authentication challenge for internal failure, got %q",
			challenge,
		)
	}
	if strings.Contains(
		response.Body.String(),
		"opaque-session-token",
	) {
		t.Fatal("expected response not to expose bearer token")
	}
	if strings.Contains(
		response.Body.String(),
		"database unavailable",
	) {
		t.Fatal("expected response not to expose internal error")
	}
}
