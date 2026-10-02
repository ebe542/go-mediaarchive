// Package media defines stored-media identities and content permissions.
package media

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maximumTitleLength            = 200
	maximumAuthorCount            = 20
	maximumAuthorLength           = 100
	maximumOriginalFilenameLength = 255
	maximumMIMETypeLength         = 127
)

// Type classifies a stored media item without introducing catalog-specific
// metadata.
type Type string

const (
	// TypeBook identifies written media commonly represented as an e-book.
	TypeBook Type = "book"

	// TypeDocument identifies general written or scanned material.
	TypeDocument Type = "document"

	// TypeVideo identifies audiovisual media.
	TypeVideo Type = "video"
)

// Valid reports whether the media type is supported.
func (mediaType Type) Valid() bool {
	switch mediaType {
	case TypeBook, TypeDocument, TypeVideo:
		return true
	default:
		return false
	}
}

var (
	// ErrInvalidMediaID indicates that a media ID is not a canonical UUID.
	ErrInvalidMediaID = errors.New("invalid media ID")

	// ErrInvalidTitle indicates that a media title violates domain limits.
	ErrInvalidTitle = errors.New("invalid media title")

	// ErrInvalidAuthors indicates that an author collection is malformed.
	ErrInvalidAuthors = errors.New("invalid media authors")

	// ErrInvalidOriginalFilename indicates unsafe descriptive filename metadata.
	ErrInvalidOriginalFilename = errors.New("invalid original filename")

	// ErrInvalidMediaType indicates an unsupported media classification.
	ErrInvalidMediaType = errors.New("invalid media type")

	// ErrInvalidMIMEType indicates malformed or parameterized content metadata.
	ErrInvalidMIMEType = errors.New("invalid MIME type")

	// ErrInvalidSize indicates a media item without positive content length.
	ErrInvalidSize = errors.New("invalid media size")

	// ErrInvalidChecksum indicates a value that is not a SHA-256 checksum.
	ErrInvalidChecksum = errors.New("invalid media checksum")

	// ErrInvalidOwnerID indicates that an owner ID is not a canonical UUID.
	ErrInvalidOwnerID = errors.New("invalid media owner ID")

	// ErrInvalidTimestamp indicates missing or inconsistent media timestamps.
	ErrInvalidTimestamp = errors.New("invalid media timestamp")
)

