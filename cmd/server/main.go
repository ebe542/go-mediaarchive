package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
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

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse server arguments: %w", err)
	}

	if flags.NArg() != 0 {
		return fmt.Errorf(
			"unexpected positional arguments: %v",
			flags.Args(),
		)
	}

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
	server := &http.Server{
		Addr:              *address,
		Handler:           api.NewHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

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
	)

	if err := server.ListenAndServe(); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}

	return nil
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
