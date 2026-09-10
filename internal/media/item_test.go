package media_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/media"
)

const (
	validMediaID = "bc3516f0-a8e5-45b9-9004-b2f402880c97"
	validOwnerID = "3f74e74d-e237-4bd4-a9bb-3407c38dd16f"
)

func TestNewItemNormalizesMinimalMetadata(t *testing.T) {
	location := time.FixedZone("test", 2*60*60)
	createdAt := time.Date(2026, time.September, 9, 12, 0, 0, 0, location)
	updatedAt := createdAt.Add(time.Hour)
	authors := []string{"  First Author  ", "Second Author"}
	checksum := bytes.Repeat([]byte{0x5a}, sha256.Size)

	item, err := media.NewItem(
		validMediaID,
		"  Security Engineering  ",
		authors,
		"  security-engineering.pdf  ",
		media.TypeBook,
		"APPLICATION/PDF",
		4096,
		checksum,
		validOwnerID,
		createdAt,
		updatedAt,
	)
	if err != nil {
		t.Fatalf("create media item: %v", err)
	}

	if item.Title != "Security Engineering" ||
		item.OriginalFilename != "security-engineering.pdf" ||
		item.MIMEType != "application/pdf" {
		t.Errorf("unexpected normalized metadata: %+v", item)
	}
	if len(item.Authors) != 2 ||
		item.Authors[0] != "First Author" ||
		item.Authors[1] != "Second Author" {
		t.Errorf("unexpected authors: %v", item.Authors)
	}
	if item.CreatedAt.Location() != time.UTC || item.UpdatedAt.Location() != time.UTC {
		t.Errorf("expected UTC timestamps, got %v and %v", item.CreatedAt, item.UpdatedAt)
	}

	authors[0] = "Changed Author"
	checksum[0] = 0
	if item.Authors[0] != "First Author" || item.Checksum[0] != 0x5a {
		t.Fatal("expected media item not to alias caller-owned input slices")
	}
}

func TestNewItemAllowsNoAuthors(t *testing.T) {
	item, err := newValidItem(nil)
	if err != nil {
		t.Fatalf("create media item without authors: %v", err)
	}
	if item.Authors == nil || len(item.Authors) != 0 {
		t.Fatalf("expected a non-nil empty author list, got %#v", item.Authors)
	}
}

func TestMediaTypeValid(t *testing.T) {
	tests := map[string]struct {
		mediaType media.Type
		valid     bool
	}{
		"book":     {media.TypeBook, true},
		"document": {media.TypeDocument, true},
		"video":    {media.TypeVideo, true},
		"empty":    {media.Type(""), false},
		"unknown":  {media.Type("audio"), false},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			if actual := testCase.mediaType.Valid(); actual != testCase.valid {
				t.Errorf("expected validity %t, got %t", testCase.valid, actual)
			}
		})
	}
}

func TestNewItemRejectsInvalidScalarMetadata(t *testing.T) {
	tests := map[string]struct {
		mutate        func(*itemArguments)
		expectedError error
	}{
		"invalid media ID": {
			mutate:        func(argArguments *itemArguments) { argArguments.id = "media-id" },
			expectedError: media.ErrInvalidMediaID,
		},
		"nil media ID": {
			mutate:        func(argArguments *itemArguments) { argArguments.id = "00000000-0000-0000-0000-000000000000" },
			expectedError: media.ErrInvalidMediaID,
		},
		"empty title": {
			mutate:        func(argArguments *itemArguments) { argArguments.title = "  " },
			expectedError: media.ErrInvalidTitle,
		},
		"long title": {
			mutate:        func(argArguments *itemArguments) { argArguments.title = strings.Repeat("x", 201) },
			expectedError: media.ErrInvalidTitle,
		},
		"title control character": {
			mutate:        func(argArguments *itemArguments) { argArguments.title = "Security\nNotes" },
			expectedError: media.ErrInvalidTitle,
		},
		"unknown type": {
			mutate:        func(argArguments *itemArguments) { argArguments.mediaType = media.Type("audio") },
			expectedError: media.ErrInvalidMediaType,
		},
		"zero size": {
			mutate:        func(argArguments *itemArguments) { argArguments.size = 0 },
			expectedError: media.ErrInvalidSize,
		},
		"negative size": {
			mutate:        func(argArguments *itemArguments) { argArguments.size = -1 },
			expectedError: media.ErrInvalidSize,
		},
		"invalid owner ID": {
			mutate:        func(argArguments *itemArguments) { argArguments.ownerID = "owner-id" },
			expectedError: media.ErrInvalidOwnerID,
		},
		"zero creation time": {
			mutate:        func(argArguments *itemArguments) { argArguments.createdAt = time.Time{} },
			expectedError: media.ErrInvalidTimestamp,
		},
		"zero update time": {
			mutate:        func(argArguments *itemArguments) { argArguments.updatedAt = time.Time{} },
			expectedError: media.ErrInvalidTimestamp,
		},
		"update before creation": {
			mutate: func(argArguments *itemArguments) {
				argArguments.updatedAt = argArguments.createdAt.Add(-time.Second)
			},
			expectedError: media.ErrInvalidTimestamp,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			arguments := validItemArguments()
			testCase.mutate(&arguments)

			_, err := arguments.newItem()
			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("expected %v, got %v", testCase.expectedError, err)
			}
		})
	}
}

