package content_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/content"
)

const locationMediaID = "123e4567-e89b-12d3-a456-426614174000"

func TestNewLocationNormalizesTimestamp(t *testing.T) {
	storedAt := time.Date(2026, 9, 20, 14, 30, 0, 0, time.FixedZone("test", 2*60*60))

	location, err := content.NewLocation(
		locationMediaID,
		"12/"+locationMediaID,
		storedAt,
	)
	if err != nil {
		t.Fatalf("create content location: %v", err)
	}
	if location.StoredAt.Location() != time.UTC {
		t.Fatalf("expected UTC timestamp, got %v", location.StoredAt.Location())
	}
	if !location.StoredAt.Equal(storedAt) {
		t.Fatalf("expected instant %v, got %v", storedAt, location.StoredAt)
	}
}

func TestNewLocationRejectsInvalidValues(t *testing.T) {
	testCases := []struct {
		name       string
		mediaID    string
		storageKey string
		storedAt   time.Time
		expected   error
	}{
		{"invalid media ID", "not-a-uuid", "12/content", time.Now(), content.ErrInvalidMediaID},
		{"empty key", locationMediaID, "", time.Now(), content.ErrInvalidStorageKey},
		{"absolute key", locationMediaID, "/12/content", time.Now(), content.ErrInvalidStorageKey},
		{"parent traversal", locationMediaID, "12/../content", time.Now(), content.ErrInvalidStorageKey},
		{"current directory", locationMediaID, "12/./content", time.Now(), content.ErrInvalidStorageKey},
		{"Windows separator", locationMediaID, `12\content`, time.Now(), content.ErrInvalidStorageKey},
		{"drive separator", locationMediaID, "C:/content", time.Now(), content.ErrInvalidStorageKey},
		{"surrounding space", locationMediaID, " 12/content", time.Now(), content.ErrInvalidStorageKey},
		{"control character", locationMediaID, "12/content\n", time.Now(), content.ErrInvalidStorageKey},
		{"long key", locationMediaID, strings.Repeat("a", 256), time.Now(), content.ErrInvalidStorageKey},
		{"zero timestamp", locationMediaID, "12/content", time.Time{}, content.ErrInvalidStoredAt},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := content.NewLocation(
				testCase.mediaID,
				testCase.storageKey,
				testCase.storedAt,
			)
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("expected %v, got %v", testCase.expected, err)
			}
		})
	}
}
