package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunHealthCommand(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/api/v1/health" {
				t.Errorf(
					"expected path %q, got %q",
					"/api/v1/health",
					request.URL.Path,
				)
			}

			response.Header().Set(
				"Content-Type",
				"application/json; charset=utf-8",
			)
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		},
	))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run(
		context.Background(),
		[]string{"--server", server.URL, "health"},
		&stdout,
		&stderr,
		func(string) string {
			return ""
		},
	)
	if err != nil {
		t.Fatalf("run health command: %v", err)
	}

	const expectedOutput = "Server status: ok\n"
	if stdout.String() != expectedOutput {
		t.Errorf(
			"expected stdout %q, got %q",
			expectedOutput,
			stdout.String(),
		)
	}

	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunReturnsUsageErrorForUnknownCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run(
		context.Background(),
		[]string{"unknown"},
		&stdout,
		&stderr,
		func(string) string {
			return ""
		},
	)
	if err == nil {
		t.Fatal("expected an error for an unknown command")
	}

	if code := exitCode(err); code != 2 {
		t.Errorf("expected exit code %d, got %d", 2, code)
	}

	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf(
			"expected usage information on stderr, got %q",
			stderr.String(),
		)
	}

	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout, got %q", stdout.String())
	}
}

func TestServerURLFromEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment string
		expectedURL string
	}{
		{
			name:        "environment value",
			environment: "https://archive.example.test",
			expectedURL: "https://archive.example.test",
		},
		{
			name:        "built-in default",
			environment: "",
			expectedURL: defaultServerURL,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			actualURL := serverURLFromEnvironment(
				func(name string) string {
					if name == "MEDIAARCHIVE_SERVER" {
						return test.environment
					}

					return ""
				},
			)

			if actualURL != test.expectedURL {
				t.Errorf(
					"expected server URL %q, got %q",
					test.expectedURL,
					actualURL,
				)
			}
		})
	}
}
