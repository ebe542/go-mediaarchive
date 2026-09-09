package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingUserAPI struct {
	healthStatus          apiclient.HealthStatus
	loginSession          apiclient.Session
	currentUser           apiclient.User
	loginUsername         string
	loginPassword         string
	logoutTokens          []string
	enrollmentToken       string
	enrollmentPassword    string
	changeToken           string
	changeCurrentPassword string
	changeNewPassword     string
}

func (api *recordingUserAPI) Health(
	context.Context,
) (apiclient.HealthStatus, error) {
	return api.healthStatus, nil
}

func (api *recordingUserAPI) Login(
	_ context.Context,
	argUsername string,
	argPassword []byte,
) (apiclient.Session, error) {
	api.loginUsername = argUsername
	api.loginPassword = string(argPassword)

	return api.loginSession, nil
}

func (api *recordingUserAPI) Logout(
	_ context.Context,
	argAccessToken string,
) error {
	api.logoutTokens = append(api.logoutTokens, argAccessToken)

	return nil
}

func (api *recordingUserAPI) CurrentUser(
	context.Context,
	string,
) (apiclient.User, error) {
	return api.currentUser, nil
}

func (api *recordingUserAPI) CompletePasswordEnrollment(
	_ context.Context,
	argToken string,
	argPassword []byte,
) error {
	api.enrollmentToken = argToken
	api.enrollmentPassword = string(argPassword)

	return nil
}

func (api *recordingUserAPI) ChangePassword(
	_ context.Context,
	argAccessToken string,
	argCurrentPassword []byte,
	argNewPassword []byte,
) error {
	api.changeToken = argAccessToken
	api.changeCurrentPassword = string(argCurrentPassword)
	api.changeNewPassword = string(argNewPassword)

	return nil
}

func TestUserConsoleRunsAuthenticatedPasswordChangeScenario(t *testing.T) {
	api := &recordingUserAPI{
		loginSession: apiclient.Session{AccessToken: "access-token"},
		currentUser: apiclient.User{
			ID:          "user-id",
			Username:    "archive_user",
			DisplayName: "Archive User",
			Role:        identity.RoleViewer,
			Active:      true,
			CreatedAt:   time.Date(2026, time.September, 8, 10, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, time.September, 8, 11, 0, 0, 0, time.UTC),
		},
	}
	secrets := [][]byte{
		[]byte("login passphrase"),
		[]byte("current passphrase"),
		[]byte("new passphrase"),
		[]byte("mistyped passphrase"),
		[]byte("new passphrase"),
		[]byte("new passphrase"),
	}
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	console := newUserConsole(
		api,
		strings.NewReader("login archive_user\nme\npassword change\nexit\n"),
		&output,
		&errorOutput,
		queuedSecretReader(t, secrets),
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run console: %v", err)
	}
	if api.loginUsername != "archive_user" ||
		api.loginPassword != "login passphrase" {
		t.Errorf("unexpected login: %q %q", api.loginUsername, api.loginPassword)
	}
	if api.changeToken != "access-token" ||
		api.changeCurrentPassword != "current passphrase" ||
		api.changeNewPassword != "new passphrase" {
		t.Errorf("unexpected password change: %+v", api)
	}
	if len(api.logoutTokens) != 0 {
		t.Errorf("expected changed password to clear the session, got logout tokens %v", api.logoutTokens)
	}
	if !strings.Contains(output.String(), "Username: archive_user") ||
		!strings.Contains(output.String(), "archive_user@mediaarchive> ") ||
		!strings.Contains(output.String(), "anonymous@mediaarchive> ") ||
		!strings.Contains(output.String(), formatLocalTime(api.currentUser.CreatedAt)) ||
		!strings.Contains(output.String(), "Password changed. Log in again.") {
		t.Errorf("unexpected console output: %q", output.String())
	}
	if !strings.Contains(errorOutput.String(), "password confirmation does not match") {
		t.Errorf("expected password confirmation retry, got %q", errorOutput.String())
	}
	assertSecretsCleared(t, secrets)
}

func TestUserConsoleEnrollsPasswordWithoutSession(t *testing.T) {
	api := &recordingUserAPI{}
	secrets := [][]byte{
		[]byte("one-time-token"),
		[]byte("new passphrase"),
		[]byte("new passphrase"),
	}
	var output bytes.Buffer
	console := newUserConsole(
		api,
		strings.NewReader("password enroll\nbye\n"),
		&output,
		&bytes.Buffer{},
		queuedSecretReader(t, secrets),
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run console: %v", err)
	}
	if api.enrollmentToken != "one-time-token" ||
		api.enrollmentPassword != "new passphrase" {
		t.Errorf(
			"unexpected enrollment: token %q password %q",
			api.enrollmentToken,
			api.enrollmentPassword,
		)
	}
	if !strings.Contains(output.String(), "Password enrolled.") {
		t.Errorf("unexpected console output: %q", output.String())
	}
	assertSecretsCleared(t, secrets)
}

func TestUserConsoleLogsOutOnExit(t *testing.T) {
	api := &recordingUserAPI{
		loginSession: apiclient.Session{AccessToken: "access-token"},
		currentUser: apiclient.User{
			Username: "archive_user",
		},
	}
	console := newUserConsole(
		api,
		strings.NewReader("login archive_user\nquit\n"),
		&bytes.Buffer{},
		&bytes.Buffer{},
		queuedSecretReader(t, [][]byte{[]byte("login passphrase")}),
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run console: %v", err)
	}
	if len(api.logoutTokens) != 1 || api.logoutTokens[0] != "access-token" {
		t.Errorf("expected exit logout, got %v", api.logoutTokens)
	}
}

func TestUserConsoleContinuesAfterCommandError(t *testing.T) {
	api := &recordingUserAPI{
		healthStatus: apiclient.HealthStatus{Status: "ok"},
	}
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	console := newUserConsole(
		api,
		strings.NewReader("unknown\nhealth\nexit\n"),
		&output,
		&errorOutput,
		func(string) ([]byte, error) {
			return nil, errors.New("unexpected secret request")
		},
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run console: %v", err)
	}
	if !strings.Contains(errorOutput.String(), `unknown command "unknown"`) {
		t.Errorf("expected command error, got %q", errorOutput.String())
	}
	if !strings.Contains(output.String(), "Server status: ok") {
		t.Errorf("expected health output after error, got %q", output.String())
	}
}

func queuedSecretReader(
	argTest *testing.T,
	argSecrets [][]byte,
) secretReader {
	argTest.Helper()

	index := 0

	return func(string) ([]byte, error) {
		if index >= len(argSecrets) {
			argTest.Fatal("unexpected secret request")
		}
		secret := argSecrets[index]
		index++

		return secret, nil
	}
}

func assertSecretsCleared(argTest *testing.T, argSecrets [][]byte) {
	argTest.Helper()

	for secretIndex, secret := range argSecrets {
		for byteIndex, value := range secret {
			if value != 0 {
				argTest.Errorf(
					"secret %d byte %d was not cleared",
					secretIndex,
					byteIndex,
				)
			}
		}
	}
}
