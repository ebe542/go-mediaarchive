package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	sharedcli "github.com/ebe542/go-mediaarchive/internal/cli"
	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
)

type userAPI interface {
	Health(context.Context) (apiclient.HealthStatus, error)
	Login(context.Context, string, []byte) (apiclient.Session, error)
	Logout(context.Context, string) error
	CurrentUser(context.Context, string) (apiclient.User, error)
	CompletePasswordEnrollment(context.Context, string, []byte) error
	ChangePassword(context.Context, string, []byte, []byte) error
}

type secretReader = sharedcli.SecretReader

type userConsole struct {
	api           userAPI
	input         *sharedcli.LineReader
	output        io.Writer
	errorOutput   io.Writer
	readSecret    sharedcli.SecretReader
	logoutTimeout time.Duration
	session       *sharedcli.Session
}

func newUserConsole(
	api userAPI,
	input io.Reader,
	output io.Writer,
	errorOutput io.Writer,
	readSecret secretReader,
	logoutTimeout time.Duration,
) *userConsole {
	return &userConsole{
		api:           api,
		input:         sharedcli.NewLineReader(input),
		output:        output,
		errorOutput:   errorOutput,
		readSecret:    readSecret,
		logoutTimeout: logoutTimeout,
		session:       sharedcli.NewSession("mediaarchive"),
	}
}

// run processes commands until the user exits or the context is canceled.
func (console *userConsole) run(ctx context.Context) error {
	fmt.Fprintln(console.output, "Media Archive interactive client")
	fmt.Fprintln(console.output, "Type 'help' to list available commands.")

	defer console.logoutOnExit()
	for {
		line, available, err := console.input.Read(
			ctx,
			console.output,
			console.session.Prompt(),
		)
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(console.output)

			return nil
		}
		if err != nil {
			return fmt.Errorf("read command: %w", err)
		}
		if !available {
			return nil
		}

		exit, err := console.execute(ctx, line)
		if err != nil {
			console.printError(err)
		}
		if exit {
			return nil
		}
	}
}

func (console *userConsole) execute(
	ctx context.Context,
	line string,
) (bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false, nil
	}

	switch fields[0] {
	case "help":
		if len(fields) != 1 {
			return false, commandUsage("help")
		}
		console.printHelp()

		return false, nil
	case "health":
		if len(fields) != 1 {
			return false, commandUsage("health")
		}

		return false, console.health(ctx)
	case "login":
		if len(fields) != 2 {
			return false, commandUsage("login <username>")
		}

		return false, console.login(ctx, fields[1])
	case "logout":
		if len(fields) != 1 {
			return false, commandUsage("logout")
		}

		return false, console.logout(ctx)
	case "me":
		if len(fields) != 1 {
			return false, commandUsage("me")
		}

		return false, console.me(ctx)
	case "password":
		return false, console.password(ctx, fields[1:])
	case "exit", "quit", "bye":
		if len(fields) != 1 {
			return false, commandUsage(fields[0])
		}

		return true, nil
	default:
		return false, fmt.Errorf("unknown command %q", fields[0])
	}
}

func (console *userConsole) health(ctx context.Context) error {
	status, err := console.api.Health(ctx)
	if err != nil {
		return fmt.Errorf("check server health: %w", err)
	}
	fmt.Fprintf(console.output, "Server status: %s\n", status.Status)

	return nil
}

func (console *userConsole) login(
	ctx context.Context,
	username string,
) error {
	if console.session.Authenticated() {
		return errors.New("already logged in; log out before starting another session")
	}

	password, err := sharedcli.ReadRequiredSecret(
		console.readSecret,
		"Password: ",
		console.printError,
	)
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	defer sharedcli.ClearSecret(password)

	session, err := console.api.Login(ctx, username, password)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	user, err := console.api.CurrentUser(ctx, session.AccessToken)
	if err != nil {
		console.revokeUnusableSession(session.AccessToken)

		return fmt.Errorf("get authenticated user: %w", err)
	}
	console.session.Set(user.Username, session.AccessToken)
	fmt.Fprintf(console.output, "Logged in as %s.\n", user.Username)

	return nil
}

