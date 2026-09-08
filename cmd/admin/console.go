package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

const (
	adminConsolePrompt = "mediaarchive-admin> "
	defaultUserLimit   = 50
	maximumUserLimit   = 100
)

type adminAPI interface {
	Health(context.Context) (apiclient.HealthStatus, error)
	Login(context.Context, string, []byte) (apiclient.Session, error)
	Logout(context.Context, string) error
	CurrentUser(context.Context, string) (apiclient.User, error)
	UserByID(context.Context, string, string) (apiclient.User, error)
	ListUsers(context.Context, string, int, string) (apiclient.UserPage, error)
	CreateUser(context.Context, string, apiclient.UserInput) (apiclient.User, error)
	UpdateUser(context.Context, string, string, apiclient.UserInput) (apiclient.User, error)
	SetUserActive(context.Context, string, string, bool) (apiclient.User, error)
	IssuePasswordEnrollment(context.Context, string, string) (apiclient.PasswordEnrollment, error)
	ChangePassword(context.Context, string, []byte, []byte) error
}

type secretReader func(string) ([]byte, error)

type adminConsole struct {
	api           adminAPI
	scanner       *bufio.Scanner
	output        io.Writer
	errorOutput   io.Writer
	readSecret    secretReader
	logoutTimeout time.Duration
	accessToken   string
	pageLimit     int
	nextCursor    string
	pageStarted   bool
}

func newAdminConsole(
	argAPI adminAPI,
	argInput io.Reader,
	argOutput io.Writer,
	argErrorOutput io.Writer,
	argReadSecret secretReader,
	argLogoutTimeout time.Duration,
) *adminConsole {
	return &adminConsole{
		api:           argAPI,
		scanner:       bufio.NewScanner(argInput),
		output:        argOutput,
		errorOutput:   argErrorOutput,
		readSecret:    argReadSecret,
		logoutTimeout: argLogoutTimeout,
		pageLimit:     defaultUserLimit,
	}
}

// run processes administrator commands until the console is closed.
func (console *adminConsole) run(argContext context.Context) error {
	fmt.Fprintln(console.output, "Media Archive administrator console")
	fmt.Fprintln(console.output, "Type 'help' to list available commands.")
	defer console.logoutOnExit()

	for {
		line, available, err := console.readCommand(
			argContext,
			adminConsolePrompt,
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

		exit, err := console.execute(argContext, line)
		if err != nil {
			console.printError(err)
		}
		if exit {
			return nil
		}
	}
}

func (console *adminConsole) readCommand(
	argContext context.Context,
	argPrompt string,
) (string, bool, error) {
	fmt.Fprint(console.output, argPrompt)

	type scanResult struct {
		line      string
		available bool
		err       error
	}
	result := make(chan scanResult, 1)
	go func() {
		available := console.scanner.Scan()
		result <- scanResult{
			line:      console.scanner.Text(),
			available: available,
			err:       console.scanner.Err(),
		}
	}()

	select {
	case <-argContext.Done():
		return "", false, argContext.Err()
	case scanned := <-result:
		return scanned.line, scanned.available, scanned.err
	}
}

func (console *adminConsole) execute(
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
			return false, adminCommandUsage("help")
		}
		console.printHelp()

		return false, nil
	case "health":
		if len(fields) != 1 {
			return false, adminCommandUsage("health")
		}

		return false, console.health(argContext)
	case "login":
		if len(fields) != 2 {
			return false, adminCommandUsage("login <username>")
		}

		return false, console.login(argContext, fields[1])
	case "logout":
		if len(fields) != 1 {
			return false, adminCommandUsage("logout")
		}

		return false, console.logout(argContext)
	case "me":
		if len(fields) != 1 {
			return false, adminCommandUsage("me")
		}

		return false, console.me(argContext)
	case "user":
		return false, console.user(argContext, fields[1:])
	case "password":
		return false, console.password(argContext, fields[1:])
	case "exit", "quit", "bye":
		if len(fields) != 1 {
			return false, adminCommandUsage(fields[0])
		}

		return true, nil
	default:
		return false, fmt.Errorf("unknown command %q", fields[0])
	}
}

func (console *adminConsole) health(argContext context.Context) error {
	status, err := console.api.Health(argContext)
	if err != nil {
		return fmt.Errorf("check server health: %w", err)
	}
	fmt.Fprintf(console.output, "Server status: %s\n", status.Status)

	return nil
}

