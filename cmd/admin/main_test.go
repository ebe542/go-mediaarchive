package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestRunBootstrapCommandConfirmsPassword(t *testing.T) {

	const plainPassword = "synthetic passphrase"

	passwordEntries := [][]byte{
		[]byte(plainPassword),
		[]byte(plainPassword),
	}
	passwordIndex := 0

	readPassword := func() ([]byte, error) {
		password := passwordEntries[passwordIndex]
		passwordIndex++

		return append([]byte(nil), password...), nil
	}

	var receivedDatabasePath string
	var receivedUsername string
	var receivedDisplayName string
	var receivedPassword []byte

	bootstrapAdmin := func(
		argContext context.Context,
		argDatabasePath string,
		argUsername string,
		argDisplayName string,
		argPassword []byte,
	) (identity.User, error) {
		receivedDatabasePath = argDatabasePath
		receivedUsername = argUsername
		receivedDisplayName = argDisplayName
		receivedPassword = append([]byte(nil), argPassword...)

		return identity.User{
			ID:          "123e4567-e89b-12d3-a456-426614174000",
			Username:    "bootstrap_admin",
			DisplayName: "Bootstrap Administrator",
			Role:        identity.RoleAdmin,
			Active:      true,
			CreatedAt:   time.Date(2026, time.August, 18, 22, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, time.August, 18, 22, 0, 0, 0, time.UTC),
		}, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run(
		context.Background(),
		[]string{
			"bootstrap",
			"--database",
			"./data/test.db",
			"--username",
			" Bootstrap_Admin ",
			"--display-name",
			" Bootstrap Administrator ",
		},
		&stdout,
		&stderr,
		readPassword,
		bootstrapAdmin,
	)
	if err != nil {
		t.Fatalf("run bootstrap command: %v", err)
	}

	if receivedDatabasePath != "./data/test.db" {
		t.Fatalf(
			"expected database path %q, got %q",
			"./data/test.db",
			receivedDatabasePath,
		)
	}
	if receivedUsername != " Bootstrap_Admin " {
		t.Fatalf("expected supplied username, got %q", receivedUsername)
	}
	if receivedDisplayName != " Bootstrap Administrator " {
		t.Fatalf(
			"expected supplied display name, got %q",
			receivedDisplayName,
		)
	}
	if string(receivedPassword) != plainPassword {
		t.Fatal("expected confirmed password to reach bootstrap service")
	}

	const expectedOutput = "Administrator \"bootstrap_admin\" created.\n"
	if stdout.String() != expectedOutput {
		t.Fatalf(
			"expected stdout %q, got %q",
			expectedOutput,
			stdout.String(),
		)
	}

	combinedOutput := stdout.String() + stderr.String()
	if strings.Contains(combinedOutput, plainPassword) {
		t.Fatal("expected command output not to contain the password")
	}

	if passwordIndex != 2 {
		t.Fatalf(
			"expected two password reads, got %d",
			passwordIndex,
		)
	}
}

func TestRunBootstrapCommandRejectsPasswordMismatch(t *testing.T) {
	const plainPassword = "synthetic passphrase"
	const differentPassword = "different passphrase"

	passwordEntries := [][]byte{
		[]byte(plainPassword),
		[]byte(differentPassword),
	}
	passwordIndex := 0

	readPassword := func() ([]byte, error) {
		password := passwordEntries[passwordIndex]
		passwordIndex++

		return append([]byte(nil), password...), nil
	}

	bootstrapCalls := 0
	bootstrapAdmin := func(
		argContext context.Context,
		argDatabasePath string,
		argUsername string,
		argDisplayName string,
		argPassword []byte,
	) (identity.User, error) {
		bootstrapCalls++

		return identity.User{}, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run(
		context.Background(),
		[]string{
			"bootstrap",
			"--username",
			"bootstrap_admin",
			"--display-name",
			"Bootstrap Administrator",
		},
		&stdout,
		&stderr,
		readPassword,
		bootstrapAdmin,
	)
	if err == nil {
		t.Fatal("expected password mismatch error")
	}

	if bootstrapCalls != 0 {
		t.Fatalf(
			"expected no bootstrap call, got %d",
			bootstrapCalls,
		)
	}

	combinedOutput := stdout.String() + stderr.String() + err.Error()
	if strings.Contains(combinedOutput, plainPassword) ||
		strings.Contains(combinedOutput, differentPassword) {
		t.Fatal("expected output and error not to contain passwords")
	}
}

func TestBootstrapAdministratorPersistsVerifiableCredential(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "data", "mediaarchive.db")
	plainPassword := []byte("synthetic passphrase")

	adminUser, err := bootstrapAdministrator(
		ctx,
		databasePath,
		" Bootstrap_Admin ",
		" Bootstrap Administrator ",
		plainPassword,
	)
	if err != nil {
		t.Fatalf("bootstrap administrator: %v", err)
	}

	if adminUser.Username != "bootstrap_admin" {
		t.Fatalf(
			"expected normalized username %q, got %q",
			"bootstrap_admin",
			adminUser.Username,
		)
	}
	if adminUser.Role != identity.RoleAdmin {
		t.Fatalf(
			"expected admin role, got %q",
			adminUser.Role,
		)
	}

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	var encodedHash string
	err = database.QueryRowContext(
		ctx,
		`
			SELECT password_hash
			FROM password_credentials
			WHERE user_id = ?
		`,
		adminUser.ID,
	).Scan(&encodedHash)
	if err != nil {
		t.Fatalf("read password credential: %v", err)
	}

	matches, err := password.NewDefaultHasher().Verify(
		plainPassword,
		encodedHash,
	)
	if err != nil {
		t.Fatalf("verify stored password hash: %v", err)
	}
	if !matches {
		t.Fatal("expected stored credential to verify")
	}
}
