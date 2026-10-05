package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/ebe542/go-mediaarchive/internal/api"
	"github.com/ebe542/go-mediaarchive/internal/application/authentication"
	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	apppasswords "github.com/ebe542/go-mediaarchive/internal/application/passwords"
	appsessions "github.com/ebe542/go-mediaarchive/internal/application/sessions"
	appusers "github.com/ebe542/go-mediaarchive/internal/application/users"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/password"
	"github.com/ebe542/go-mediaarchive/internal/session"
	filesystemstore "github.com/ebe542/go-mediaarchive/internal/storage/filesystem"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

const (
	defaultServerAddress      = "127.0.0.1:8080"
	defaultDatabasePath       = "data/mediaarchive.db"
	defaultContentDirectory   = "data/content"
	defaultMaximumUploadSize  = int64(1024 * 1024 * 1024)
	sessionAbsoluteLifetime   = 8 * time.Hour
	sessionIdleTimeout        = 30 * time.Minute
	loginLimitWindow          = 15 * time.Minute
	loginUsernameLimit        = 5
	loginIPLimit              = 20
	defaultEnrollmentLifetime = 24 * time.Hour
	enrollmentLimitWindow     = 15 * time.Minute
	enrollmentIPLimit         = 20
)

func main() {
	if err := run(os.Args[1:], os.Getenv); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string) error {
	flags := flag.NewFlagSet("mediaarchive-server", flag.ContinueOnError)

	address := flags.String(
		"addr",
		addressFromEnvironment(getenv),
		"address on which the HTTP server listens",
	)

	databasePath := flags.String(
		"database",
		databasePathFromEnvironment(getenv),
		"path to the SQLite database file",
	)

	contentDirectory := flags.String(
		"content-directory",
		contentDirectoryFromEnvironment(getenv),
		"root directory for managed media content",
	)

	maximumUploadSizeValue := flags.String(
		"maximum-upload-size",
		maximumUploadSizeDefault(getenv),
		"maximum uploaded file size in bytes",
	)

	certificatePath := flags.String(
		"tls-certificate",
		tlsCertificatePathFromEnvironment(getenv),
		"path to the TLS server certificate",
	)

	privateKeyPath := flags.String(
		"tls-private-key",
		tlsPrivateKeyPathFromEnvironment(getenv),
		"path to the TLS private key",
	)

	enrollmentLifetimeValue := flags.String(
		"password-enrollment-lifetime",
		passwordEnrollmentLifetimeDefault(getenv),
		"lifetime of one-time password enrollment tokens",
	)

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse server arguments: %w", err)
	}

	if flags.NArg() != 0 {
		return fmt.Errorf(
			"unexpected positional arguments: %v",
			flags.Args(),
		)
	}
	maximumUploadSize, err := parseMaximumUploadSize(*maximumUploadSizeValue)
	if err != nil {
		return err
	}

	enrollmentLifetime, err := parsePasswordEnrollmentLifetime(
		*enrollmentLifetimeValue,
	)
	if err != nil {
		return err
	}

	if err := validateTransportConfiguration(
		*address,
		*certificatePath,
		*privateKeyPath,
	); err != nil {
		return fmt.Errorf(
			"validate transport configuration: %w",
			err,
		)
	}

	tlsEnabled := *certificatePath != ""

	if err := createDatabaseDirectory(*databasePath); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	database, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return fmt.Errorf("initialize SQLite database: %w", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			slog.Error("database close failed", "error", err)
		}
	}()

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		return fmt.Errorf("migrate SQLite database: %w", err)
	}
	contentStore, err := filesystemstore.NewContentStore(*contentDirectory)
	if err != nil {
		return fmt.Errorf("initialize managed content store: %w", err)
	}

	handler, err := newApplicationHandlerWithContent(
		database,
		enrollmentLifetime,
		contentStore,
		maximumUploadSize,
	)
	if err != nil {
		return fmt.Errorf(
			"initialize application handler: %w",
			err,
		)
	}

	// Explicit timeouts protect the server from clients that keep connections
	// open without completing their requests.
	server := newHTTPServer(
		*address,
		handler,
		tlsEnabled,
	)

	// SIGINT and SIGTERM initiate a graceful shutdown so active requests get
	// an opportunity to finish before the process exits.
	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown failed", "error", err)
		}
	}()

	slog.Info(
		"server listening",
		"address",
		*address,
		"database",
		*databasePath,
		"content_directory",
		*contentDirectory,
		"tls",
		tlsEnabled,
	)

	var serveErr error

	if tlsEnabled {
		serveErr = server.ListenAndServeTLS(
			*certificatePath,
			*privateKeyPath,
		)
	} else {
		serveErr = server.ListenAndServe()
	}

	if serveErr != nil &&
		!errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", serveErr)
	}

	return nil
}