func (console *adminConsole) login(
	argContext context.Context,
	argUsername string,
) error {
	if console.accessToken != "" {
		return errors.New("already logged in; log out before starting another session")
	}

	password, err := console.readSecret("Password: ")
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	defer clearBytes(password)

	session, err := console.api.Login(argContext, argUsername, password)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	user, err := console.api.CurrentUser(argContext, session.AccessToken)
	if err != nil {
		console.revokeRejectedSession(session.AccessToken)

		return fmt.Errorf("verify administrator: %w", err)
	}
	if user.Role != identity.RoleAdmin {
		console.revokeRejectedSession(session.AccessToken)

		return errors.New("the authenticated user is not an administrator")
	}

	console.accessToken = session.AccessToken
	fmt.Fprintf(console.output, "Logged in as %s.\n", user.Username)

	return nil
}

func (console *adminConsole) revokeRejectedSession(argAccessToken string) {
	ctx, cancel := context.WithTimeout(context.Background(), console.logoutTimeout)
	defer cancel()
	if err := console.api.Logout(ctx, argAccessToken); err != nil {
		console.printError(fmt.Errorf("revoke rejected session: %w", err))
	}
}

func (console *adminConsole) logout(argContext context.Context) error {
	if err := console.requireAuthentication(); err != nil {
		return err
	}
	if err := console.api.Logout(argContext, console.accessToken); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	console.clearSession()
	fmt.Fprintln(console.output, "Logged out.")

	return nil
}

func (console *adminConsole) logoutOnExit() {
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

func (console *adminConsole) clearSession() {
	console.accessToken = ""
	console.pageStarted = false
	console.nextCursor = ""
}

func (console *adminConsole) me(argContext context.Context) error {
	if err := console.requireAuthentication(); err != nil {
		return err
	}

	user, err := console.api.CurrentUser(argContext, console.accessToken)
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}
	printAdminUser(console.output, user)

	return nil
}

func (console *adminConsole) user(
	argContext context.Context,
	argArguments []string,
) error {
	if err := console.requireAuthentication(); err != nil {
		return err
	}
	if len(argArguments) == 0 {
		return adminCommandUsage("user list|get|create|update|activate|deactivate")
	}

	switch argArguments[0] {
	case "list":
		return console.startUserList(argContext, argArguments[1:])
	case "next":
		if len(argArguments) != 1 {
			return adminCommandUsage("user next")
		}

		return console.nextUserPage(argContext)
	case "first":
		if len(argArguments) != 1 {
			return adminCommandUsage("user first")
		}

		return console.firstUserPage(argContext)
	case "get":
		if len(argArguments) != 2 {
			return adminCommandUsage("user get <id>")
		}

		return console.getUser(argContext, argArguments[1])
	case "create":
		if len(argArguments) != 1 {
			return adminCommandUsage("user create")
		}

		return console.createUser(argContext)
	case "update":
		if len(argArguments) != 2 {
			return adminCommandUsage("user update <id>")
		}

		return console.updateUser(argContext, argArguments[1])
	case "activate", "deactivate":
		if len(argArguments) != 2 {
			return adminCommandUsage("user " + argArguments[0] + " <id>")
		}

		return console.setUserActive(
			argContext,
			argArguments[1],
			argArguments[0] == "activate",
		)
	default:
		return fmt.Errorf("unknown user command %q", argArguments[0])
	}
}

func (console *adminConsole) startUserList(
	argContext context.Context,
	argArguments []string,
) error {
	if len(argArguments) > 1 {
		return adminCommandUsage("user list [limit]")
	}

	limit := defaultUserLimit
	if len(argArguments) == 1 {
		parsedLimit, err := strconv.Atoi(argArguments[0])
		if err != nil || parsedLimit < 1 || parsedLimit > maximumUserLimit {
			return fmt.Errorf("user list limit must be between 1 and %d", maximumUserLimit)
		}
		limit = parsedLimit
	}
	console.pageLimit = limit
	console.pageStarted = true

	if err := console.loadUserPage(argContext, ""); err != nil {
		console.pageStarted = false

		return err
	}

	return nil
}

func (console *adminConsole) nextUserPage(argContext context.Context) error {
	if !console.pageStarted {
		return errors.New("start pagination with 'user list [limit]'")
	}
	if console.nextCursor == "" {
		return errors.New("there is no next user page")
	}

	return console.loadUserPage(argContext, console.nextCursor)
}

func (console *adminConsole) firstUserPage(argContext context.Context) error {
	if !console.pageStarted {
		return errors.New("start pagination with 'user list [limit]'")
	}

	return console.loadUserPage(argContext, "")
}

