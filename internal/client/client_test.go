package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"errors"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/client"
)

func TestHealthRequestsVersionedEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet {
				t.Errorf(
					"expected method %q, got %q",
					http.MethodGet,
					request.Method,
				)
			}

			if request.URL.Path != "/api/v1/health" {
				t.Errorf(
					"expected path %q, got %q",
					"/api/v1/health",
					request.URL.Path,
				)
			}

			if accept := request.Header.Get("Accept"); accept != "application/json" {
				t.Errorf(
					"expected Accept header %q, got %q",
					"application/json",
					accept,
				)
			}

			response.Header().Set(
				"Content-Type",
				"application/json; charset=utf-8",
			)
			response.WriteHeader(http.StatusOK)

			_, _ = response.Write([]byte(`{"status":"ok"}`))
		},
	))
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())

	status, err := apiClient.Health(context.Background())
	if err != nil {
		t.Fatalf("request health status: %v", err)
	}

	if status.Status != "ok" {
		t.Errorf("expected status %q, got %q", "ok", status.Status)
	}
}

func TestHealthRejectsMissingStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set(
				"Content-Type",
				"application/json; charset=utf-8",
			)

			_, _ = response.Write([]byte(`{}`))
		},
	))
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())

	_, err := apiClient.Health(context.Background())
	if err == nil {
		t.Fatal("expected an error for a missing health status")
	}
}

func TestHealthRejectsUnexpectedContentType(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "text/plain")
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		},
	))
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())

	_, err := apiClient.Health(context.Background())
	if err == nil {
		t.Fatal("expected an error for an unexpected Content-Type")
	}
}

func TestHealthReportsInvalidResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		statusCode        int
		contentType       string
		body              string
		expectedErrorText string
	}{
		{
			name:              "unexpected HTTP status",
			statusCode:        http.StatusServiceUnavailable,
			contentType:       "application/json; charset=utf-8",
			body:              `{"error":{"code":"unavailable"}}`,
			expectedErrorText: "unexpected HTTP status 503 Service Unavailable",
		},
		{
			name:              "malformed JSON",
			statusCode:        http.StatusOK,
			contentType:       "application/json; charset=utf-8",
			body:              `{"status":`,
			expectedErrorText: "decode health response",
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(
				func(response http.ResponseWriter, _ *http.Request) {
					response.Header().Set("Content-Type", test.contentType)
					response.WriteHeader(test.statusCode)
					_, _ = response.Write([]byte(test.body))
				},
			))
			defer server.Close()

			apiClient := client.New(server.URL, server.Client())

			_, err := apiClient.Health(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}

			if !strings.Contains(err.Error(), test.expectedErrorText) {
				t.Errorf(
					"expected error containing %q, got %q",
					test.expectedErrorText,
					err,
				)
			}
		})
	}
}
func TestHealthHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set(
				"Content-Type",
				"application/json; charset=utf-8",
			)
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		},
	))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	apiClient := client.New(server.URL, server.Client())

	_, err := apiClient.Health(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation, got %v", err)
	}
}

func TestHealthHonorsHTTPTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(_ http.ResponseWriter, request *http.Request) {
			// Keep the handler active until the client timeout cancels the request.
			<-request.Context().Done()
		},
	))
	defer server.Close()

	httpClient := server.Client()
	httpClient.Timeout = 100 * time.Millisecond

	apiClient := client.New(server.URL, httpClient)

	_, err := apiClient.Health(context.Background())
	if err == nil {
		t.Fatal("expected a timeout error")
	}

	var timeoutError interface {
		Timeout() bool
	}
	if !errors.As(err, &timeoutError) || !timeoutError.Timeout() {
		t.Errorf("expected an HTTP timeout, got %v", err)
	}
}
