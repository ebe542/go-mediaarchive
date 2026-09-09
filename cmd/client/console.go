package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
)

const anonymousUsername = "anonymous"

type userAPI interface {
	Health(context.Context) (apiclient.HealthStatus, error)
	Login(context.Context, string, []byte) (apiclient.Session, error)
	Logout(context.Context, string) error
	CurrentUser(context.Context, string) (apiclient.User, error)
	CompletePasswordEnrollment(context.Context, string, []byte) error
	ChangePassword(context.Context, string, []byte, []byte) error
}

type secretReader func(string) ([]byte, error)

type userConsole struct {
	api           userAPI
	input         io.Reader
	output        io.Writer
	errorOutput   io.Writer
	readSecret    secretReader
	logoutTimeout time.Duration
	accessToken   string
	username      string
}

func newUserConsole(
	argAPI userAPI,
	argInput io.Reader,
	argOutput io.Writer,
	argErrorOutput io.Writer,
	argReadSecret secretReader,
	argLogoutTimeout time.Duration,
) *userConsole {
	return &userConsole{
		api:           argAPI,
		input:         argInput,
		output:        argOutput,
		errorOutput:   argErrorOutput,
		readSecret:    argReadSecret,
		logoutTimeout: argLogoutTimeout,
	}
}

// run processes commands until the user exits or the context is canceled.
func (console *userConsole) run(argContext context.Context) error {
	fmt.Fprintln(console.output, "Media Archive interactive client")
	fmt.Fprintln(console.output, "Type 'help' to list available commands.")

	scanner := bufio.NewScanner(console.input)
	defer console.logoutOnExit()
	for {
		fmt.Fprint(console.output, console.prompt())

		line, available, err := scanCommand(argContext, scanner)
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

		exit, err := console.execute(argContext, line)
		if err != nil {
			console.printError(err)
		}
		if exit {
			return nil
		}
	}
}

func (console *userConsole) prompt() string {
	username := console.username
	if username == "" {
		username = anonymousUsername
	}

	return fmt.Sprintf("%s@mediaarchive> ", username)
}

type commandScan struct {
	line      string
	available bool
	err       error
}

func scanCommand(
	argContext context.Context,
	argScanner *bufio.Scanner,
) (string, bool, error) {
	result := make(chan commandScan, 1)
	go func() {
		available := argScanner.Scan()
		result <- commandScan{
			line:      argScanner.Text(),
			available: available,
			err:       argScanner.Err(),
		}
	}()

	select {
	case <-argContext.Done():
		return "", false, argContext.Err()
	case scanned := <-result:
		return scanned.line, scanned.available, scanned.err
	}
}

func (console *userConsole) execute(
	argContext context.Context,
	argLine string,
) (bool, error) {
	fields := strings.Fields(argLine)
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

		return false, console.health(argContext)
	case "login":
		if len(fields) != 2 {
			return false, commandUsage("login <username>")
		}

		return false, console.login(argContext, fields[1])
	case "logout":
		if len(fields) != 1 {
			return false, commandUsage("logout")
		}

		return false, console.logout(argContext)
	case "me":
		if len(fields) != 1 {
			return false, commandUsage("me")
		}

		return false, console.me(argContext)
	case "password":
		return false, console.password(argContext, fields[1:])
	case "exit", "quit", "bye":
		if len(fields) != 1 {
			return false, commandUsage(fields[0])
		}

		return true, nil
	default:
		return false, fmt.Errorf("unknown command %q", fields[0])
	}
}

func (console *userConsole) health(argContext context.Context) error {
	status, err := console.api.Health(argContext)
	if err != nil {
		return fmt.Errorf("check server health: %w", err)
	}
	fmt.Fprintf(console.output, "Server status: %s\n", status.Status)

	return nil
}

func (console *userConsole) login(
	argContext context.Context,
	argUsername string,
) error {
	if console.accessToken != "" {
		return errors.New("already logged in; log out before starting another session")
	}

	password, err := console.readRequiredSecret("Password: ")
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	defer clearSecret(password)

	session, err := console.api.Login(argContext, argUsername, password)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	user, err := console.api.CurrentUser(argContext, session.AccessToken)
	if err != nil {
		console.revokeUnusableSession(session.AccessToken)

		return fmt.Errorf("get authenticated user: %w", err)
	}
	console.accessToken = session.AccessToken
	console.username = user.Username
	fmt.Fprintf(console.output, "Logged in as %s.\n", user.Username)

	return nil
}

func (console *userConsole) logout(argContext context.Context) error {
	if console.accessToken == "" {
		return errors.New("not logged in")
	}
	if err := console.api.Logout(argContext, console.accessToken); err != nil {
		return fmt.Errorf("logout: %w", err)
	}

	console.clearSession()
	fmt.Fprintln(console.output, "Logged out.")

	return nil
}

func (console *userConsole) revokeUnusableSession(argAccessToken string) {
	ctx, cancel := context.WithTimeout(context.Background(), console.logoutTimeout)
	defer cancel()
	if err := console.api.Logout(ctx, argAccessToken); err != nil {
		console.printError(fmt.Errorf("revoke unusable session: %w", err))
	}
}

