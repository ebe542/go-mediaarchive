package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/client"
)

func TestLoginCreatesSession(t *testing.T) {
	expiresAt := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodPost, "/api/v1/auth/sessions", "")

		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		decodeRequest(t, request, &body)
		if body.Username != "archive_user" || body.Password != "synthetic passphrase" {
			t.Errorf("unexpected login request: %+v", body)
		}

		writeJSON(t, response, http.StatusCreated, map[string]any{
			"accessToken": "opaque-access-token",
			"tokenType":   "Bearer",
			"expiresAt":   expiresAt,
		})
	})
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())
	session, err := apiClient.Login(
		context.Background(),
		"archive_user",
		[]byte("synthetic passphrase"),
	)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if session.AccessToken != "opaque-access-token" ||
		session.TokenType != "Bearer" ||
		!session.ExpiresAt.Equal(expiresAt) {
		t.Errorf("unexpected session: %+v", session)
	}
}

func TestLogoutRevokesCurrentSession(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(
			t,
			request,
			http.MethodDelete,
			"/api/v1/auth/sessions/current",
			"opaque-access-token",
		)
		response.WriteHeader(http.StatusNoContent)
	})
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())
	if err := apiClient.Logout(context.Background(), "opaque-access-token"); err != nil {
		t.Fatalf("logout: %v", err)
	}
}

func TestClientReturnsStructuredAPIError(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{
				"code":    "invalid_credentials",
				"message": "Invalid credentials.",
			},
		})
	})
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())
	_, err := apiClient.Login(context.Background(), "user", []byte("password"))
	var apiError *client.APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiError.StatusCode != http.StatusUnauthorized ||
		apiError.Code != "invalid_credentials" ||
		apiError.Message != "Invalid credentials." {
		t.Errorf("unexpected API error: %+v", apiError)
	}
}

func newJSONServer(
	argTest *testing.T,
	argHandler http.HandlerFunc,
) *httptest.Server {
	argTest.Helper()

	return httptest.NewServer(argHandler)
}

func assertRequest(
	argTest *testing.T,
	argRequest *http.Request,
	argMethod string,
	argPath string,
	argAccessToken string,
) {
	argTest.Helper()

	if argRequest.Method != argMethod {
		argTest.Errorf("expected method %q, got %q", argMethod, argRequest.Method)
	}
	if argRequest.URL.Path != argPath {
		argTest.Errorf("expected path %q, got %q", argPath, argRequest.URL.Path)
	}
	if accept := argRequest.Header.Get("Accept"); accept != "application/json" {
		argTest.Errorf("expected JSON Accept header, got %q", accept)
	}
	expectedAuthorization := ""
	if argAccessToken != "" {
		expectedAuthorization = "Bearer " + argAccessToken
	}
	if authorization := argRequest.Header.Get("Authorization"); authorization != expectedAuthorization {
		argTest.Errorf(
			"expected Authorization header %q, got %q",
			expectedAuthorization,
			authorization,
		)
	}
}

func decodeRequest(argTest *testing.T, argRequest *http.Request, argBody any) {
	argTest.Helper()

	if contentType := argRequest.Header.Get("Content-Type"); contentType != "application/json" {
		argTest.Errorf("expected JSON Content-Type, got %q", contentType)
	}
	if err := json.NewDecoder(argRequest.Body).Decode(argBody); err != nil {
		argTest.Fatalf("decode request: %v", err)
	}
}

func writeJSON(
	argTest *testing.T,
	argResponse http.ResponseWriter,
	argStatus int,
	argBody any,
) {
	argTest.Helper()

	argResponse.Header().Set("Content-Type", "application/json; charset=utf-8")
	argResponse.WriteHeader(argStatus)
	if err := json.NewEncoder(argResponse).Encode(argBody); err != nil {
		argTest.Fatalf("encode response: %v", err)
	}
}
