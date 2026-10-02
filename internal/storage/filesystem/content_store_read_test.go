package filesystem

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/content"
)

const secondTestMediaID = "223e4567-e89b-12d3-a456-426614174000"

func TestContentStoreOpensSeekableContent(t *testing.T) {
	store, stored := putTestContent(t, testMediaID, "managed content")

	opened, err := store.Open(context.Background(), stored.StorageKey)
	if err != nil {
		t.Fatalf("open content: %v", err)
	}
	defer func() {
		if err := opened.Reader.Close(); err != nil {
			t.Fatalf("close content: %v", err)
		}
	}()

	if opened.Size != int64(len("managed content")) {
		t.Fatalf("expected size %d, got %d", len("managed content"), opened.Size)
	}
	if opened.LastModified.IsZero() {
		t.Fatal("expected a last-modified time")
	}
	if _, err := opened.Reader.Seek(8, io.SeekStart); err != nil {
		t.Fatalf("seek content: %v", err)
	}
	written, err := io.ReadAll(opened.Reader)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if string(written) != "content" {
		t.Fatalf("expected seeked content, got %q", written)
	}
}

func TestContentStoreOpenRejectsInvalidAndMissingKeys(t *testing.T) {
	store, _ := putTestContent(t, testMediaID, "content")

	if _, err := store.Open(context.Background(), "../content"); !errors.Is(err, content.ErrInvalidStorageKey) {
		t.Fatalf("expected ErrInvalidStorageKey, got %v", err)
	}
	missingKey := storageKeyForMedia(secondTestMediaID)
	if _, err := store.Open(context.Background(), missingKey); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestContentStoreOpenRejectsSymbolicAndNonRegularContent(t *testing.T) {
	t.Run("symbolic link", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewContentStore(root)
		if err != nil {
			t.Fatalf("create content store: %v", err)
		}
		shardPath := filepath.Join(root, "12")
		if err := os.Mkdir(shardPath, 0o700); err != nil {
			t.Fatalf("create shard: %v", err)
		}
		targetPath := filepath.Join(root, "target")
		if err := os.WriteFile(targetPath, []byte("outside"), 0o600); err != nil {
			t.Fatalf("write target: %v", err)
		}
		if err := os.Symlink(targetPath, filepath.Join(shardPath, testMediaID)); err != nil {
			t.Skipf("symbolic links unavailable: %v", err)
		}

		if _, err := store.Open(context.Background(), storageKeyForMedia(testMediaID)); err == nil {
			t.Fatal("expected symbolic content rejection")
		}
	})

	t.Run("directory", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewContentStore(root)
		if err != nil {
			t.Fatalf("create content store: %v", err)
		}
		contentPath := filepath.Join(root, "12", testMediaID)
		if err := os.MkdirAll(contentPath, 0o700); err != nil {
			t.Fatalf("create content directory: %v", err)
		}

		if _, err := store.Open(context.Background(), storageKeyForMedia(testMediaID)); err == nil {
			t.Fatal("expected non-regular content rejection")
		}
	})
}

func TestContentStoreAllowsParallelReaders(t *testing.T) {
	store, stored := putTestContent(t, testMediaID, "content")

	first, err := store.Open(context.Background(), stored.StorageKey)
	if err != nil {
		t.Fatalf("open first reader: %v", err)
	}
	defer first.Reader.Close()

	secondResult := make(chan content.Opened, 1)
	errorResult := make(chan error, 1)
	go func() {
		second, err := store.Open(context.Background(), stored.StorageKey)
		if err != nil {
			errorResult <- err
			return
		}
		secondResult <- second
	}()

	select {
	case second := <-secondResult:
		if err := second.Reader.Close(); err != nil {
			t.Fatalf("close second reader: %v", err)
		}
	case err := <-errorResult:
		t.Fatalf("open second reader: %v", err)
	case <-time.After(time.Second):
		t.Fatal("parallel reader was blocked")
	}
}

func TestContentStoreDeleteWaitsForOpenReader(t *testing.T) {
	store, stored := putTestContent(t, testMediaID, "content")
	opened, err := store.Open(context.Background(), stored.StorageKey)
	if err != nil {
		t.Fatalf("open content: %v", err)
	}

	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		result <- store.Delete(context.Background(), stored.StorageKey)
	}()
	<-started
	assertOperationBlocked(t, result)

	if err := opened.Reader.Close(); err != nil {
		t.Fatalf("close content: %v", err)
	}
	if err := awaitOperation(t, result); err != nil {
		t.Fatalf("delete content: %v", err)
	}
}

