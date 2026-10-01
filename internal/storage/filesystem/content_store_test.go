package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/content"
)

const testMediaID = "123e4567-e89b-12d3-a456-426614174000"

func TestContentStorePutStreamsPrivateContent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "content")
	store, err := NewContentStore(root)
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}

	source := []byte("managed media content")
	stored, err := store.Put(
		context.Background(),
		testMediaID,
		bytes.NewReader(source),
		1024,
	)
	if err != nil {
		t.Fatalf("put content: %v", err)
	}

	expectedChecksum := sha256.Sum256(source)
	if stored.StorageKey != "12/"+testMediaID {
		t.Fatalf("unexpected storage key %q", stored.StorageKey)
	}
	if stored.Size != int64(len(source)) {
		t.Fatalf("expected size %d, got %d", len(source), stored.Size)
	}
	if stored.Checksum != expectedChecksum {
		t.Fatalf("unexpected checksum %x", stored.Checksum)
	}

	contentPath := filepath.Join(root, "12", testMediaID)
	written, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatalf("read stored content: %v", err)
	}
	if !bytes.Equal(written, source) {
		t.Fatalf("expected %q, got %q", source, written)
	}
	assertPrivateMode(t, root, 0o700)
	assertPrivateMode(t, filepath.Join(root, "12"), 0o700)
	assertPrivateMode(t, contentPath, 0o600)

	temporaryFiles, err := filepath.Glob(filepath.Join(root, "12", ".upload-*"))
	if err != nil {
		t.Fatalf("find temporary files: %v", err)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("expected no temporary files, got %v", temporaryFiles)
	}
}

func TestContentStorePutRejectsInvalidInputs(t *testing.T) {
	store, err := NewContentStore(t.TempDir())
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}

	testCases := []struct {
		name        string
		mediaID     string
		source      io.Reader
		maximumSize int64
		expected    error
	}{
		{"invalid media ID", "../content", bytes.NewReader([]byte("x")), 1, content.ErrInvalidMediaID},
		{"nil source", testMediaID, nil, 1, content.ErrInvalidSource},
		{"zero limit", testMediaID, bytes.NewReader([]byte("x")), 0, content.ErrInvalidSizeLimit},
		{"negative limit", testMediaID, bytes.NewReader([]byte("x")), -1, content.ErrInvalidSizeLimit},
		{"empty content", testMediaID, bytes.NewReader(nil), 1, content.ErrEmpty},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := store.Put(
				context.Background(),
				testCase.mediaID,
				testCase.source,
				testCase.maximumSize,
			)
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("expected %v, got %v", testCase.expected, err)
			}
		})
	}
}

func TestContentStorePutRejectsOversizedContentAndCleansUp(t *testing.T) {
	root := t.TempDir()
	store, err := NewContentStore(root)
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}

	_, err = store.Put(
		context.Background(),
		testMediaID,
		strings.NewReader("too large"),
		3,
	)
	if !errors.Is(err, content.ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
	assertEmptyShard(t, filepath.Join(root, "12"))
}

func TestContentStorePutHonorsCanceledContextAndCleansUp(t *testing.T) {
	root := t.TempDir()
	store, err := NewContentStore(root)
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = store.Put(ctx, testMediaID, strings.NewReader("content"), 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "12")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no shard directory, got %v", err)
	}
}

func TestContentStorePutRejectsExistingContent(t *testing.T) {
	root := t.TempDir()
	store, err := NewContentStore(root)
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}
	if _, err := store.Put(
		context.Background(),
		testMediaID,
		strings.NewReader("first"),
		100,
	); err != nil {
		t.Fatalf("put first content: %v", err)
	}

	_, err = store.Put(
		context.Background(),
		testMediaID,
		strings.NewReader("second"),
		100,
	)
	if !errors.Is(err, content.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	written, err := os.ReadFile(filepath.Join(root, "12", testMediaID))
	if err != nil {
		t.Fatalf("read original content: %v", err)
	}
	if string(written) != "first" {
		t.Fatalf("expected original content, got %q", written)
	}
	assertEmptyTemporaryFiles(t, filepath.Join(root, "12"))
}

func TestContentStoreDeleteRemovesManagedContent(t *testing.T) {
	root := t.TempDir()
	store, err := NewContentStore(root)
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}
	stored, err := store.Put(
		context.Background(),
		testMediaID,
		strings.NewReader("content"),
		100,
	)
	if err != nil {
		t.Fatalf("put content: %v", err)
	}

	if err := store.Delete(context.Background(), stored.StorageKey); err != nil {
		t.Fatalf("delete content: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "12", testMediaID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected content removal, got %v", err)
	}
	if err := store.Delete(context.Background(), stored.StorageKey); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestContentStoreDeleteRejectsUnsafeKeys(t *testing.T) {
	store, err := NewContentStore(t.TempDir())
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}

	for _, key := range []string{
		"",
		"../" + testMediaID,
		"12/../" + testMediaID,
		"13/" + testMediaID,
		`12\` + testMediaID,
		"/12/" + testMediaID,
	} {
		t.Run(key, func(t *testing.T) {
			err := store.Delete(context.Background(), key)
			if !errors.Is(err, content.ErrInvalidStorageKey) {
				t.Fatalf("expected ErrInvalidStorageKey, got %v", err)
			}
		})
	}
}

func TestContentStoreRejectsSymbolicShardDirectory(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "12")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	store, err := NewContentStore(root)
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}

	_, err = store.Put(
		context.Background(),
		testMediaID,
		strings.NewReader("content"),
		100,
	)
	if err == nil || !strings.Contains(err.Error(), "symbolic directory links") {
		t.Fatalf("expected symbolic link rejection, got %v", err)
	}
}

func assertPrivateMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inspect permissions for %q: %v", path, err)
	}
	if mode := info.Mode().Perm(); mode != expected {
		t.Fatalf("expected permissions %o for %q, got %o", expected, path, mode)
	}
}

func assertEmptyShard(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read shard directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty shard directory, got %d entries", len(entries))
	}
}

func assertEmptyTemporaryFiles(t *testing.T, path string) {
	t.Helper()
	temporaryFiles, err := filepath.Glob(filepath.Join(path, ".upload-*"))
	if err != nil {
		t.Fatalf("find temporary files: %v", err)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("expected no temporary files, got %v", temporaryFiles)
	}
}
