package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	sharedcli "github.com/ebe542/go-mediaarchive/internal/cli"
	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type userListCall struct {
	limit  int
	cursor string
}

type recordingAdminAPI struct {
	loginSession       apiclient.Session
	currentUser        apiclient.User
	logoutTokens       []string
	listCalls          []userListCall
	createdInput       apiclient.UserInput
	updatedID          string
	updatedInput       apiclient.UserInput
	activationIDs      []string
	activationValues   []bool
	userLookupIDs      []string
	deletedIDs         []string
	enrollmentUserID   string
	passwordChange     bool
	currentPassword    string
	newPassword        string
	currentUserCallNum int
}

func (api *recordingAdminAPI) Health(
	context.Context,
) (apiclient.HealthStatus, error) {
	return apiclient.HealthStatus{Status: "ok"}, nil
}

func (api *recordingAdminAPI) Login(
	context.Context,
	string,
	[]byte,
) (apiclient.Session, error) {
	return api.loginSession, nil
}

func (api *recordingAdminAPI) Logout(
	_ context.Context,
	accessToken string,
) error {
	api.logoutTokens = append(api.logoutTokens, accessToken)

	return nil
}

func (api *recordingAdminAPI) CurrentUser(
	context.Context,
	string,
) (apiclient.User, error) {
	api.currentUserCallNum++

	return api.currentUser, nil
}

func (api *recordingAdminAPI) UserByID(
	_ context.Context,
	_ string,
	id string,
) (apiclient.User, error) {
	api.userLookupIDs = append(api.userLookupIDs, id)

	return testAdminUser(id, "target_user", identity.RoleEditor, true), nil
}

func (api *recordingAdminAPI) ListUsers(
	_ context.Context,
	_ string,
	limit int,
	cursor string,
) (apiclient.UserPage, error) {
	api.listCalls = append(api.listCalls, userListCall{
		limit:  limit,
		cursor: cursor,
	})
	nextCursor := "next-cursor"
	if cursor != "" {
		nextCursor = ""
	}

	return apiclient.UserPage{
		Users: []apiclient.User{
			testAdminUser("listed-id", "listed_user", identity.RoleViewer, true),
		},
		NextCursor: nextCursor,
	}, nil
}

func (api *recordingAdminAPI) CreateUser(
	_ context.Context,
	_ string,
	input apiclient.UserInput,
) (apiclient.User, error) {
	api.createdInput = input

	return testAdminUser("created-id", input.Username, input.Role, true), nil
}

func (api *recordingAdminAPI) UpdateUser(
	_ context.Context,
	_ string,
	id string,
	input apiclient.UserInput,
) (apiclient.User, error) {
	api.updatedID = id
	api.updatedInput = input

	return testAdminUser(id, input.Username, input.Role, true), nil
}

func (api *recordingAdminAPI) SetUserActive(
	_ context.Context,
	_ string,
	id string,
	active bool,
) (apiclient.User, error) {
	api.activationIDs = append(api.activationIDs, id)
	api.activationValues = append(api.activationValues, active)

	return testAdminUser(id, "target_user", identity.RoleEditor, active), nil
}

func (api *recordingAdminAPI) DeleteUser(
	_ context.Context,
	_ string,
	id string,
) error {
	api.deletedIDs = append(api.deletedIDs, id)

	return nil
}

func (api *recordingAdminAPI) IssuePasswordEnrollment(
	_ context.Context,
	_ string,
	userID string,
) (apiclient.PasswordEnrollment, error) {
	api.enrollmentUserID = userID

	return apiclient.PasswordEnrollment{
		Token:     "one-time-token",
		ExpiresAt: time.Date(2026, time.September, 8, 13, 0, 0, 0, time.UTC),
	}, nil
}

func (api *recordingAdminAPI) ChangePassword(
	_ context.Context,
	_ string,
	currentPassword []byte,
	newPassword []byte,
) error {
	api.passwordChange = true
	api.currentPassword = string(currentPassword)
	api.newPassword = string(newPassword)

	return nil
}