func TestContentStoreOpenWaitsForStagedDeletion(t *testing.T) {
	store, stored := putTestContent(t, testMediaID, "content")
	deletion, err := store.StageDelete(context.Background(), stored.StorageKey)
	if err != nil {
		t.Fatalf("stage deletion: %v", err)
	}

	started := make(chan struct{})
	result := make(chan openResult, 1)
	go func() {
		close(started)
		opened, err := store.Open(context.Background(), stored.StorageKey)
		result <- openResult{opened: opened, err: err}
	}()
	<-started
	assertOpenBlocked(t, result)

	if err := deletion.Rollback(context.Background()); err != nil {
		t.Fatalf("roll back deletion: %v", err)
	}
	openedResult := awaitOpen(t, result)
	if openedResult.err != nil {
		t.Fatalf("open restored content: %v", openedResult.err)
	}
	if err := openedResult.opened.Reader.Close(); err != nil {
		t.Fatalf("close restored content: %v", err)
	}
}

func TestStagedDeletionDoesNotBlockDifferentContent(t *testing.T) {
	root := t.TempDir()
	store, err := NewContentStore(root)
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}
	first, err := store.Put(
		context.Background(),
		testMediaID,
		strings.NewReader("first"),
		5,
	)
	if err != nil {
		t.Fatalf("put first content: %v", err)
	}
	second, err := store.Put(
		context.Background(),
		secondTestMediaID,
		strings.NewReader("second"),
		6,
	)
	if err != nil {
		t.Fatalf("put second content: %v", err)
	}
	deletion, err := store.StageDelete(context.Background(), first.StorageKey)
	if err != nil {
		t.Fatalf("stage first deletion: %v", err)
	}
	defer func() {
		if err := deletion.Rollback(context.Background()); err != nil {
			t.Fatalf("roll back first deletion: %v", err)
		}
	}()

	opened, err := store.Open(context.Background(), second.StorageKey)
	if err != nil {
		t.Fatalf("open different content: %v", err)
	}
	if err := opened.Reader.Close(); err != nil {
		t.Fatalf("close different content: %v", err)
	}
}

func TestStagedDeletionFinalizationReleasesLock(t *testing.T) {
	testCases := []struct {
		name     string
		finalize func(content.StagedDeletion) error
	}{
		{
			name: "commit",
			finalize: func(deletion content.StagedDeletion) error {
				return deletion.Commit(context.Background())
			},
		},
		{
			name: "rollback",
			finalize: func(deletion content.StagedDeletion) error {
				return deletion.Rollback(context.Background())
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store, stored := putTestContent(t, testMediaID, "content")
			deletion, err := store.StageDelete(context.Background(), stored.StorageKey)
			if err != nil {
				t.Fatalf("stage deletion: %v", err)
			}
			if err := testCase.finalize(deletion); err != nil {
				t.Fatalf("finalize deletion: %v", err)
			}

			result := make(chan openResult, 1)
			go func() {
				opened, err := store.Open(context.Background(), stored.StorageKey)
				result <- openResult{opened: opened, err: err}
			}()
			select {
			case openedResult := <-result:
				if openedResult.err == nil {
					if err := openedResult.opened.Reader.Close(); err != nil {
						t.Fatalf("close content: %v", err)
					}
				}
			case <-time.After(time.Second):
				t.Fatal("finalized deletion retained its lock")
			}
		})
	}
}

type openResult struct {
	opened content.Opened
	err    error
}

func putTestContent(
	t *testing.T,
	mediaID string,
	value string,
) (*ContentStore, content.Stored) {
	t.Helper()
	store, err := NewContentStore(t.TempDir())
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}
	stored, err := store.Put(
		context.Background(),
		mediaID,
		strings.NewReader(value),
		int64(len(value)),
	)
	if err != nil {
		t.Fatalf("put content: %v", err)
	}

	return store, stored
}

func assertOperationBlocked(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("expected operation to wait, got %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func assertOpenBlocked(t *testing.T, result <-chan openResult) {
	t.Helper()
	select {
	case openResult := <-result:
		t.Fatalf("expected open to wait, got %v", openResult.err)
	case <-time.After(50 * time.Millisecond):
	}
}

func awaitOperation(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for operation")
		return nil
	}
}

func awaitOpen(t *testing.T, result <-chan openResult) openResult {
	t.Helper()
	select {
	case opened := <-result:
		return opened
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for open")
		return openResult{}
	}
}
