package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
)

const defaultServerURL = "http://127.0.0.1:8080"

const (
	exitCodeFailure = 1
	exitCodeUsage   = 2
)

type usageError struct {
	err error
}

func (err *usageError) Error() string {
	return err.err.Error()
}

func (err *usageError) Unwrap() error {
	return err.err
}

func main() {
	// Cancel active HTTP requests when the user interrupts the CLI.
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
	)
	defer stop()

	if err := run(
		ctx,
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		os.Getenv,
	); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	var target *usageError
	if errors.As(err, &target) {
		return exitCodeUsage
	}

	return exitCodeFailure
}

func run(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
) error {
	flags := flag.NewFlagSet("mediaarchive", flag.ContinueOnError)
	flags.SetOutput(stderr)

	serverURL := flags.String(
		"server",
		serverURLFromEnvironment(getenv),
		"base URL of the Media Archive server",
	)

	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mediaarchive [options] health")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Options:")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return &usageError{
			err: fmt.Errorf("parse arguments: %w", err),
		}
	}

	if flags.NArg() != 1 {
		flags.Usage()

		return &usageError{
			err: errors.New("exactly one command is required"),
		}
	}

	switch flags.Arg(0) {
	case "health":
		status, err := apiclient.New(*serverURL, nil).Health(ctx)
		if err != nil {
			return fmt.Errorf("check server health: %w", err)
		}

		fmt.Fprintf(stdout, "Server status: %s\n", status.Status)

		return nil
	default:
		flags.Usage()

		return &usageError{
			err: fmt.Errorf("unknown command %q", flags.Arg(0)),
		}
	}
}

func serverURLFromEnvironment(getenv func(string) string) string {
	if serverURL := getenv("MEDIAARCHIVE_SERVER"); serverURL != "" {
		return serverURL
	}

	return defaultServerURL
}
