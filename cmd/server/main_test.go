package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/api"
)

func TestDatabasePathFromEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		environment  string
		expectedPath string
	}{
		{
			name:         "environment value",
			environment:  "runtime/archive.db",
			expectedPath: "runtime/archive.db",
		},
		{
			name:         "built-in default",
			environment:  "",
			expectedPath: defaultDatabasePath,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			actualPath := databasePathFromEnvironment(
				func(name string) string {
					if name == "MEDIAARCHIVE_DATABASE" {
						return test.environment
					}

					return ""
				},
			)

			if actualPath != test.expectedPath {
				t.Errorf(
					"expected database path %q, got %q",
					test.expectedPath,
					actualPath,
				)
			}
		})
	}
}

func TestValidateTransportConfiguration(t *testing.T) {
	testCases := map[string]struct {
		address       string
		certificate   string
		privateKey    string
		expectedError bool
	}{
		"IPv4 loopback HTTP": {
			address: "127.0.0.1:8080",
		},
		"IPv6 loopback HTTP": {
			address: "[::1]:8080",
		},
		"TLS network listener": {
			address:     "0.0.0.0:8443",
			certificate: "server.crt",
			privateKey:  "server.key",
		},
		"certificate without key": {
			address:       "127.0.0.1:8443",
			certificate:   "server.crt",
			expectedError: true,
		},
		"key without certificate": {
			address:       "127.0.0.1:8443",
			privateKey:    "server.key",
			expectedError: true,
		},
		"IPv4 wildcard HTTP": {
			address:       "0.0.0.0:8080",
			expectedError: true,
		},
		"IPv6 wildcard HTTP": {
			address:       "[::]:8080",
			expectedError: true,
		},
		"non-loopback HTTP": {
			address:       "192.0.2.10:8080",
			expectedError: true,
		},
		"hostname HTTP": {
			address:       "localhost:8080",
			expectedError: true,
		},
		"missing port": {
			address:       "127.0.0.1",
			expectedError: true,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			err := validateTransportConfiguration(
				testCase.address,
				testCase.certificate,
				testCase.privateKey,
			)

			if testCase.expectedError && err == nil {
				t.Fatal("expected a transport configuration error")
			}
			if !testCase.expectedError && err != nil {
				t.Fatalf(
					"expected valid transport configuration, got %v",
					err,
				)
			}
		})
	}
}

func TestNewHTTPServerConfiguresTLS13(t *testing.T) {
	handler := http.NewServeMux()

	secureServer := newHTTPServer(
		"127.0.0.1:8443",
		handler,
		true,
	)

	if secureServer.TLSConfig == nil {
		t.Fatal("expected TLS configuration")
	}
	if secureServer.TLSConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf(
			"expected minimum TLS version %d, got %d",
			tls.VersionTLS13,
			secureServer.TLSConfig.MinVersion,
		)
	}

	plainServer := newHTTPServer(
		"127.0.0.1:8080",
		handler,
		false,
	)

	if plainServer.TLSConfig != nil {
		t.Fatal("expected no TLS configuration for plain HTTP")
	}
}

func TestTLSPathsFromEnvironment(t *testing.T) {
	environment := map[string]string{
		"MEDIAARCHIVE_TLS_CERTIFICATE": "certificates/server.crt",
		"MEDIAARCHIVE_TLS_PRIVATE_KEY": "certificates/server.key",
	}

	getenv := func(argName string) string {
		return environment[argName]
	}

	if certificatePath := tlsCertificatePathFromEnvironment(
		getenv,
	); certificatePath != "certificates/server.crt" {
		t.Fatalf(
			"expected certificate path %q, got %q",
			"certificates/server.crt",
			certificatePath,
		)
	}

	if privateKeyPath := tlsPrivateKeyPathFromEnvironment(
		getenv,
	); privateKeyPath != "certificates/server.key" {
		t.Fatalf(
			"expected private-key path %q, got %q",
			"certificates/server.key",
			privateKeyPath,
		)
	}
}

func TestHTTPServerServesHealthEndpointOverTLS13(t *testing.T) {
	configuredServer := newHTTPServer(
		"127.0.0.1:0",
		api.NewHandler(),
		true,
	)

	testServer := httptest.NewUnstartedServer(
		configuredServer.Handler,
	)
	testServer.TLS = configuredServer.TLSConfig
	testServer.StartTLS()
	t.Cleanup(testServer.Close)

	response, err := testServer.Client().Get(
		testServer.URL + "/api/v1/health",
	)
	if err != nil {
		t.Fatalf("request HTTPS health endpoint: %v", err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	})

	if response.StatusCode != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			response.StatusCode,
		)
	}

	if response.TLS == nil {
		t.Fatal("expected TLS connection state")
	}
	if response.TLS.Version != tls.VersionTLS13 {
		t.Fatalf(
			"expected TLS version %d, got %d",
			tls.VersionTLS13,
			response.TLS.Version,
		)
	}
}
