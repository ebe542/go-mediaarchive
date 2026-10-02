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
func NewLineReader(input io.Reader) *LineReader {
	return &LineReader{scanner: bufio.NewScanner(input)}
}

// Read writes a prompt and waits for one line or context cancellation.
func (reader *LineReader) Read(
	ctx context.Context,
	output io.Writer,
	prompt string,
) (string, bool, error) {
	fmt.Fprint(output, prompt)

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
	case <-ctx.Done():
		return "", false, ctx.Err()
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
func NewSession(application string) *Session {
	return &Session{application: application}
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
func (session *Session) Set(username string, accessToken string) {
	session.username = username
	session.accessToken = accessToken
}

// Clear removes all local authentication state.
func (session *Session) Clear() {
	session.username = ""
	session.accessToken = ""
}

// ReadRequiredSecret repeats an empty secret prompt.
func ReadRequiredSecret(
	reader SecretReader,
	prompt string,
	report func(error),
) ([]byte, error) {
	for {
		secret, err := reader(prompt)
		if err != nil {
			return nil, err
		}
		if len(secret) != 0 {
			return secret, nil
		}

		ClearSecret(secret)
		report(errors.New("value is required; try again"))
	}
}

// ReadConfirmedSecret repeats both prompts until their values match.
func ReadConfirmedSecret(
	reader SecretReader,
	report func(error),
) ([]byte, error) {
	for {
		secret, err := ReadRequiredSecret(reader, "New password: ", report)
		if err != nil {
			return nil, fmt.Errorf("read new password: %w", err)
		}
		confirmation, err := ReadRequiredSecret(
			reader,
			"Confirm new password: ",
			report,
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
		report(errors.New("password confirmation does not match; try again"))
	}
}

// ClearSecret overwrites a mutable secret buffer.
func ClearSecret(secret []byte) {
	for index := range secret {
		secret[index] = 0
	}
}

// PrintError writes a safe structured API error or a local error.
func PrintError(output io.Writer, inputError error) {
	var apiError *apiclient.APIError
	if errors.As(inputError, &apiError) {
		fmt.Fprintf(output, "Error [%s]: %s\n", apiError.Code, apiError.Message)

		return
	}
	fmt.Fprintf(output, "Error: %v\n", inputError)
}

// FormatLocalTime renders an instant in the operating system's local zone.
func FormatLocalTime(timestamp time.Time) string {
	return timestamp.Local().Format(time.RFC3339)
}

// PrintUser writes the common user representation.
func PrintUser(output io.Writer, user apiclient.User) {
	fmt.Fprintf(output, "ID: %s\n", user.ID)
	fmt.Fprintf(output, "Username: %s\n", user.Username)
	fmt.Fprintf(output, "Display name: %s\n", user.DisplayName)
	fmt.Fprintf(output, "Role: %s\n", user.Role)
	fmt.Fprintf(output, "Active: %t\n", user.Active)
	fmt.Fprintf(output, "Created: %s\n", FormatLocalTime(user.CreatedAt))
	fmt.Fprintf(output, "Updated: %s\n", FormatLocalTime(user.UpdatedAt))
}
