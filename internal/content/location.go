package content

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maximumStorageKeyLength = 255

var (
	// ErrInvalidStoredAt indicates a missing content-storage timestamp.
	ErrInvalidStoredAt = errors.New("invalid content storage timestamp")

	// ErrLocationConflict indicates an existing media or storage-key mapping.
	ErrLocationConflict = errors.New("content location conflict")

	// ErrLocationNotFound indicates that a medium has no managed content.
	ErrLocationNotFound = errors.New("content location not found")

	// ErrLocationMediaMismatch indicates a location associated with a different
	// media identity than the metadata stored with it.
	ErrLocationMediaMismatch = errors.New("content location media mismatch")
)

// Location associates a media identity with an opaque relative storage key.
type Location struct {
	MediaID    string
	StorageKey string
	StoredAt   time.Time
}

// NewLocation validates and creates a managed-content location.
func NewLocation(
	mediaID string,
	rawStorageKey string,
	storedAt time.Time,
) (Location, error) {
	parsedID, err := uuid.Parse(mediaID)
	if err != nil || parsedID == uuid.Nil || parsedID.String() != mediaID {
		return Location{}, contentMediaIDError()
	}

	storageKey, err := normalizeStorageKey(rawStorageKey)
	if err != nil {
		return Location{}, err
	}
	if storedAt.IsZero() {
		return Location{}, ErrInvalidStoredAt
	}

	return Location{
		MediaID:    mediaID,
		StorageKey: storageKey,
		StoredAt:   storedAt.UTC(),
	}, nil
}

// LocationRepository persists media-to-content associations.
type LocationRepository interface {
	Create(ctx context.Context, location Location) error
	FindByMediaID(ctx context.Context, mediaID string) (Location, error)
	Delete(ctx context.Context, mediaID string) error
}

func normalizeStorageKey(storageKey string) (string, error) {
	length := utf8.RuneCountInString(storageKey)
	if length < 1 || length > maximumStorageKeyLength {
		return "", fmt.Errorf(
			"%w: length must be between 1 and %d characters",
			ErrInvalidStorageKey,
			maximumStorageKeyLength,
		)
	}
	if storageKey != strings.TrimSpace(storageKey) ||
		path.IsAbs(storageKey) ||
		storageKey == "." ||
		storageKey == ".." ||
		path.Clean(storageKey) != storageKey ||
		strings.ContainsAny(storageKey, `\:`) {
		return "", fmt.Errorf(
			"%w: expected a normalized relative key",
			ErrInvalidStorageKey,
		)
	}
	for _, character := range storageKey {
		if unicode.IsControl(character) {
			return "", fmt.Errorf(
				"%w: control characters are not allowed",
				ErrInvalidStorageKey,
			)
		}
	}

	return storageKey, nil
}

func contentMediaIDError() error {
	return fmt.Errorf(
		"%w: expected a canonical lowercase UUID",
		ErrInvalidMediaID,
	)
}