func TestNewItemRejectsInvalidAuthors(t *testing.T) {
	tests := map[string][]string{
		"empty author":      {"Author", "  "},
		"long author":       {strings.Repeat("a", 101)},
		"control character": {"First\nAuthor"},
		"duplicate author":  {"Same Author", " Same Author "},
		"too many authors":  makeAuthors(21),
	}

	for name, authors := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := newValidItem(authors)
			if !errors.Is(err, media.ErrInvalidAuthors) {
				t.Fatalf("expected ErrInvalidAuthors, got %v", err)
			}
		})
	}
}

func TestNewItemRejectsUnsafeOriginalFilenames(t *testing.T) {
	filenames := map[string]string{
		"empty":             "  ",
		"too long":          strings.Repeat("f", 256),
		"current directory": ".",
		"parent directory":  "..",
		"Unix path":         "private/book.pdf",
		"Windows path":      `private\book.pdf`,
		"drive designator":  "C:book.pdf",
		"control character": "book\n.pdf",
	}

	for name, filename := range filenames {
		t.Run(name, func(t *testing.T) {
			arguments := validItemArguments()
			arguments.originalFilename = filename

			_, err := arguments.newItem()
			if !errors.Is(err, media.ErrInvalidOriginalFilename) {
				t.Fatalf("expected ErrInvalidOriginalFilename, got %v", err)
			}
		})
	}
}

func TestNewItemRejectsInvalidMIMETypes(t *testing.T) {
	mimeTypes := map[string]string{
		"empty":         "",
		"malformed":     "application",
		"parameterized": "text/plain; charset=utf-8",
		"too long":      "application/" + strings.Repeat("x", 116),
	}

	for name, mimeType := range mimeTypes {
		t.Run(name, func(t *testing.T) {
			arguments := validItemArguments()
			arguments.mimeType = mimeType

			_, err := arguments.newItem()
			if !errors.Is(err, media.ErrInvalidMIMEType) {
				t.Fatalf("expected ErrInvalidMIMEType, got %v", err)
			}
		})
	}
}

func TestNewItemRejectsInvalidChecksumLengths(t *testing.T) {
	for name, checksum := range map[string][]byte{
		"too short": make([]byte, sha256.Size-1),
		"too long":  make([]byte, sha256.Size+1),
	} {
		t.Run(name, func(t *testing.T) {
			arguments := validItemArguments()
			arguments.checksum = checksum

			_, err := arguments.newItem()
			if !errors.Is(err, media.ErrInvalidChecksum) {
				t.Fatalf("expected ErrInvalidChecksum, got %v", err)
			}
		})
	}
}

type itemArguments struct {
	id               string
	title            string
	authors          []string
	originalFilename string
	mediaType        media.Type
	mimeType         string
	size             int64
	checksum         []byte
	ownerID          string
	createdAt        time.Time
	updatedAt        time.Time
}

func validItemArguments() itemArguments {
	now := time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC)

	return itemArguments{
		id:               validMediaID,
		title:            "Security Notes",
		authors:          []string{"Archive Author"},
		originalFilename: "security-notes.pdf",
		mediaType:        media.TypeDocument,
		mimeType:         "application/pdf",
		size:             1024,
		checksum:         bytes.Repeat([]byte{0x42}, sha256.Size),
		ownerID:          validOwnerID,
		createdAt:        now,
		updatedAt:        now,
	}
}

func newValidItem(argAuthors []string) (media.Item, error) {
	arguments := validItemArguments()
	arguments.authors = argAuthors

	return arguments.newItem()
}

func (arguments itemArguments) newItem() (media.Item, error) {
	return media.NewItem(
		arguments.id,
		arguments.title,
		arguments.authors,
		arguments.originalFilename,
		arguments.mediaType,
		arguments.mimeType,
		arguments.size,
		arguments.checksum,
		arguments.ownerID,
		arguments.createdAt,
		arguments.updatedAt,
	)
}

func makeAuthors(argCount int) []string {
	authors := make([]string, argCount)
	for index := range authors {
		authors[index] = fmt.Sprintf("Author %d", index+1)
	}

	return authors
}
