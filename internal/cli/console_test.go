package cli_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/cli"
	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
)

func TestSessionBuildsUserAwarePromptAndClearsState(t *testing.T) {
	session := cli.NewSession("mediaarchive")
	if prompt := session.Prompt(); prompt != "anonymous@mediaarchive> " {
		t.Fatalf("unexpected anonymous prompt %q", prompt)
	}
	if session.Authenticated() {
		t.Fatal("expected a new session not to be authenticated")
	}

	session.Set("archive_user", "access-token")
	if prompt := session.Prompt(); prompt != "archive_user@mediaarchive> " {
		t.Errorf("unexpected authenticated prompt %q", prompt)
	}
	if !session.Authenticated() || session.AccessToken() != "access-token" {
		t.Error("expected authenticated session state")
	}

	session.Clear()
	if session.Authenticated() || session.AccessToken() != "" {
		t.Error("expected cleared authentication state")
	}
	if prompt := session.Prompt(); prompt != "anonymous@mediaarchive> " {
		t.Errorf("unexpected cleared prompt %q", prompt)
	}
}

func TestLineReaderReadsOnePromptedValue(t *testing.T) {
	reader := cli.NewLineReader(strings.NewReader("value\n"))
	var output bytes.Buffer

	value, available, err := reader.Read(
		context.Background(),
		&output,
		"Value: ",
	)
	if err != nil {
		t.Fatalf("read value: %v", err)
	}
	if !available || value != "value" {
		t.Errorf("unexpected read result: available=%t value=%q", available, value)
	}
	if output.String() != "Value: " {
		t.Errorf("unexpected prompt %q", output.String())
	}
}

func TestReadConfirmedSecretRetriesAndClearsRejectedValues(t *testing.T) {
	secrets := [][]byte{
		{},
		[]byte("first passphrase"),
		[]byte("mistyped passphrase"),
		[]byte("final passphrase"),
		[]byte("final passphrase"),
	}
	index := 0
	var reported []error

	secret, err := cli.ReadConfirmedSecret(
		func(string) ([]byte, error) {
			value := secrets[index]
			index++

			return value, nil
		},
		func(argError error) {
			reported = append(reported, argError)
		},
	)
	if err != nil {
		t.Fatalf("read confirmed secret: %v", err)
	}
	if string(secret) != "final passphrase" {
		t.Errorf("unexpected accepted secret %q", secret)
	}
	if len(reported) != 2 {
		t.Errorf("expected required-value and mismatch reports, got %v", reported)
	}
	for _, secretIndex := range []int{0, 1, 2, 4} {
		rejected := secrets[secretIndex]
		for byteIndex, value := range rejected {
			if value != 0 {
				t.Errorf("rejected secret byte %d was not cleared", byteIndex)
			}
		}
	}

	cli.ClearSecret(secret)
	for byteIndex, value := range secret {
		if value != 0 {
			t.Errorf("accepted secret byte %d was not cleared", byteIndex)
		}
	}
}

func TestPrintErrorHidesWrappedAPIDetails(t *testing.T) {
	apiError := &apiclient.APIError{
		StatusCode: http.StatusForbidden,
		Code:       "forbidden",
		Message:    "The operation is not permitted.",
	}
	err := errors.New("internal detail")
	wrapped := errors.Join(err, apiError)
	var output bytes.Buffer

	cli.PrintError(&output, wrapped)
	if output.String() != "Error [forbidden]: The operation is not permitted.\n" {
		t.Errorf("unexpected safe error output %q", output.String())
	}
}
