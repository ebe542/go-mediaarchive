// Package filesystem stores managed media content on a local filesystem.
package filesystem

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/ebe542/go-mediaarchive/internal/content"
)

const copyBufferSize = 32 * 1024

// ContentStore keeps files below one private, server-controlled root.
type ContentStore struct {
	root string
}

// Verify at compile time that ContentStore implements the content contract.
var _ content.Store = (*ContentStore)(nil)
var _ content.DeletionStore = (*ContentStore)(nil)

type stagedDeletion struct {
	mutex        sync.Mutex
	originalPath string
	stagedPath   string
	finished     bool
}

// NewContentStore prepares a private root for managed content.
func NewContentStore(argRoot string) (*ContentStore, error) {
	if strings.TrimSpace(argRoot) == "" {
		return nil, errors.New("content root must not be empty")
	}

	root, err := filepath.Abs(argRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve content root: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create content root: %w", err)
	}
	if err := requirePrivateDirectory(root); err != nil {
		return nil, fmt.Errorf("validate content root: %w", err)
	}

	return &ContentStore{root: root}, nil
}

// Put streams content into a temporary file before publishing it under a key
// derived exclusively from the media ID.
func (store *ContentStore) Put(
	argContext context.Context,
	argMediaID string,
	argSource io.Reader,
	argMaximumSize int64,
) (content.Stored, error) {
	if err := validateMediaID(argMediaID); err != nil {
		return content.Stored{}, err
	}
	if argSource == nil {
		return content.Stored{}, content.ErrInvalidSource
	}
	if argMaximumSize <= 0 {
		return content.Stored{}, content.ErrInvalidSizeLimit
	}
	if err := argContext.Err(); err != nil {
		return content.Stored{}, err
	}
	if err := requirePrivateDirectory(store.root); err != nil {
		return content.Stored{}, fmt.Errorf("validate content root: %w", err)
	}

	storageKey := storageKeyForMedia(argMediaID)
	shardPath := filepath.Join(store.root, argMediaID[:2])
	if err := prepareShardDirectory(shardPath); err != nil {
		return content.Stored{}, err
	}

	temporary, err := os.CreateTemp(shardPath, ".upload-*")
	if err != nil {
		return content.Stored{}, fmt.Errorf("create temporary content: %w", err)
	}
	temporaryPath := temporary.Name()
	temporaryOpen := true
	defer func() {
		if temporaryOpen {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return content.Stored{}, fmt.Errorf("set temporary content permissions: %w", err)
	}

	hasher := sha256.New()
	written, err := copyBounded(
		argContext,
		io.MultiWriter(temporary, hasher),
		argSource,
		argMaximumSize,
	)
	if err != nil {
		return content.Stored{}, err
	}
	if written == 0 {
		return content.Stored{}, content.ErrEmpty
	}
	if err := temporary.Sync(); err != nil {
		return content.Stored{}, fmt.Errorf("flush temporary content: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporaryOpen = false
		return content.Stored{}, fmt.Errorf("close temporary content: %w", err)
	}
	temporaryOpen = false

	finalPath := filepath.Join(store.root, filepath.FromSlash(storageKey))
	if _, err := os.Lstat(finalPath); err == nil {
		return content.Stored{}, content.ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return content.Stored{}, fmt.Errorf("inspect final content: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		if _, inspectErr := os.Lstat(finalPath); inspectErr == nil {
			return content.Stored{}, content.ErrConflict
		}

		return content.Stored{}, fmt.Errorf("publish content: %w", err)
	}

	var checksum [sha256.Size]byte
	copy(checksum[:], hasher.Sum(nil))

	return content.Stored{
		StorageKey: storageKey,
		Size:       written,
		Checksum:   checksum,
	}, nil
}

// Delete removes one regular managed file. Missing content is reported so
// application compensation can distinguish an already absent object.
func (store *ContentStore) Delete(
	argContext context.Context,
	argStorageKey string,
) error {
	mediaID, err := mediaIDFromStorageKey(argStorageKey)
	if err != nil {
		return err
	}
	if err := argContext.Err(); err != nil {
		return err
	}
	if err := requirePrivateDirectory(store.root); err != nil {
		return fmt.Errorf("validate content root: %w", err)
	}

	shardPath := filepath.Join(store.root, mediaID[:2])
	if err := requirePrivateDirectory(shardPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return content.ErrNotFound
		}

		return fmt.Errorf("validate content directory: %w", err)
	}

	contentPath := filepath.Join(store.root, filepath.FromSlash(argStorageKey))
	info, err := os.Lstat(contentPath)
	if errors.Is(err, os.ErrNotExist) {
		return content.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect content for deletion: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("managed content is not a regular file")
	}
	if err := os.Remove(contentPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return content.ErrNotFound
		}

		return fmt.Errorf("delete content: %w", err)
	}

	return nil
}

