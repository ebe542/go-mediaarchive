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
)

// Location associates a media identity with an opaque relative storage key.
type Location struct {
	MediaID    string
	StorageKey string
	StoredAt   time.Time
}

// NewLocation validates and creates a managed-content location.
func NewLocation(
	argMediaID string,
	argStorageKey string,
	argStoredAt time.Time,
) (Location, error) {
	parsedID, err := uuid.Parse(argMediaID)
	if err != nil || parsedID == uuid.Nil || parsedID.String() != argMediaID {
		return Location{}, contentMediaIDError()
	}

	storageKey, err := normalizeStorageKey(argStorageKey)
	if err != nil {
		return Location{}, err
	}
	if argStoredAt.IsZero() {
		return Location{}, ErrInvalidStoredAt
	}

	return Location{
		MediaID:    argMediaID,
		StorageKey: storageKey,
		StoredAt:   argStoredAt.UTC(),
	}, nil
}

// LocationRepository persists media-to-content associations.
type LocationRepository interface {
	Create(argContext context.Context, argLocation Location) error
	FindByMediaID(argContext context.Context, argMediaID string) (Location, error)
	Delete(argContext context.Context, argMediaID string) error
}

func normalizeStorageKey(argStorageKey string) (string, error) {
	length := utf8.RuneCountInString(argStorageKey)
	if length < 1 || length > maximumStorageKeyLength {
		return "", fmt.Errorf(
			"%w: length must be between 1 and %d characters",
			ErrInvalidStorageKey,
			maximumStorageKeyLength,
		)
	}
	if argStorageKey != strings.TrimSpace(argStorageKey) ||
		path.IsAbs(argStorageKey) ||
		argStorageKey == "." ||
		argStorageKey == ".." ||
		path.Clean(argStorageKey) != argStorageKey ||
		strings.ContainsAny(argStorageKey, `\:`) {
		return "", fmt.Errorf(
			"%w: expected a normalized relative key",
			ErrInvalidStorageKey,
		)
	}
	for _, character := range argStorageKey {
		if unicode.IsControl(character) {
			return "", fmt.Errorf(
				"%w: control characters are not allowed",
				ErrInvalidStorageKey,
			)
		}
	}

	return argStorageKey, nil
}

func contentMediaIDError() error {
	return fmt.Errorf(
		"%w: expected a canonical lowercase UUID",
		ErrInvalidMediaID,
	)
}