// Item represents minimal searchable metadata for one stored file. The
// original filename is descriptive and must never select a storage path.
type Item struct {
	ID               string
	Title            string
	Authors          []string
	OriginalFilename string
	Type             Type
	MIMEType         string
	Size             int64
	Checksum         [sha256.Size]byte
	OwnerID          string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// NewItem validates and creates a storage-independent media identity.
func NewItem(
	id string,
	rawTitle string,
	rawAuthors []string,
	rawFilename string,
	valueType Type,
	rawMIMEType string,
	size int64,
	checksumBytes []byte,
	ownerID string,
	createdAt time.Time,
	updatedAt time.Time,
) (Item, error) {
	if err := validateCanonicalUUID(id, ErrInvalidMediaID); err != nil {
		return Item{}, err
	}

	title := strings.TrimSpace(rawTitle)
	if length := utf8.RuneCountInString(title); length < 1 || length > maximumTitleLength {
		return Item{}, fmt.Errorf(
			"%w: length must be between 1 and %d characters",
			ErrInvalidTitle,
			maximumTitleLength,
		)
	}
	if containsControlCharacter(title) {
		return Item{}, fmt.Errorf(
			"%w: control characters are not allowed",
			ErrInvalidTitle,
		)
	}

	authors, err := normalizeAuthors(rawAuthors)
	if err != nil {
		return Item{}, err
	}

	originalFilename, err := normalizeOriginalFilename(rawFilename)
	if err != nil {
		return Item{}, err
	}

	if !valueType.Valid() {
		return Item{}, fmt.Errorf("%w: %q", ErrInvalidMediaType, valueType)
	}

	mimeType, err := normalizeMIMEType(rawMIMEType)
	if err != nil {
		return Item{}, err
	}

	if size <= 0 {
		return Item{}, fmt.Errorf("%w: size must be positive", ErrInvalidSize)
	}

	if len(checksumBytes) != sha256.Size {
		return Item{}, fmt.Errorf(
			"%w: expected %d bytes",
			ErrInvalidChecksum,
			sha256.Size,
		)
	}
	var checksum [sha256.Size]byte
	copy(checksum[:], checksumBytes)

	if err := validateCanonicalUUID(ownerID, ErrInvalidOwnerID); err != nil {
		return Item{}, err
	}
	if createdAt.IsZero() || updatedAt.IsZero() {
		return Item{}, fmt.Errorf("%w: timestamps must not be zero", ErrInvalidTimestamp)
	}
	if updatedAt.Before(createdAt) {
		return Item{}, fmt.Errorf(
			"%w: update time must not precede creation time",
			ErrInvalidTimestamp,
		)
	}

	return Item{
		ID:               id,
		Title:            title,
		Authors:          authors,
		OriginalFilename: originalFilename,
		Type:             valueType,
		MIMEType:         mimeType,
		Size:             size,
		Checksum:         checksum,
		OwnerID:          ownerID,
		CreatedAt:        createdAt.UTC(),
		UpdatedAt:        updatedAt.UTC(),
	}, nil
}

func validateCanonicalUUID(id string, inputError error) error {
	parsedID, err := uuid.Parse(id)
	if err != nil || parsedID == uuid.Nil || parsedID.String() != id {
		return fmt.Errorf("%w: expected a canonical lowercase UUID", inputError)
	}

	return nil
}

func normalizeAuthors(rawAuthors []string) ([]string, error) {
	if len(rawAuthors) > maximumAuthorCount {
		return nil, fmt.Errorf(
			"%w: at most %d authors are allowed",
			ErrInvalidAuthors,
			maximumAuthorCount,
		)
	}

	authors := make([]string, len(rawAuthors))
	seen := make(map[string]struct{}, len(rawAuthors))
	for index, author := range rawAuthors {
		normalized := strings.TrimSpace(author)
		length := utf8.RuneCountInString(normalized)
		if length < 1 || length > maximumAuthorLength {
			return nil, fmt.Errorf(
				"%w: author %d length must be between 1 and %d characters",
				ErrInvalidAuthors,
				index+1,
				maximumAuthorLength,
			)
		}
		if containsControlCharacter(normalized) {
			return nil, fmt.Errorf(
				"%w: author %d contains a control character",
				ErrInvalidAuthors,
				index+1,
			)
		}
		if _, exists := seen[normalized]; exists {
			return nil, fmt.Errorf(
				"%w: duplicate author %q",
				ErrInvalidAuthors,
				normalized,
			)
		}

		seen[normalized] = struct{}{}
		authors[index] = normalized
	}

	return authors, nil
}

func normalizeOriginalFilename(rawFilename string) (string, error) {
	filename := strings.TrimSpace(rawFilename)
	length := utf8.RuneCountInString(filename)
	if length < 1 || length > maximumOriginalFilenameLength {
		return "", fmt.Errorf(
			"%w: length must be between 1 and %d characters",
			ErrInvalidOriginalFilename,
			maximumOriginalFilenameLength,
		)
	}
	if filename == "." || filename == ".." || strings.ContainsAny(filename, `/\:`) {
		return "", fmt.Errorf(
			"%w: path syntax is not allowed",
			ErrInvalidOriginalFilename,
		)
	}
	if containsControlCharacter(filename) {
		return "", fmt.Errorf(
			"%w: control characters are not allowed",
			ErrInvalidOriginalFilename,
		)
	}

	return filename, nil
}

func normalizeMIMEType(mimeType string) (string, error) {
	value := strings.TrimSpace(mimeType)
	if length := utf8.RuneCountInString(value); length < 1 || length > maximumMIMETypeLength {
		return "", fmt.Errorf(
			"%w: length must be between 1 and %d characters",
			ErrInvalidMIMEType,
			maximumMIMETypeLength,
		)
	}

	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || len(parameters) != 0 {
		return "", fmt.Errorf(
			"%w: expected a base media type without parameters",
			ErrInvalidMIMEType,
		)
	}
	typeParts := strings.Split(mediaType, "/")
	if len(typeParts) != 2 || typeParts[0] == "" || typeParts[1] == "" {
		return "", fmt.Errorf(
			"%w: expected type/subtype syntax",
			ErrInvalidMIMEType,
		)
	}

	return mediaType, nil
}

func containsControlCharacter(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}

	return false
}