// StageDelete atomically hides managed content under an internal deletion name.
func (store *ContentStore) StageDelete(
	argContext context.Context,
	argStorageKey string,
) (content.StagedDeletion, error) {
	mediaID, err := mediaIDFromStorageKey(argStorageKey)
	if err != nil {
		return nil, err
	}
	if err := argContext.Err(); err != nil {
		return nil, err
	}
	if err := requirePrivateDirectory(store.root); err != nil {
		return nil, fmt.Errorf("validate content root: %w", err)
	}

	shardPath := filepath.Join(store.root, mediaID[:2])
	if err := requirePrivateDirectory(shardPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, content.ErrNotFound
		}

		return nil, fmt.Errorf("validate content directory: %w", err)
	}

	originalPath := filepath.Join(store.root, filepath.FromSlash(argStorageKey))
	info, err := os.Lstat(originalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, content.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("inspect content for staged deletion: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("managed content is not a regular file")
	}

	stagedPath := filepath.Join(shardPath, ".delete-"+mediaID)
	if _, err := os.Lstat(stagedPath); err == nil {
		return nil, content.ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect staged deletion path: %w", err)
	}
	if err := os.Rename(originalPath, stagedPath); err != nil {
		return nil, fmt.Errorf("stage content deletion: %w", err)
	}

	return &stagedDeletion{
		originalPath: originalPath,
		stagedPath:   stagedPath,
	}, nil
}

func (deletion *stagedDeletion) Commit(argContext context.Context) error {
	deletion.mutex.Lock()
	defer deletion.mutex.Unlock()
	if deletion.finished {
		return errors.New("staged deletion is already finalized")
	}
	if err := argContext.Err(); err != nil {
		return err
	}
	if err := os.Remove(deletion.stagedPath); err != nil {
		return fmt.Errorf("commit content deletion: %w", err)
	}
	deletion.finished = true

	return nil
}

func (deletion *stagedDeletion) Rollback(argContext context.Context) error {
	deletion.mutex.Lock()
	defer deletion.mutex.Unlock()
	if deletion.finished {
		return errors.New("staged deletion is already finalized")
	}
	if err := argContext.Err(); err != nil {
		return err
	}
	if _, err := os.Lstat(deletion.originalPath); err == nil {
		return content.ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect content rollback path: %w", err)
	}
	if err := os.Rename(deletion.stagedPath, deletion.originalPath); err != nil {
		return fmt.Errorf("roll back content deletion: %w", err)
	}
	deletion.finished = true

	return nil
}

func copyBounded(
	argContext context.Context,
	argDestination io.Writer,
	argSource io.Reader,
	argMaximumSize int64,
) (int64, error) {
	limited := io.LimitReader(argSource, argMaximumSize+1)
	buffer := make([]byte, copyBufferSize)
	var written int64

	for {
		if err := argContext.Err(); err != nil {
			return 0, err
		}

		readCount, readErr := limited.Read(buffer)
		if readCount > 0 {
			remaining := argMaximumSize + 1 - written
			if int64(readCount) > remaining {
				readCount = int(remaining)
			}
			writeCount, writeErr := argDestination.Write(buffer[:readCount])
			written += int64(writeCount)
			if writeErr != nil {
				return 0, fmt.Errorf("write temporary content: %w", writeErr)
			}
			if writeCount != readCount {
				return 0, io.ErrShortWrite
			}
			if written > argMaximumSize {
				return 0, content.ErrTooLarge
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return 0, fmt.Errorf("%w: %w", content.ErrInvalidSource, readErr)
		}
	}
}

func prepareShardDirectory(argPath string) error {
	if err := os.Mkdir(argPath, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create content directory: %w", err)
	}
	if err := requirePrivateDirectory(argPath); err != nil {
		return fmt.Errorf("validate content directory: %w", err)
	}

	return nil
}

func requirePrivateDirectory(argPath string) error {
	info, err := os.Lstat(argPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("symbolic directory links are not allowed")
	}
	if !info.IsDir() {
		return errors.New("expected a directory")
	}
	if err := os.Chmod(argPath, 0o700); err != nil {
		return fmt.Errorf("set private directory permissions: %w", err)
	}

	return nil
}

func storageKeyForMedia(argMediaID string) string {
	return path.Join(argMediaID[:2], argMediaID)
}

func mediaIDFromStorageKey(argStorageKey string) (string, error) {
	if path.IsAbs(argStorageKey) || strings.Contains(argStorageKey, `\`) {
		return "", content.ErrInvalidStorageKey
	}
	parts := strings.Split(argStorageKey, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", content.ErrInvalidStorageKey
	}
	if err := validateMediaID(parts[1]); err != nil || parts[0] != parts[1][:2] {
		return "", content.ErrInvalidStorageKey
	}
	if storageKeyForMedia(parts[1]) != argStorageKey {
		return "", content.ErrInvalidStorageKey
	}

	return parts[1], nil
}

func validateMediaID(argMediaID string) error {
	parsed, err := uuid.Parse(argMediaID)
	if err != nil || parsed == uuid.Nil || parsed.String() != argMediaID {
		return content.ErrInvalidMediaID
	}

	return nil
}
