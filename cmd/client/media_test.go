package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	sharedcli "github.com/ebe542/go-mediaarchive/internal/cli"
	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const consoleMediaID = "123e4567-e89b-12d3-a456-426614174000"

func TestUserConsoleDiscoversAndReadsMedia(t *testing.T) {
	item := consoleMediaItem()
	api := &recordingUserAPI{
		loginSession: apiclient.Session{AccessToken: "access-token"},
		currentUser: apiclient.User{
			Username: "archive_user",
			Role:     identity.RoleViewer,
			Active:   true,
		},
		mediaPages: []apiclient.MediaPage{
			{Items: []apiclient.Media{item}, NextCursor: "next-cursor"},
			{Items: []apiclient.Media{}},
			{Items: []apiclient.Media{item}, NextCursor: "next-cursor"},
			{Items: []apiclient.Media{item}},
		},
		mediaByID: item,
	}
	input := strings.Join([]string{
		"login archive_user",
		"media list 2",
		"media next",
		"media first",
		"media search 1",
		"",
		"Security",
		"audio",
		"book",
		"media get " + consoleMediaID,
		"help",
		"logout",
		"exit",
	}, "\n") + "\n"
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	console := newUserConsole(
		api,
		strings.NewReader(input),
		&output,
		&errorOutput,
		queuedSecretReader(t, [][]byte{[]byte("login passphrase")}),
		time.Second,
	)

	if err := console.run(context.Background()); err != nil {
		t.Fatalf("run media console: %v", err)
	}
	if len(api.mediaCalls) != 4 {
		t.Fatalf("expected four media list calls, got %+v", api.mediaCalls)
	}
	if first := api.mediaCalls[0]; first.accessToken != "access-token" ||
		first.filter.Limit != 2 || first.filter.Query != "" || first.filter.Cursor != "" {
		t.Fatalf("unexpected first media call: %+v", first)
	}
	if api.mediaCalls[1].filter.Cursor != "next-cursor" {
		t.Fatalf("expected continuation cursor, got %+v", api.mediaCalls[1])
	}
	if api.mediaCalls[2].filter.Cursor != "" || api.mediaCalls[2].filter.Limit != 2 {
		t.Fatalf("expected first page with retained filter, got %+v", api.mediaCalls[2])
	}
	if search := api.mediaCalls[3].filter; search.Query != "Security" ||
		search.Type != domainmedia.TypeBook || search.Limit != 1 || search.Cursor != "" {
		t.Fatalf("unexpected media search: %+v", search)
	}
	if api.mediaByIDToken != "access-token" || api.mediaByIDValue != consoleMediaID {
		t.Fatalf("unexpected media read: token=%q id=%q", api.mediaByIDToken, api.mediaByIDValue)
	}
	for _, fragment := range []string{
		consoleMediaID,
		`"Security Engineering"`,
		"Example Author",
		sharedcli.FormatLocalTime(item.CreatedAt),
		"More media are available",
		"No media found.",
		"SHA-256: " + strings.Repeat("5a", sha256.Size),
		"media search [limit]",
	} {
		if !strings.Contains(output.String(), fragment) {
			t.Errorf("expected output %q, got %q", fragment, output.String())
		}
	}
	for _, fragment := range []string{
		"search text is required",
		"media type must be book, document, video, or blank",
	} {
		if !strings.Contains(errorOutput.String(), fragment) {
			t.Errorf("expected retry error %q, got %q", fragment, errorOutput.String())
		}
	}
	if console.mediaStarted || console.mediaCursor != "" ||
		console.mediaFilter != (apiclient.MediaDirectoryFilter{}) {
		t.Fatalf("expected logout to clear media pagination: %+v", console)
	}
}

func TestUserConsoleRequiresMediaSearchBeforePaging(t *testing.T) {
	console := newUserConsole(
		&recordingUserAPI{},
		strings.NewReader(""),
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(string) ([]byte, error) { return nil, nil },
		time.Second,
	)
	if _, err := console.execute(context.Background(), "media first"); err == nil ||
		!strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("expected authentication error, got %v", err)
	}
	console.session.Set("archive_user", "access-token")
	if err := console.nextMediaPage(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "start pagination") {
		t.Fatalf("expected pagination start error, got %v", err)
	}
	console.mediaStarted = true
	if err := console.nextMediaPage(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "no next media page") {
		t.Fatalf("expected final-page error, got %v", err)
	}
}

func TestPasswordChangeClearsMediaPagination(t *testing.T) {
	api := &recordingUserAPI{}
	secrets := [][]byte{
		[]byte("current passphrase"),
		[]byte("new passphrase"),
		[]byte("new passphrase"),
	}
	console := newUserConsole(
		api,
		strings.NewReader(""),
		&bytes.Buffer{},
		&bytes.Buffer{},
		queuedSecretReader(t, secrets),
		time.Second,
	)
	console.session.Set("archive_user", "access-token")
	console.mediaStarted = true
	console.mediaCursor = "next-cursor"
	console.mediaFilter = apiclient.MediaDirectoryFilter{Query: "Security", Limit: 10}

	if err := console.changePassword(context.Background()); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if console.mediaStarted || console.mediaCursor != "" ||
		console.mediaFilter != (apiclient.MediaDirectoryFilter{}) {
		t.Fatalf("expected password change to clear media pagination: %+v", console)
	}
	assertSecretsCleared(t, secrets)
}

func consoleMediaItem() apiclient.Media {
	var checksum [sha256.Size]byte
	copy(checksum[:], bytes.Repeat([]byte{0x5a}, sha256.Size))

	return apiclient.Media{
		ID:               consoleMediaID,
		Title:            "Security Engineering",
		Authors:          []string{"Example Author"},
		OriginalFilename: "security-engineering.pdf",
		Type:             domainmedia.TypeBook,
		MIMEType:         "application/pdf",
		Size:             4096,
		Checksum:         checksum,
		OwnerID:          "223e4567-e89b-12d3-a456-426614174000",
		CreatedAt:        time.Date(2026, time.October, 27, 8, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, time.October, 27, 9, 0, 0, 0, time.UTC),
	}
}
