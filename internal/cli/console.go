// Package cli provides shared primitives for interactive command-line tools.
package cli

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"time"

	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
)

// SecretReader reads a secret without exposing it through the command stream.
type SecretReader func(string) ([]byte, error)

// LineReader reads one command or prompted value at a time.
type LineReader struct {
	scanner *bufio.Scanner
}

// NewLineReader creates a line reader around an input stream.
func NewLineReader(argInput io.Reader) *LineReader {
	return &LineReader{scanner: bufio.NewScanner(argInput)}
}

// Read writes a prompt and waits for one line or context cancellation.
func (reader *LineReader) Read(
	argContext context.Context,
	argOutput io.Writer,
	argPrompt string,
) (string, bool, error) {
	fmt.Fprint(argOutput, argPrompt)

	type result struct {
		line      string
		available bool
		err       error
	}
	results := make(chan result, 1)
	go func() {
		available := reader.scanner.Scan()
		results <- result{
			line:      reader.scanner.Text(),
			available: available,
			err:       reader.scanner.Err(),
		}
	}()

	select {
	case <-argContext.Done():
		return "", false, argContext.Err()
	case scanned := <-results:
		return scanned.line, scanned.available, scanned.err
	}
}

// Session stores authentication state only for the lifetime of the process.
type Session struct {
	application string
	username    string
	accessToken string
}

// NewSession creates empty prompt and authentication state.
func NewSession(argApplication string) *Session {
	return &Session{application: argApplication}
}

// Prompt identifies the authenticated user and current application.
func (session *Session) Prompt() string {
	username := session.username
	if username == "" {
		username = "anonymous"
	}

	return fmt.Sprintf("%s@%s> ", username, session.application)
}

// Authenticated reports whether an access token is present.
func (session *Session) Authenticated() bool { return session.accessToken != "" }

// AccessToken returns the in-memory bearer token.
func (session *Session) AccessToken() string { return session.accessToken }

// Set records a server-confirmed identity and its bearer token.
func (session *Session) Set(argUsername string, argAccessToken string) {
	session.username = argUsername
	session.accessToken = argAccessToken
}

// Clear removes all local authentication state.
func (session *Session) Clear() {
	session.username = ""
	session.accessToken = ""
}

// ReadRequiredSecret repeats an empty secret prompt.
func ReadRequiredSecret(
	argReader SecretReader,
	argPrompt string,
	argReport func(error),
) ([]byte, error) {
	for {
		secret, err := argReader(argPrompt)
		if err != nil {
			return nil, err
		}
		if len(secret) != 0 {
			return secret, nil
		}

		ClearSecret(secret)
		argReport(errors.New("value is required; try again"))
	}
}

// ReadConfirmedSecret repeats both prompts until their values match.
func ReadConfirmedSecret(
	argReader SecretReader,
	argReport func(error),
) ([]byte, error) {
	for {
		secret, err := ReadRequiredSecret(argReader, "New password: ", argReport)
		if err != nil {
			return nil, fmt.Errorf("read new password: %w", err)
		}
		confirmation, err := ReadRequiredSecret(
			argReader,
			"Confirm new password: ",
			argReport,
		)
		if err != nil {
			ClearSecret(secret)

			return nil, fmt.Errorf("read password confirmation: %w", err)
		}
		if subtle.ConstantTimeCompare(secret, confirmation) == 1 {
			ClearSecret(confirmation)

			return secret, nil
		}

		ClearSecret(secret)
		ClearSecret(confirmation)
		argReport(errors.New("password confirmation does not match; try again"))
	}
}

// ClearSecret overwrites a mutable secret buffer.
func ClearSecret(argSecret []byte) {
	for index := range argSecret {
		argSecret[index] = 0
	}
}

// PrintError writes a safe structured API error or a local error.
func PrintError(argOutput io.Writer, argError error) {
	var apiError *apiclient.APIError
	if errors.As(argError, &apiError) {
		fmt.Fprintf(argOutput, "Error [%s]: %s\n", apiError.Code, apiError.Message)

		return
	}
	fmt.Fprintf(argOutput, "Error: %v\n", argError)
}

// FormatLocalTime renders an instant in the operating system's local zone.
func FormatLocalTime(argTime time.Time) string {
	return argTime.Local().Format(time.RFC3339)
}

// PrintUser writes the common user representation.
func PrintUser(argOutput io.Writer, argUser apiclient.User) {
	fmt.Fprintf(argOutput, "ID: %s\n", argUser.ID)
	fmt.Fprintf(argOutput, "Username: %s\n", argUser.Username)
	fmt.Fprintf(argOutput, "Display name: %s\n", argUser.DisplayName)
	fmt.Fprintf(argOutput, "Role: %s\n", argUser.Role)
	fmt.Fprintf(argOutput, "Active: %t\n", argUser.Active)
	fmt.Fprintf(argOutput, "Created: %s\n", FormatLocalTime(argUser.CreatedAt))
	fmt.Fprintf(argOutput, "Updated: %s\n", FormatLocalTime(argUser.UpdatedAt))
}