func (console *userConsole) logoutOnExit() {
	if console.accessToken == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), console.logoutTimeout)
	defer cancel()
	if err := console.api.Logout(ctx, console.accessToken); err != nil {
		console.printError(fmt.Errorf("logout during exit: %w", err))
	}
	console.clearSession()
}

func (console *userConsole) clearSession() {
	console.accessToken = ""
	console.username = ""
}

func (console *userConsole) me(argContext context.Context) error {
	if console.accessToken == "" {
		return errors.New("not logged in")
	}

	user, err := console.api.CurrentUser(argContext, console.accessToken)
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}
	printUser(console.output, user)

	return nil
}

func (console *userConsole) password(
	argContext context.Context,
	argArguments []string,
) error {
	if len(argArguments) != 1 {
		return commandUsage("password enroll|change")
	}

	switch argArguments[0] {
	case "enroll":
		return console.enrollPassword(argContext)
	case "change":
		return console.changePassword(argContext)
	default:
		return commandUsage("password enroll|change")
	}
}

func (console *userConsole) enrollPassword(argContext context.Context) error {
	if console.accessToken != "" {
		return errors.New("log out before enrolling a password")
	}

	token, err := console.readRequiredSecret("Enrollment token: ")
	if err != nil {
		return fmt.Errorf("read enrollment token: %w", err)
	}
	defer clearSecret(token)

	password, err := console.confirmedPassword()
	if err != nil {
		return err
	}
	defer clearSecret(password)

	if err := console.api.CompletePasswordEnrollment(
		argContext,
		string(token),
		password,
	); err != nil {
		return fmt.Errorf("enroll password: %w", err)
	}
	fmt.Fprintln(console.output, "Password enrolled. You can now log in.")

	return nil
}

func (console *userConsole) changePassword(argContext context.Context) error {
	if console.accessToken == "" {
		return errors.New("not logged in")
	}

	currentPassword, err := console.readRequiredSecret("Current password: ")
	if err != nil {
		return fmt.Errorf("read current password: %w", err)
	}
	defer clearSecret(currentPassword)

	newPassword, err := console.confirmedPassword()
	if err != nil {
		return err
	}
	defer clearSecret(newPassword)

	if err := console.api.ChangePassword(
		argContext,
		console.accessToken,
		currentPassword,
		newPassword,
	); err != nil {
		return fmt.Errorf("change password: %w", err)
	}

	// The server revokes every user session after a successful password change.
	console.clearSession()
	fmt.Fprintln(console.output, "Password changed. Log in again.")

	return nil
}

func (console *userConsole) confirmedPassword() ([]byte, error) {
	for {
		password, err := console.readRequiredSecret("New password: ")
		if err != nil {
			return nil, fmt.Errorf("read new password: %w", err)
		}

		confirmation, err := console.readRequiredSecret("Confirm new password: ")
		if err != nil {
			clearSecret(password)

			return nil, fmt.Errorf("read password confirmation: %w", err)
		}

		if equalSecrets(password, confirmation) {
			clearSecret(confirmation)

			return password, nil
		}

		clearSecret(password)
		clearSecret(confirmation)
		console.printError(errors.New("password confirmation does not match; try again"))
	}
}

func (console *userConsole) readRequiredSecret(
	argPrompt string,
) ([]byte, error) {
	for {
		secret, err := console.readSecret(argPrompt)
		if err != nil {
			return nil, err
		}
		if len(secret) != 0 {
			return secret, nil
		}

		clearSecret(secret)
		console.printError(errors.New("value is required; try again"))
	}
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

func (console *userConsole) printError(argError error) {
	var apiError *apiclient.APIError
	if errors.As(argError, &apiError) {
		fmt.Fprintf(
			console.errorOutput,
			"Error [%s]: %s\n",
			apiError.Code,
			apiError.Message,
		)

		return
	}
	fmt.Fprintf(console.errorOutput, "Error: %v\n", argError)
}

func commandUsage(argUsage string) error {
	return fmt.Errorf("usage: %s", argUsage)
}

func printUser(argOutput io.Writer, argUser apiclient.User) {
	fmt.Fprintf(argOutput, "ID: %s\n", argUser.ID)
	fmt.Fprintf(argOutput, "Username: %s\n", argUser.Username)
	fmt.Fprintf(argOutput, "Display name: %s\n", argUser.DisplayName)
	fmt.Fprintf(argOutput, "Role: %s\n", argUser.Role)
	fmt.Fprintf(argOutput, "Active: %t\n", argUser.Active)
	fmt.Fprintf(argOutput, "Created: %s\n", formatLocalTime(argUser.CreatedAt))
	fmt.Fprintf(argOutput, "Updated: %s\n", formatLocalTime(argUser.UpdatedAt))
}

func formatLocalTime(argTime time.Time) string {
	return argTime.Local().Format(time.RFC3339)
}

func equalSecrets(argFirst []byte, argSecond []byte) bool {
	return subtle.ConstantTimeCompare(argFirst, argSecond) == 1
}

func clearSecret(argSecret []byte) {
	for index := range argSecret {
		argSecret[index] = 0
	}
}
