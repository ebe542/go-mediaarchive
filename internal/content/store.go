// Package content defines storage-independent managed-content contracts.
package content

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
)

var (
	// ErrInvalidMediaID indicates that a content operation received an invalid
	// media identity.
	ErrInvalidMediaID = errors.New("invalid content media ID")

	// ErrInvalidStorageKey indicates a key outside the managed key format.
	ErrInvalidStorageKey = errors.New("invalid content storage key")

	// ErrInvalidSizeLimit indicates a non-positive maximum content size.
	ErrInvalidSizeLimit = errors.New("invalid content size limit")

	// ErrInvalidSource indicates that no content reader was supplied.
	ErrInvalidSource = errors.New("invalid content source")

	// ErrEmpty indicates an upload that contains no bytes.
	ErrEmpty = errors.New("empty content")

	// ErrTooLarge indicates content beyond the configured size limit.
	ErrTooLarge = errors.New("content too large")

	// ErrConflict indicates that managed content already exists for a key.
	ErrConflict = errors.New("content conflict")

	// ErrNotFound indicates that no managed content exists for a key.
	ErrNotFound = errors.New("content not found")
)

// Stored describes content after it has been completely written. StorageKey is
// opaque outside storage adapters and must never be exposed through the API.
type Stored struct {
	StorageKey string
	Size       int64
	Checksum   [sha256.Size]byte
}

// Store persists and removes managed media content.
type Store interface {
	Put(
		argContext context.Context,
		argMediaID string,
		argSource io.Reader,
		argMaximumSize int64,
	) (Stored, error)
	Delete(argContext context.Context, argStorageKey string) error
}

// StagedDeletion represents content hidden from its published key but not yet
// irreversibly removed.
type StagedDeletion interface {
	Commit(argContext context.Context) error
	Rollback(argContext context.Context) error
}

// DeletionStore temporarily hides managed content before permanent deletion.
type DeletionStore interface {
	StageDelete(
		argContext context.Context,
		argStorageKey string,
	) (StagedDeletion, error)
}
