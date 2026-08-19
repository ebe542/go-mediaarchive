package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/api"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

const (
	defaultServerAddress = "127.0.0.1:8080"
	defaultDatabasePath  = "data/mediaarchive.db"
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

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse server arguments: %w", err)
	}

	if flags.NArg() != 0 {
		return fmt.Errorf(
			"unexpected positional arguments: %v",
			flags.Args(),
		)
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

	// Explicit timeouts protect the server from clients that keep connections
	// open without completing their requests.
	server := newHTTPServer(
		*address,
		api.NewHandler(),
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
	argAddress string,
	argCertificatePath string,
	argPrivateKeyPath string,
) error {
	_, port, err := net.SplitHostPort(argAddress)
	if err != nil || port == "" {
		return fmt.Errorf(
			"invalid server address %q: expected host and port",
			argAddress,
		)
	}

	certificateConfigured := argCertificatePath != ""
	privateKeyConfigured := argPrivateKeyPath != ""

	if certificateConfigured != privateKeyConfigured {
		return errors.New(
			"TLS certificate and private key must be configured together",
		)
	}

	if certificateConfigured {
		return nil
	}

	host, _, err := net.SplitHostPort(argAddress)
	if err != nil {
		return fmt.Errorf(
			"parse plain HTTP address %q: %w",
			argAddress,
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
	argAddress string,
	argHandler http.Handler,
	argTLSEnabled bool,
) *http.Server {
	server := &http.Server{
		Addr:              argAddress,
		Handler:           argHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	if argTLSEnabled {
		server.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS13,
		}
	}

	return server
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

func tlsCertificatePathFromEnvironment(
	argGetenv func(string) string,
) string {
	return argGetenv("MEDIAARCHIVE_TLS_CERTIFICATE")
}

func tlsPrivateKeyPathFromEnvironment(
	argGetenv func(string) string,
) string {
	return argGetenv("MEDIAARCHIVE_TLS_PRIVATE_KEY")
}
