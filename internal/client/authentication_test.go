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
	test *testing.T,
	handler http.HandlerFunc,
) *httptest.Server {
	test.Helper()

	return httptest.NewServer(handler)
}

func assertRequest(
	test *testing.T,
	request *http.Request,
	method string,
	path string,
	accessToken string,
) {
	test.Helper()

	if request.Method != method {
		test.Errorf("expected method %q, got %q", method, request.Method)
	}
	if request.URL.Path != path {
		test.Errorf("expected path %q, got %q", path, request.URL.Path)
	}
	if accept := request.Header.Get("Accept"); accept != "application/json" {
		test.Errorf("expected JSON Accept header, got %q", accept)
	}
	expectedAuthorization := ""
	if accessToken != "" {
		expectedAuthorization = "Bearer " + accessToken
	}
	if authorization := request.Header.Get("Authorization"); authorization != expectedAuthorization {
		test.Errorf(
			"expected Authorization header %q, got %q",
			expectedAuthorization,
			authorization,
		)
	}
}

func decodeRequest(test *testing.T, request *http.Request, body any) {
	test.Helper()

	if contentType := request.Header.Get("Content-Type"); contentType != "application/json" {
		test.Errorf("expected JSON Content-Type, got %q", contentType)
	}
	if err := json.NewDecoder(request.Body).Decode(body); err != nil {
		test.Fatalf("decode request: %v", err)
	}
}

func writeJSON(
	test *testing.T,
	response http.ResponseWriter,
	status int,
	body any,
) {
	test.Helper()

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(body); err != nil {
		test.Fatalf("encode response: %v", err)
	}
}
