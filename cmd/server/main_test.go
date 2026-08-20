package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/api"
	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
	"github.com/ebe542/go-mediaarchive/internal/session"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
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

func TestNewApplicationHandlerRejectsUnknownLogin(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	handler, err := newApplicationHandler(database)
	if err != nil {
		t.Fatalf("create application handler: %v", err)
	}

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
			"expected status %d, got %d: %s",
			http.StatusUnauthorized,
			response.Code,
			response.Body.String(),
		)
	}
}

func TestApplicationHandlerAuthenticatesAndStoresOnlyTokenHash(
	t *testing.T,
) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	now := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"
	plainPassword := []byte("synthetic passphrase")

	user, err := identity.NewUser(
		userID,
		"archive_admin",
		"Archive Administrator",
		identity.RoleAdmin,
		now,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	encodedHash, err := password.NewDefaultHasher().Hash(
		plainPassword,
	)
	if err != nil {
		t.Fatalf("hash password fixture: %v", err)
	}

	passwordCredential, err := credential.NewPasswordCredential(
		userID,
		encodedHash,
		now,
	)
	if err != nil {
		t.Fatalf("create credential fixture: %v", err)
	}

	if err := sqlitestore.NewAdminBootstrapRepository(
		database,
	).BootstrapAdmin(
		ctx,
		user,
		passwordCredential,
	); err != nil {
		t.Fatalf("store authentication fixture: %v", err)
	}

	handler, err := newApplicationHandler(database)
	if err != nil {
		t.Fatalf("create application handler: %v", err)
	}

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

	if response.Code != http.StatusCreated {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusCreated,
			response.Code,
			response.Body.String(),
		)
	}

	var body struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if body.AccessToken == "" {
		t.Fatal("expected non-empty access token")
	}

	var storedTokenHash []byte
	err = database.QueryRowContext(
		ctx,
		`SELECT token_hash FROM sessions`,
	).Scan(&storedTokenHash)
	if err != nil {
		t.Fatalf("read stored session token hash: %v", err)
	}

	expectedTokenHash := session.HashToken(body.AccessToken)
	if !bytes.Equal(storedTokenHash, expectedTokenHash[:]) {
		t.Fatal("expected only the access-token hash to be stored")
	}

	if bytes.Equal(storedTokenHash, []byte(body.AccessToken)) {
		t.Fatal("expected raw access token not to be stored")
	}
}