func TestAdminConsoleRunsUserManagementScenario(t *testing.T) {
	api := &recordingAdminAPI{
		loginSession: apiclient.Session{AccessToken: "admin-token"},
		currentUser: testAdminUser(
			"admin-id",
			"archive_admin",
			identity.RoleAdmin,
			true,
		),
	}
	input := strings.Join([]string{
		"login archive_admin",
		"me",
		"user list 2",
		"user next",
		"user first",
		"user get target-id",
		"user create",
		"",
		"created_user",
		"Created User",
		"edtor",
		"editor",
		"user update target-id",
		"",
		"Updated User",
		"viewer",
		"user deactivate target-id",
		"user activate target-id",
		"password enrollment target-id",
		"logout",
		"exit",
	}, "\n") + "\n"
	loginPassword := []byte("admin passphrase")
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	console := newAdminConsole(
		api,
		strings.NewReader(input),
		&output,
		&errorOutput,
		func(string) ([]byte, error) {
			return loginPassword, nil
		},
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run admin console: %v", err)
	}
	if !strings.Contains(errorOutput.String(), "value is required") ||
		!strings.Contains(errorOutput.String(), "role must be viewer, editor, or admin") {
		t.Errorf("expected field validation retries, got %q", errorOutput.String())
	}
	if api.currentUserCallNum != 2 {
		t.Errorf("expected login verification and me lookup, got %d calls", api.currentUserCallNum)
	}
	expectedLists := []userListCall{
		{limit: 2, cursor: ""},
		{limit: 2, cursor: "next-cursor"},
		{limit: 2, cursor: ""},
	}
	if len(api.listCalls) != len(expectedLists) {
		t.Fatalf("expected list calls %v, got %v", expectedLists, api.listCalls)
	}
	for index := range expectedLists {
		if api.listCalls[index] != expectedLists[index] {
			t.Errorf("expected list call %+v, got %+v", expectedLists[index], api.listCalls[index])
		}
	}
	if api.createdInput.Username != "created_user" ||
		api.createdInput.DisplayName != "Created User" ||
		api.createdInput.Role != identity.RoleEditor {
		t.Errorf("unexpected create input: %+v", api.createdInput)
	}
	if api.updatedID != "target-id" ||
		api.updatedInput.Username != "target_user" ||
		api.updatedInput.DisplayName != "Updated User" ||
		api.updatedInput.Role != identity.RoleViewer {
		t.Errorf("unexpected update: %q %+v", api.updatedID, api.updatedInput)
	}
	if len(api.activationValues) != 2 ||
		api.activationValues[0] ||
		!api.activationValues[1] {
		t.Errorf("unexpected activation values: %v", api.activationValues)
	}
	if api.enrollmentUserID != "target-id" {
		t.Errorf("unexpected enrollment user ID %q", api.enrollmentUserID)
	}
	if len(api.logoutTokens) != 1 || api.logoutTokens[0] != "admin-token" {
		t.Errorf("unexpected logout tokens: %v", api.logoutTokens)
	}
	if !strings.Contains(output.String(), "one-time-token") ||
		!strings.Contains(output.String(), "More users are available") ||
		!strings.Contains(output.String(), "archive_admin@mediaarchive-admin> ") ||
		!strings.Contains(
			output.String(),
			sharedcli.FormatLocalTime(
				time.Date(2026, time.September, 8, 13, 0, 0, 0, time.UTC),
			),
		) {
		t.Errorf("unexpected output: %q", output.String())
	}
	for index, value := range loginPassword {
		if value != 0 {
			t.Errorf("login password byte %d was not cleared", index)
		}
	}
}

func TestAdminConsoleRepeatsOnlyUserDeletionConfirmation(t *testing.T) {
	api := &recordingAdminAPI{
		loginSession: apiclient.Session{AccessToken: "admin-token"},
		currentUser: testAdminUser(
			"admin-id",
			"archive_admin",
			identity.RoleAdmin,
			true,
		),
	}
	input := strings.Join([]string{
		"login archive_admin",
		"user delete target-id",
		"wrong_user",
		"target_user",
		"exit",
	}, "\n") + "\n"
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	console := newAdminConsole(
		api,
		strings.NewReader(input),
		&output,
		&errorOutput,
		func(string) ([]byte, error) {
			return []byte("admin passphrase"), nil
		},
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run admin console: %v", err)
	}
	if len(api.userLookupIDs) != 1 || api.userLookupIDs[0] != "target-id" {
		t.Fatalf("expected one target lookup, got %v", api.userLookupIDs)
	}
	if len(api.deletedIDs) != 1 || api.deletedIDs[0] != "target-id" {
		t.Fatalf("expected confirmed target deletion, got %v", api.deletedIDs)
	}
	confirmationPrompt := `Type username "target_user" to permanently delete this user (blank cancels): `
	if strings.Count(output.String(), confirmationPrompt) != 2 {
		t.Errorf("expected only confirmation prompt to repeat, got %q", output.String())
	}
	if !strings.Contains(errorOutput.String(), "username does not match; try again") {
		t.Errorf("expected confirmation error, got %q", errorOutput.String())
	}
	if !strings.Contains(output.String(), "Permanently deleted user target_user (target-id).") {
		t.Errorf("expected deletion result, got %q", output.String())
	}
}