func (console *userConsole) logout(ctx context.Context) error {
	if !console.session.Authenticated() {
		return errors.New("not logged in")
	}
	if err := console.api.Logout(ctx, console.session.AccessToken()); err != nil {
		return fmt.Errorf("logout: %w", err)
	}

	console.session.Clear()
	fmt.Fprintln(console.output, "Logged out.")

	return nil
}

func (console *userConsole) revokeUnusableSession(accessToken string) {
	ctx, cancel := context.WithTimeout(context.Background(), console.logoutTimeout)
	defer cancel()
	if err := console.api.Logout(ctx, accessToken); err != nil {
		console.printError(fmt.Errorf("revoke unusable session: %w", err))
	}
}

func (console *userConsole) logoutOnExit() {
	if !console.session.Authenticated() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), console.logoutTimeout)
	defer cancel()
	if err := console.api.Logout(ctx, console.session.AccessToken()); err != nil {
		console.printError(fmt.Errorf("logout during exit: %w", err))
	}
	console.session.Clear()
}

func (console *userConsole) me(ctx context.Context) error {
	if !console.session.Authenticated() {
		return errors.New("not logged in")
	}

	user, err := console.api.CurrentUser(ctx, console.session.AccessToken())
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}
	sharedcli.PrintUser(console.output, user)

	return nil
}

func (console *userConsole) password(
	ctx context.Context,
	arguments []string,
) error {
	if len(arguments) != 1 {
		return commandUsage("password enroll|change")
	}

	switch arguments[0] {
	case "enroll":
		return console.enrollPassword(ctx)
	case "change":
		return console.changePassword(ctx)
	default:
		return commandUsage("password enroll|change")
	}
}

func (console *userConsole) enrollPassword(ctx context.Context) error {
	if console.session.Authenticated() {
		return errors.New("log out before enrolling a password")
	}

	token, err := sharedcli.ReadRequiredSecret(
		console.readSecret,
		"Enrollment token: ",
		console.printError,
	)
	if err != nil {
		return fmt.Errorf("read enrollment token: %w", err)
	}
	defer sharedcli.ClearSecret(token)

	password, err := sharedcli.ReadConfirmedSecret(
		console.readSecret,
		console.printError,
	)
	if err != nil {
		return err
	}
	defer sharedcli.ClearSecret(password)

	if err := console.api.CompletePasswordEnrollment(
		ctx,
		string(token),
		password,
	); err != nil {
		return fmt.Errorf("enroll password: %w", err)
	}
	fmt.Fprintln(console.output, "Password enrolled. You can now log in.")

	return nil
}

func (console *userConsole) changePassword(ctx context.Context) error {
	if !console.session.Authenticated() {
		return errors.New("not logged in")
	}

	currentPassword, err := sharedcli.ReadRequiredSecret(
		console.readSecret,
		"Current password: ",
		console.printError,
	)
	if err != nil {
		return fmt.Errorf("read current password: %w", err)
	}
	defer sharedcli.ClearSecret(currentPassword)

	newPassword, err := sharedcli.ReadConfirmedSecret(
		console.readSecret,
		console.printError,
	)
	if err != nil {
		return err
	}
	defer sharedcli.ClearSecret(newPassword)

	if err := console.api.ChangePassword(
		ctx,
		console.session.AccessToken(),
		currentPassword,
		newPassword,
	); err != nil {
		return fmt.Errorf("change password: %w", err)
	}

	// The server revokes every user session after a successful password change.
	console.session.Clear()
	fmt.Fprintln(console.output, "Password changed. Log in again.")

	return nil
}

func (console *userConsole) printHelp() {
	fmt.Fprintln(console.output, "Commands:")
	fmt.Fprintln(console.output, "  help")
	fmt.Fprintln(console.output, "  health")
	fmt.Fprintln(console.output, "  login <username>")
	fmt.Fprintln(console.output, "  logout")
	fmt.Fprintln(console.output, "  me")
	fmt.Fprintln(console.output, "  password enroll")
	fmt.Fprintln(console.output, "  password change")
	fmt.Fprintln(console.output, "  exit | quit | bye")
}

func (console *userConsole) printError(inputError error) {
	sharedcli.PrintError(console.errorOutput, inputError)
}

func commandUsage(usage string) error {
	return fmt.Errorf("usage: %s", usage)
}