func (console *adminConsole) loadUserPage(
	argContext context.Context,
	argCursor string,
) error {
	page, err := console.api.ListUsers(
		argContext,
		console.accessToken,
		console.pageLimit,
		argCursor,
	)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	console.nextCursor = page.NextCursor
	if len(page.Users) == 0 {
		fmt.Fprintln(console.output, "No users found.")

		return nil
	}
	for _, user := range page.Users {
		fmt.Fprintf(
			console.output,
			"%s  %-32s  %-6s  active=%t  %s\n",
			user.ID,
			user.Username,
			user.Role,
			user.Active,
			user.DisplayName,
		)
	}
	if page.NextCursor != "" {
		fmt.Fprintln(console.output, "More users are available; use 'user next'.")
	}

	return nil
}

func (console *adminConsole) getUser(
	argContext context.Context,
	argID string,
) error {
	user, err := console.api.UserByID(argContext, console.accessToken, argID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	printAdminUser(console.output, user)

	return nil
}

func (console *adminConsole) createUser(argContext context.Context) error {
	username, err := console.requiredValue(argContext, "Username: ")
	if err != nil {
		return err
	}
	displayName, err := console.requiredValue(argContext, "Display name: ")
	if err != nil {
		return err
	}
	role, err := console.readRole(argContext, "Role (viewer|editor|admin): ", "")
	if err != nil {
		return err
	}

	user, err := console.api.CreateUser(
		argContext,
		console.accessToken,
		apiclient.UserInput{
			Username:    username,
			DisplayName: displayName,
			Role:        role,
		},
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	fmt.Fprintf(console.output, "Created user %s (%s).\n", user.Username, user.ID)

	return nil
}

func (console *adminConsole) updateUser(
	argContext context.Context,
	argID string,
) error {
	current, err := console.api.UserByID(argContext, console.accessToken, argID)
	if err != nil {
		return fmt.Errorf("get user for update: %w", err)
	}

	username, err := console.optionalValue(
		argContext,
		fmt.Sprintf("Username [%s]: ", current.Username),
		current.Username,
	)
	if err != nil {
		return err
	}
	displayName, err := console.optionalValue(
		argContext,
		fmt.Sprintf("Display name [%s]: ", current.DisplayName),
		current.DisplayName,
	)
	if err != nil {
		return err
	}
	role, err := console.readRole(
		argContext,
		fmt.Sprintf("Role (viewer|editor|admin) [%s]: ", current.Role),
		current.Role,
	)
	if err != nil {
		return err
	}

	updated, err := console.api.UpdateUser(
		argContext,
		console.accessToken,
		argID,
		apiclient.UserInput{
			Username:    username,
			DisplayName: displayName,
			Role:        role,
		},
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	fmt.Fprintf(console.output, "Updated user %s (%s).\n", updated.Username, updated.ID)

	return nil
}

func (console *adminConsole) setUserActive(
	argContext context.Context,
	argID string,
	argActive bool,
) error {
	user, err := console.api.SetUserActive(
		argContext,
		console.accessToken,
		argID,
		argActive,
	)
	if err != nil {
		return fmt.Errorf("set user activation: %w", err)
	}
	fmt.Fprintf(console.output, "User %s active=%t.\n", user.Username, user.Active)

	return nil
}

func (console *adminConsole) password(
	argContext context.Context,
	argArguments []string,
) error {
	if err := console.requireAuthentication(); err != nil {
		return err
	}
	if len(argArguments) == 2 && argArguments[0] == "enrollment" {
		return console.issuePasswordEnrollment(argContext, argArguments[1])
	}
	if len(argArguments) == 1 && argArguments[0] == "change" {
		return console.changePassword(argContext)
	}

	return adminCommandUsage("password enrollment <user-id>|change")
}

func (console *adminConsole) issuePasswordEnrollment(
	argContext context.Context,
	argUserID string,
) error {
	enrollment, err := console.api.IssuePasswordEnrollment(
		argContext,
		console.accessToken,
		argUserID,
	)
	if err != nil {
		return fmt.Errorf("issue password enrollment: %w", err)
	}
	fmt.Fprintln(console.output, "Enrollment token (shown once):")
	fmt.Fprintln(console.output, enrollment.Token)
	fmt.Fprintf(
		console.output,
		"Expires: %s\n",
		enrollment.ExpiresAt.Format(time.RFC3339),
	)

	return nil
}

func (console *adminConsole) changePassword(argContext context.Context) error {
	currentPassword, err := console.readSecret("Current password: ")
	if err != nil {
		return fmt.Errorf("read current password: %w", err)
	}
	defer clearBytes(currentPassword)

	newPassword, err := console.confirmedPassword()
	if err != nil {
		return err
	}
	defer clearBytes(newPassword)

	if err := console.api.ChangePassword(
		argContext,
		console.accessToken,
		currentPassword,
		newPassword,
	); err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	console.clearSession()
	fmt.Fprintln(console.output, "Password changed. Log in again.")

	return nil
}

func (console *adminConsole) confirmedPassword() ([]byte, error) {
	password, err := console.readSecret("New password: ")
	if err != nil {
		return nil, fmt.Errorf("read new password: %w", err)
	}

	confirmation, err := console.readSecret("Confirm new password: ")
	if err != nil {
		clearBytes(password)

		return nil, fmt.Errorf("read password confirmation: %w", err)
	}
	defer clearBytes(confirmation)

	if subtle.ConstantTimeCompare(password, confirmation) != 1 {
		clearBytes(password)

		return nil, errors.New("password confirmation does not match")
	}

	return password, nil
}

func (console *adminConsole) requiredValue(
	argContext context.Context,
	argPrompt string,
) (string, error) {
	value, available, err := console.readCommand(argContext, argPrompt)
	if err != nil {
		return "", fmt.Errorf("read value: %w", err)
	}
	if !available {
		return "", io.EOF
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("value is required")
	}

	return value, nil
}

func (console *adminConsole) optionalValue(
	argContext context.Context,
	argPrompt string,
	argDefault string,
) (string, error) {
	value, available, err := console.readCommand(argContext, argPrompt)
	if err != nil {
		return "", fmt.Errorf("read value: %w", err)
	}
	if !available {
		return "", io.EOF
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return argDefault, nil
	}

	return value, nil
}

func (console *adminConsole) readRole(
	argContext context.Context,
	argPrompt string,
	argDefault identity.Role,
) (identity.Role, error) {
	value, available, err := console.readCommand(argContext, argPrompt)
	if err != nil {
		return "", fmt.Errorf("read role: %w", err)
	}
	if !available {
		return "", io.EOF
	}
	if strings.TrimSpace(value) == "" && argDefault.Valid() {
		return argDefault, nil
	}

	role := identity.Role(strings.TrimSpace(value))
	if !role.Valid() {
		return "", errors.New("role must be viewer, editor, or admin")
	}

	return role, nil
}

func (console *adminConsole) requireAuthentication() error {
	if console.accessToken == "" {
		return errors.New("not logged in")
	}

	return nil
}

func (console *adminConsole) printHelp() {
	fmt.Fprintln(console.output, "Commands:")
	fmt.Fprintln(console.output, "  help")
	fmt.Fprintln(console.output, "  health")
	fmt.Fprintln(console.output, "  login <username>")
	fmt.Fprintln(console.output, "  logout")
	fmt.Fprintln(console.output, "  me")
	fmt.Fprintln(console.output, "  user list [limit]")
	fmt.Fprintln(console.output, "  user next")
	fmt.Fprintln(console.output, "  user first")
	fmt.Fprintln(console.output, "  user get <id>")
	fmt.Fprintln(console.output, "  user create")
	fmt.Fprintln(console.output, "  user update <id>")
	fmt.Fprintln(console.output, "  user activate <id>")
	fmt.Fprintln(console.output, "  user deactivate <id>")
	fmt.Fprintln(console.output, "  password enrollment <user-id>")
	fmt.Fprintln(console.output, "  password change")
	fmt.Fprintln(console.output, "  exit | quit | bye")
}

func (console *adminConsole) printError(argError error) {
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

func adminCommandUsage(argUsage string) error {
	return fmt.Errorf("usage: %s", argUsage)
}

func printAdminUser(argOutput io.Writer, argUser apiclient.User) {
	fmt.Fprintf(argOutput, "ID: %s\n", argUser.ID)
	fmt.Fprintf(argOutput, "Username: %s\n", argUser.Username)
	fmt.Fprintf(argOutput, "Display name: %s\n", argUser.DisplayName)
	fmt.Fprintf(argOutput, "Role: %s\n", argUser.Role)
	fmt.Fprintf(argOutput, "Active: %t\n", argUser.Active)
	fmt.Fprintf(argOutput, "Created: %s\n", argUser.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(argOutput, "Updated: %s\n", argUser.UpdatedAt.Format(time.RFC3339))
}
