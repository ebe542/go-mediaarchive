// Package content defines storage-independent managed-content contracts.
package content

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"time"
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

// ReadSeekCloser combines the operations needed to stream complete content or
// selected byte ranges without exposing a filesystem path.
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// Opened describes an opened managed-content object. The caller must close
// Reader so the storage adapter can release all associated resources.
type Opened struct {
	Reader       ReadSeekCloser
	Size         int64
	LastModified time.Time
}

// Store persists and removes managed media content.
type Store interface {
	Put(
		ctx context.Context,
		mediaID string,
		source io.Reader,
		maximumSize int64,
	) (Stored, error)
	Delete(ctx context.Context, storageKey string) error
}

// ReadStore opens managed content through an opaque storage key.
type ReadStore interface {
	Open(ctx context.Context, storageKey string) (Opened, error)
}

// StagedDeletion represents content hidden from its published key but not yet
// irreversibly removed.
type StagedDeletion interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// DeletionStore temporarily hides managed content before permanent deletion.
type DeletionStore interface {
	StageDelete(
		ctx context.Context,
		storageKey string,
	) (StagedDeletion, error)
}
