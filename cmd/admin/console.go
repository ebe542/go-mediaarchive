package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	sharedcli "github.com/ebe542/go-mediaarchive/internal/cli"
	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

const (
	defaultUserLimit = 50
	maximumUserLimit = 100
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
	DeleteUser(context.Context, string, string) error
	IssuePasswordEnrollment(context.Context, string, string) (apiclient.PasswordEnrollment, error)
	ChangePassword(context.Context, string, []byte, []byte) error
}

type secretReader = sharedcli.SecretReader

type adminConsole struct {
	api           adminAPI
	input         *sharedcli.LineReader
	output        io.Writer
	errorOutput   io.Writer
	readSecret    sharedcli.SecretReader
	logoutTimeout time.Duration
	session       *sharedcli.Session
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
		input:         sharedcli.NewLineReader(argInput),
		output:        argOutput,
		errorOutput:   argErrorOutput,
		readSecret:    argReadSecret,
		logoutTimeout: argLogoutTimeout,
		session:       sharedcli.NewSession("mediaarchive-admin"),
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
	return console.input.Read(argContext, console.output, argPrompt)
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

	console.session.Set(user.Username, session.AccessToken)
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
	if err := console.api.Logout(argContext, console.session.AccessToken()); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	console.clearSession()
	fmt.Fprintln(console.output, "Logged out.")

	return nil
}

func (console *adminConsole) logoutOnExit() {
	if !console.session.Authenticated() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), console.logoutTimeout)
	defer cancel()
	if err := console.api.Logout(ctx, console.session.AccessToken()); err != nil {
		console.printError(fmt.Errorf("logout during exit: %w", err))
	}
	console.clearSession()
}

func (console *adminConsole) clearSession() {
	console.session.Clear()
	console.pageStarted = false
	console.nextCursor = ""
}

func (console *adminConsole) me(argContext context.Context) error {
	if err := console.requireAuthentication(); err != nil {
		return err
	}

	user, err := console.api.CurrentUser(argContext, console.session.AccessToken())
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}
	sharedcli.PrintUser(console.output, user)

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
		return adminCommandUsage("user list|get|create|update|activate|deactivate|delete")
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
	case "delete":
		if len(argArguments) != 2 {
			return adminCommandUsage("user delete <id>")
		}

		return console.deleteUser(argContext, argArguments[1])
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
		console.session.AccessToken(),
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
	user, err := console.api.UserByID(argContext, console.session.AccessToken(), argID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	sharedcli.PrintUser(console.output, user)

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
		console.session.AccessToken(),
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
	current, err := console.api.UserByID(
		argContext,
		console.session.AccessToken(),
		argID,
	)
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
		console.session.AccessToken(),
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
		console.session.AccessToken(),
		argID,
		argActive,
	)
	if err != nil {
		return fmt.Errorf("set user activation: %w", err)
	}
	fmt.Fprintf(console.output, "User %s active=%t.\n", user.Username, user.Active)

	return nil
}

func (console *adminConsole) deleteUser(
	argContext context.Context,
	argID string,
) error {
	user, err := console.api.UserByID(
		argContext,
		console.session.AccessToken(),
		argID,
	)
	if err != nil {
		return fmt.Errorf("get user for deletion: %w", err)
	}
	sharedcli.PrintUser(console.output, user)

	for {
		confirmation, available, err := console.readCommand(
			argContext,
			fmt.Sprintf(
				"Type username %q to permanently delete this user (blank cancels): ",
				user.Username,
			),
		)
		if err != nil {
			return fmt.Errorf("read deletion confirmation: %w", err)
		}
		if !available {
			return io.EOF
		}

		confirmation = strings.TrimSpace(confirmation)
		if confirmation == "" {
			fmt.Fprintln(console.output, "User deletion canceled.")

			return nil
		}
		if confirmation != user.Username {
			console.printError(errors.New("username does not match; try again"))

			continue
		}

		break
	}

	if err := console.api.DeleteUser(
		argContext,
		console.session.AccessToken(),
		user.ID,
	); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	console.pageStarted = false
	console.nextCursor = ""
	console.pageLimit = defaultUserLimit
	fmt.Fprintf(console.output, "Permanently deleted user %s (%s).\n", user.Username, user.ID)

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
		console.session.AccessToken(),
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
		sharedcli.FormatLocalTime(enrollment.ExpiresAt),
	)

	return nil
}

func (console *adminConsole) changePassword(argContext context.Context) error {
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
		argContext,
		console.session.AccessToken(),
		currentPassword,
		newPassword,
	); err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	console.clearSession()
	fmt.Fprintln(console.output, "Password changed. Log in again.")

	return nil
}

func (console *adminConsole) requiredValue(
	argContext context.Context,
	argPrompt string,
) (string, error) {
	for {
		value, available, err := console.readCommand(argContext, argPrompt)
		if err != nil {
			return "", fmt.Errorf("read value: %w", err)
		}
		if !available {
			return "", io.EOF
		}
		value = strings.TrimSpace(value)
		if value != "" {
			return value, nil
		}

		console.printError(errors.New("value is required; try again"))
	}
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
	for {
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
		if role.Valid() {
			return role, nil
		}

		console.printError(errors.New("role must be viewer, editor, or admin; try again"))
	}
}

func (console *adminConsole) requireAuthentication() error {
	if !console.session.Authenticated() {
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
	fmt.Fprintln(console.output, "  user delete <id>")
	fmt.Fprintln(console.output, "  password enrollment <user-id>")
	fmt.Fprintln(console.output, "  password change")
	fmt.Fprintln(console.output, "  exit | quit | bye")
}

func (console *adminConsole) printError(argError error) {
	sharedcli.PrintError(console.errorOutput, argError)
}

func adminCommandUsage(argUsage string) error {
	return fmt.Errorf("usage: %s", argUsage)
}
