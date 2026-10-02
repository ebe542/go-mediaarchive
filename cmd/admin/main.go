package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"golang.org/x/term"

	adminbootstrap "github.com/ebe542/go-mediaarchive/internal/application/bootstrap"
	sharedcli "github.com/ebe542/go-mediaarchive/internal/cli"
	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

const (
	defaultDatabasePath = "data/mediaarchive.db"
	defaultServerURL    = "http://127.0.0.1:8080"
)

type passwordReader func() ([]byte, error)

type bootstrapAdminFunc func(
	ctx context.Context,
	databasePath string,
	username string,
	displayName string,
	passwordValue []byte,
) (identity.User, error)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
	)
	defer stop()

	err := run(
		ctx,
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		readTerminalPassword,
		bootstrapAdministrator,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func readTerminalPassword() ([]byte, error) {
	passwordValue, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return nil, fmt.Errorf("read password from terminal: %w", err)
	}

	return passwordValue, nil
}

func bootstrapAdministrator(
	ctx context.Context,
	databasePath string,
	username string,
	displayName string,
	passwordValue []byte,
) (identity.User, error) {
	if err := createDatabaseDirectory(databasePath); err != nil {
		return identity.User{}, err
	}

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"open bootstrap database: %w",
			err,
		)
	}

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		_ = database.Close()

		return identity.User{}, fmt.Errorf(
			"migrate bootstrap database: %w",
			err,
		)
	}

	service := adminbootstrap.NewService(
		sqlitestore.NewAdminBootstrapRepository(database),
		password.NewDefaultHasher(),
		uuid.NewString,
		time.Now,
	)

	adminUser, bootstrapErr := service.BootstrapAdmin(
		ctx,
		adminbootstrap.Input{
			Username:    username,
			DisplayName: displayName,
			Password:    passwordValue,
		},
	)

	closeErr := database.Close()

	if bootstrapErr != nil {
		return identity.User{}, bootstrapErr
	}
	if closeErr != nil {
		return identity.User{}, fmt.Errorf(
			"close bootstrap database: %w",
			closeErr,
		)
	}

	return adminUser, nil
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

func run(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	readPassword passwordReader,
	bootstrapAdmin bootstrapAdminFunc,
) error {
	if len(arguments) > 0 && arguments[0] == "bootstrap" {
		return runBootstrap(
			ctx,
			arguments[1:],
			stdout,
			stderr,
			readPassword,
			bootstrapAdmin,
		)
	}

	flags := flag.NewFlagSet("mediaarchive-admin", flag.ContinueOnError)
	flags.SetOutput(stderr)
	serverURL := flags.String(
		"server",
		serverURLFromEnvironment(os.Getenv),
		"base URL of the Media Archive server",
	)
	caCertificatePath := flags.String(
		"ca-certificate",
		"",
		"path to an additional trusted CA certificate",
	)
	flags.Usage = func() {
		printUsage(stderr)
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse arguments: %w", err)
	}
	if flags.NArg() != 0 {
		flags.Usage()

		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}

	httpClient, err := apiclient.NewHTTPClient(*serverURL, *caCertificatePath)
	if err != nil {
		return fmt.Errorf("configure HTTP client: %w", err)
	}

	console := newAdminConsole(
		apiclient.New(*serverURL, httpClient),
		os.Stdin,
		stdout,
		stderr,
		func(prompt string) ([]byte, error) {
			fmt.Fprint(stdout, prompt)
			secret, err := readPassword()
			fmt.Fprintln(stdout)

			return secret, err
		},
		5*time.Second,
	)

	return console.run(ctx)
}

func runBootstrap(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	readPassword passwordReader,
	bootstrapAdmin bootstrapAdminFunc,
) error {

	flags := flag.NewFlagSet(
		"mediaarchive-admin bootstrap",
		flag.ContinueOnError,
	)
	flags.SetOutput(stderr)

	databasePath := flags.String(
		"database",
		defaultDatabasePath,
		"path to the SQLite database file",
	)
	username := flags.String(
		"username",
		"",
		"username for the initial administrator",
	)
	displayName := flags.String(
		"display-name",
		"",
		"display name for the initial administrator",
	)

	flags.Usage = func() {
		printUsage(stderr)
		flags.PrintDefaults()
	}

	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse bootstrap arguments: %w", err)
	}
	if flags.NArg() != 0 {
		flags.Usage()

		return fmt.Errorf(
			"unexpected positional arguments: %v",
			flags.Args(),
		)
	}
	if *username == "" {
		return errors.New("username is required")
	}
	if *displayName == "" {
		return errors.New("display name is required")
	}

	fmt.Fprint(stderr, "Password: ")
	plainPassword, err := readPassword()
	fmt.Fprintln(stderr)
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	defer sharedcli.ClearSecret(plainPassword)

	fmt.Fprint(stderr, "Confirm password: ")
	confirmedPassword, err := readPassword()
	fmt.Fprintln(stderr)
	if err != nil {
		return fmt.Errorf("read password confirmation: %w", err)
	}
	defer sharedcli.ClearSecret(confirmedPassword)

	if subtle.ConstantTimeCompare(
		plainPassword,
		confirmedPassword,
	) != 1 {
		return errors.New("password confirmation does not match")
	}

	adminUser, err := bootstrapAdmin(
		ctx,
		*databasePath,
		*username,
		*displayName,
		plainPassword,
	)
	if err != nil {
		return fmt.Errorf("bootstrap administrator: %w", err)
	}

	fmt.Fprintf(
		stdout,
		"Administrator %q created.\n",
		adminUser.Username,
	)

	return nil
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  mediaarchive-admin [options]")
	fmt.Fprintln(output, "  mediaarchive-admin bootstrap [options]")
}

func serverURLFromEnvironment(getenv func(string) string) string {
	if serverURL := getenv("MEDIAARCHIVE_SERVER"); serverURL != "" {
		return serverURL
	}

	return defaultServerURL
}