func validateTransportConfiguration(
	address string,
	certificatePath string,
	privateKeyPath string,
) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return fmt.Errorf(
			"invalid server address %q: expected host and port",
			address,
		)
	}

	certificateConfigured := certificatePath != ""
	privateKeyConfigured := privateKeyPath != ""

	if certificateConfigured != privateKeyConfigured {
		return errors.New(
			"TLS certificate and private key must be configured together",
		)
	}

	if certificateConfigured {
		return nil
	}

	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf(
			"parse plain HTTP address %q: %w",
			address,
			err,
		)
	}

	ipAddress := net.ParseIP(host)
	if ipAddress == nil || !ipAddress.IsLoopback() {
		return fmt.Errorf(
			"plain HTTP requires an IP loopback address, got %q",
			host,
		)
	}

	return nil
}

func newHTTPServer(
	address string,
	handler http.Handler,
	tlsEnabled bool,
) *http.Server {
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	if tlsEnabled {
		server.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS13,
		}
	}

	return server
}

func newApplicationHandler(
	database *sql.DB,
	enrollmentLifetime time.Duration,
) (http.Handler, error) {
	return newApplicationHandlerWithContent(
		database,
		enrollmentLifetime,
		nil,
		0,
	)
}

type managedContentStore interface {
	content.Store
	content.ReadStore
	content.DeletionStore
}

func newApplicationHandlerWithContent(
	database *sql.DB,
	enrollmentLifetime time.Duration,
	contentStore managedContentStore,
	maximumUploadSize int64,
) (http.Handler, error) {
	passwordHasher := password.NewDefaultHasher()

	// The dummy hash ensures that unknown-user authentication performs the same
	// Argon2id work as authentication for a stored password credential.
	dummyHash, err := passwordHasher.Hash(
		[]byte("media-archive-dummy-password"),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create dummy authentication hash: %w",
			err,
		)
	}

	userRepository := sqlitestore.NewUserRepository(database)
	userService := appusers.NewService(
		userRepository,
		uuid.NewString,
		time.Now,
	)
	credentialRepository := sqlitestore.NewPasswordCredentialRepository(database)

	authenticator := authentication.NewService(
		userRepository,
		credentialRepository,
		passwordHasher,
		dummyHash,
	)

	sessionRepository := sqlitestore.NewSessionRepository(database)
	auditRepository := sqlitestore.NewAuditRepository(database)

	sessionService := appsessions.NewService(
		authenticator,
		userRepository,
		sessionRepository,
		auditRepository,
		uuid.NewString,
		session.NewDefaultTokenGenerator(),
		time.Now,
		sessionAbsoluteLifetime,
		sessionIdleTimeout,
	)

	loginLimiter := authentication.NewAttemptLimiter(
		loginUsernameLimit,
		loginIPLimit,
		loginLimitWindow,
	)

	passwordEnrollmentService := apppasswords.NewService(
		userRepository,
		sqlitestore.NewPasswordEnrollmentRepository(database),
		credential.NewDefaultEnrollmentTokenGenerator(),
		passwordHasher,
		time.Now,
		enrollmentLifetime,
	)
	passwordEnrollmentLimiter := apppasswords.NewIPAttemptLimiter(
		enrollmentIPLimit,
		enrollmentLimitWindow,
	)
	passwordChangeService := apppasswords.NewChangeService(
		credentialRepository,
		passwordHasher,
		time.Now,
	)
	mediaRepository := sqlitestore.NewMediaRepository(database)
	grantRepository := sqlitestore.NewMediaGrantRepository(database)
	mediaService := appmedia.NewService(
		mediaRepository,
		grantRepository,
		uuid.NewString,
		time.Now,
	)
	grantService := appmedia.NewGrantService(
		mediaRepository,
		grantRepository,
		userRepository,
	)
	mediaMetadataService := api.MediaMetadataService(mediaService)
	options := []api.Option{
		api.WithAuthentication(
			sessionService,
			loginLimiter,
			time.Now,
		),
		api.WithUserReadAPI(
			sessionService,
			userService,
		),
		api.WithUserManagementAPI(
			sessionService,
			userService,
		),
		api.WithUserDirectoryAPI(
			sessionService,
			userService,
		),
		api.WithPasswordEnrollmentAPI(
			sessionService,
			passwordEnrollmentService,
			passwordEnrollmentLimiter,
			time.Now,
		),
		api.WithPasswordChangeAPI(
			sessionService,
			passwordChangeService,
		),
		api.WithMediaGrantAPI(
			sessionService,
			grantService,
		),
	}
	if contentStore != nil {
		locationRepository := sqlitestore.NewContentLocationRepository(database)
		contentReadService := appmedia.NewContentReadService(
			mediaRepository,
			grantRepository,
			locationRepository,
			contentStore,
		)
		mediaMetadataService = appmedia.NewManagedService(
			mediaService,
			locationRepository,
			mediaRepository,
			contentStore,
		)
		uploadService, err := appmedia.NewUploadService(
			mediaRepository,
			contentStore,
			uuid.NewString,
			time.Now,
			maximumUploadSize,
		)
		if err != nil {
			return nil, fmt.Errorf("initialize media upload service: %w", err)
		}
		options = append(
			options,
			api.WithMediaContentAPI(
				sessionService,
				contentReadService,
			),
			api.WithMediaUploadAPI(
				sessionService,
				uploadService,
				maximumUploadSize,
			),
		)
	}
	options = append(
		options,
		api.WithMediaMetadataAPI(
			sessionService,
			mediaMetadataService,
		),
	)

	return api.NewHandler(options...), nil
}

