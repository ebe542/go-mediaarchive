package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHealthCommand(t *testing.T) {

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

		t.Run(test.name, func(t *testing.T) {

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

func TestNewHTTPClientRejectsCustomCAForPlainHTTP(t *testing.T) {

	_, err := newHTTPClient(
		"http://127.0.0.1:8080",
		"test-ca.pem",
	)
	if err == nil {
		t.Fatal("expected custom CA with plain HTTP to be rejected")
	}
}

func TestNewHTTPClientTrustsCustomCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(
			argResponse http.ResponseWriter,
			argRequest *http.Request,
		) {
			argResponse.WriteHeader(http.StatusNoContent)
		},
	))
	t.Cleanup(server.Close)

	certificatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	})

	certificatePath := filepath.Join(
		t.TempDir(),
		"test-ca.pem",
	)
	if err := os.WriteFile(
		certificatePath,
		certificatePEM,
		0o600,
	); err != nil {
		t.Fatalf("write test CA certificate: %v", err)
	}

	httpClient, err := newHTTPClient(
		server.URL,
		certificatePath,
	)
	if err != nil {
		t.Fatalf("create HTTP client: %v", err)
	}

	response, err := httpClient.Get(server.URL)
	if err != nil {
		t.Fatalf("request HTTPS test server: %v", err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	})

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusNoContent,
			response.StatusCode,
		)
	}
}

func TestRunHealthCommandWithCustomCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(
			argResponse http.ResponseWriter,
			argRequest *http.Request,
		) {
			argResponse.Header().Set(
				"Content-Type",
				"application/json; charset=utf-8",
			)
			_, _ = argResponse.Write([]byte(`{"status":"ok"}`))
		},
	))
	t.Cleanup(server.Close)

	certificatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	})

	certificatePath := filepath.Join(
		t.TempDir(),
		"test-ca.pem",
	)
	if err := os.WriteFile(
		certificatePath,
		certificatePEM,
		0o600,
	); err != nil {
		t.Fatalf("write test CA certificate: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run(
		context.Background(),
		[]string{
			"--server",
			server.URL,
			"--ca-certificate",
			certificatePath,
			"health",
		},
		&stdout,
		&stderr,
		func(string) string {
			return ""
		},
	)
	if err != nil {
		t.Fatalf("run HTTPS health command: %v", err)
	}

	const expectedOutput = "Server status: ok\n"
	if stdout.String() != expectedOutput {
		t.Fatalf(
			"expected stdout %q, got %q",
			expectedOutput,
			stdout.String(),
		)
	}
}

func TestNewHTTPClientRejectsUntrustedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(
			argResponse http.ResponseWriter,
			argRequest *http.Request,
		) {
			argResponse.WriteHeader(http.StatusNoContent)
		},
	))
	t.Cleanup(server.Close)

	httpClient, err := newHTTPClient(server.URL, "")
	if err != nil {
		t.Fatalf("create default HTTP client: %v", err)
	}

	response, err := httpClient.Get(server.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("expected untrusted certificate to be rejected")
	}
}
