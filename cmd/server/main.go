package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/api"
)

const defaultServerAddress = "127.0.0.1:8080"

func main() {
	address := flag.String(
		"addr",
		addressFromEnvironment(),
		"address on which the HTTP server listens",
	)
	flag.Parse()

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
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

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

	slog.Info("server listening", "address", *address)

	if err := server.ListenAndServe(); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func addressFromEnvironment() string {
	if address := os.Getenv("MEDIAARCHIVE_ADDR"); address != "" {
		return address
	}

	return defaultServerAddress
}