func createDatabaseDirectory(databasePath string) error {
	directory := filepath.Dir(databasePath)

	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf(
			"create database directory %q: %w",
			directory,
			err,
		)
	}

	return nil
}

func addressFromEnvironment(getenv func(string) string) string {
	if address := getenv("MEDIAARCHIVE_ADDR"); address != "" {
		return address
	}

	return defaultServerAddress
}

func databasePathFromEnvironment(getenv func(string) string) string {
	if databasePath := getenv("MEDIAARCHIVE_DATABASE"); databasePath != "" {
		return databasePath
	}

	return defaultDatabasePath
}

func contentDirectoryFromEnvironment(getenv func(string) string) string {
	if directory := getenv("MEDIAARCHIVE_CONTENT_DIRECTORY"); directory != "" {
		return directory
	}

	return defaultContentDirectory
}

func maximumUploadSizeDefault(getenv func(string) string) string {
	value := getenv("MEDIAARCHIVE_MAXIMUM_UPLOAD_SIZE")
	if value != "" {
		return value
	}

	return strconv.FormatInt(defaultMaximumUploadSize, 10)
}

func parseMaximumUploadSize(value string) (int64, error) {
	maximumSize, err := strconv.ParseInt(value, 10, 64)
	if err != nil || maximumSize <= 0 {
		return 0, fmt.Errorf(
			"invalid maximum upload size %q: expected a positive byte count",
			value,
		)
	}

	return maximumSize, nil
}

func tlsCertificatePathFromEnvironment(
	getenv func(string) string,
) string {
	return getenv("MEDIAARCHIVE_TLS_CERTIFICATE")
}

func tlsPrivateKeyPathFromEnvironment(
	getenv func(string) string,
) string {
	return getenv("MEDIAARCHIVE_TLS_PRIVATE_KEY")
}

func passwordEnrollmentLifetimeDefault(
	getenv func(string) string,
) string {
	value := getenv("MEDIAARCHIVE_PASSWORD_ENROLLMENT_LIFETIME")
	if value != "" {
		return value
	}

	return defaultEnrollmentLifetime.String()
}

func parsePasswordEnrollmentLifetime(
	value string,
) (time.Duration, error) {
	lifetime, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf(
			"parse password enrollment lifetime: %w",
			err,
		)
	}
	if lifetime <= 0 {
		return 0, errors.New(
			"password enrollment lifetime must be positive",
		)
	}

	return lifetime, nil
}
