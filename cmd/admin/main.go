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
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

const defaultDatabasePath = "data/mediaarchive.db"

type passwordReader func() ([]byte, error)

type bootstrapAdminFunc func(
	argContext context.Context,
	argDatabasePath string,
	argUsername string,
	argDisplayName string,
	argPassword []byte,
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
	argContext context.Context,
	argDatabasePath string,
	argUsername string,
	argDisplayName string,
	argPassword []byte,
) (identity.User, error) {
	if err := createDatabaseDirectory(argDatabasePath); err != nil {
		return identity.User{}, err
	}

	database, err := sqlitestore.Open(argContext, argDatabasePath)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"open bootstrap database: %w",
			err,
		)
	}

	if err := sqlitestore.Migrate(argContext, database); err != nil {
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
		argContext,
		adminbootstrap.Input{
			Username:    argUsername,
			DisplayName: argDisplayName,
			Password:    argPassword,
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

func createDatabaseDirectory(argDatabasePath string) error {
	directory := filepath.Dir(argDatabasePath)

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
	argContext context.Context,
	argArguments []string,
	argStdout io.Writer,
	argStderr io.Writer,
	argReadPassword passwordReader,
	argBootstrapAdmin bootstrapAdminFunc,
) error {
	if len(argArguments) == 0 ||
		argArguments[0] != "bootstrap" {
		printUsage(argStderr)

		return errors.New("expected the bootstrap command")
	}

	flags := flag.NewFlagSet(
		"mediaarchive-admin bootstrap",
		flag.ContinueOnError,
	)
	flags.SetOutput(argStderr)

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
		printUsage(argStderr)
		flags.PrintDefaults()
	}

	if err := flags.Parse(argArguments[1:]); err != nil {
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

	fmt.Fprint(argStderr, "Password: ")
	plainPassword, err := argReadPassword()
	fmt.Fprintln(argStderr)
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	defer clearBytes(plainPassword)

	fmt.Fprint(argStderr, "Confirm password: ")
	confirmedPassword, err := argReadPassword()
	fmt.Fprintln(argStderr)
	if err != nil {
		return fmt.Errorf("read password confirmation: %w", err)
	}
	defer clearBytes(confirmedPassword)

	if subtle.ConstantTimeCompare(
		plainPassword,
		confirmedPassword,
	) != 1 {
		return errors.New("password confirmation does not match")
	}

	adminUser, err := argBootstrapAdmin(
		argContext,
		*databasePath,
		*username,
		*displayName,
		plainPassword,
	)
	if err != nil {
		return fmt.Errorf("bootstrap administrator: %w", err)
	}

	fmt.Fprintf(
		argStdout,
		"Administrator %q created.\n",
		adminUser.Username,
	)

	return nil
}

func printUsage(argOutput io.Writer) {
	fmt.Fprintln(
		argOutput,
		"Usage: mediaarchive-admin bootstrap [options]",
	)
}

func clearBytes(argValue []byte) {
	for index := range argValue {
		argValue[index] = 0
	}
}