func TestAdminConsoleCancelsUserDeletionOnBlankConfirmation(t *testing.T) {
	api := &recordingAdminAPI{
		loginSession: apiclient.Session{AccessToken: "admin-token"},
		currentUser: testAdminUser(
			"admin-id",
			"archive_admin",
			identity.RoleAdmin,
			true,
		),
	}
	var output bytes.Buffer
	console := newAdminConsole(
		api,
		strings.NewReader("login archive_admin\nuser delete target-id\n\nexit\n"),
		&output,
		&bytes.Buffer{},
		func(string) ([]byte, error) {
			return []byte("admin passphrase"), nil
		},
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run admin console: %v", err)
	}
	if len(api.deletedIDs) != 0 {
		t.Fatalf("expected no deletion, got %v", api.deletedIDs)
	}
	if !strings.Contains(output.String(), "User deletion canceled.") {
		t.Errorf("expected cancellation result, got %q", output.String())
	}
}

func TestAdminConsoleRejectsNonAdministratorAndRevokesSession(t *testing.T) {
	api := &recordingAdminAPI{
		loginSession: apiclient.Session{AccessToken: "viewer-token"},
		currentUser: testAdminUser(
			"viewer-id",
			"archive_viewer",
			identity.RoleViewer,
			true,
		),
	}
	var errorOutput bytes.Buffer
	console := newAdminConsole(
		api,
		strings.NewReader("login archive_viewer\nexit\n"),
		&bytes.Buffer{},
		&errorOutput,
		func(string) ([]byte, error) {
			return []byte("viewer passphrase"), nil
		},
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run admin console: %v", err)
	}
	if !strings.Contains(errorOutput.String(), "is not an administrator") {
		t.Errorf("expected role error, got %q", errorOutput.String())
	}
	if len(api.logoutTokens) != 1 || api.logoutTokens[0] != "viewer-token" {
		t.Errorf("expected rejected session revocation, got %v", api.logoutTokens)
	}
}

func TestAdminConsoleChangesPasswordAndClearsSession(t *testing.T) {
	api := &recordingAdminAPI{
		loginSession: apiclient.Session{AccessToken: "admin-token"},
		currentUser: testAdminUser(
			"admin-id",
			"archive_admin",
			identity.RoleAdmin,
			true,
		),
	}
	secrets := [][]byte{
		[]byte("login passphrase"),
		[]byte("current passphrase"),
		[]byte("new passphrase"),
		[]byte("mistyped passphrase"),
		[]byte("new passphrase"),
		[]byte("new passphrase"),
	}
	secretIndex := 0
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	console := newAdminConsole(
		api,
		strings.NewReader("login archive_admin\npassword change\nbye\n"),
		&output,
		&errorOutput,
		func(string) ([]byte, error) {
			secret := secrets[secretIndex]
			secretIndex++

			return secret, nil
		},
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run admin console: %v", err)
	}
	if !api.passwordChange ||
		api.currentPassword != "current passphrase" ||
		api.newPassword != "new passphrase" {
		t.Errorf("unexpected password change: %+v", api)
	}
	if len(api.logoutTokens) != 0 {
		t.Errorf("expected password change to clear session, got %v", api.logoutTokens)
	}
	if !strings.Contains(errorOutput.String(), "password confirmation does not match") {
		t.Errorf("expected password confirmation retry, got %q", errorOutput.String())
	}
	if !strings.Contains(output.String(), "anonymous@mediaarchive-admin> ") {
		t.Errorf("expected anonymous prompt after password change, got %q", output.String())
	}
	for secretIndex, secret := range secrets {
		for byteIndex, value := range secret {
			if value != 0 {
				t.Errorf("secret %d byte %d was not cleared", secretIndex, byteIndex)
			}
		}
	}
}

func TestAdminConsoleLogsOutActiveSessionOnExit(t *testing.T) {
	api := &recordingAdminAPI{
		loginSession: apiclient.Session{AccessToken: "admin-token"},
		currentUser: testAdminUser(
			"admin-id",
			"archive_admin",
			identity.RoleAdmin,
			true,
		),
	}
	console := newAdminConsole(
		api,
		strings.NewReader("login archive_admin\nexit\n"),
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(string) ([]byte, error) {
			return []byte("admin passphrase"), nil
		},
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run admin console: %v", err)
	}
	if len(api.logoutTokens) != 1 || api.logoutTokens[0] != "admin-token" {
		t.Errorf("expected exit logout, got %v", api.logoutTokens)
	}
}

func testAdminUser(
	id string,
	username string,
	role identity.Role,
	active bool,
) apiclient.User {
	return apiclient.User{
		ID:          id,
		Username:    username,
		DisplayName: strings.ReplaceAll(username, "_", " "),
		Role:        role,
		Active:      active,
		CreatedAt:   time.Date(2026, time.September, 8, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.September, 8, 11, 0, 0, 0, time.UTC),
	}
}
