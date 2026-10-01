package media_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

type recordingContentStore struct {
	stored      content.Stored
	putError    error
	deleteError error
	putCalls    int
	deletedKey  string
	maximumSize int64
	source      io.Reader
}

func (store *recordingContentStore) Put(
	_ context.Context,
	_ string,
	argSource io.Reader,
	argMaximumSize int64,
) (content.Stored, error) {
	store.putCalls++
	store.source = argSource
	store.maximumSize = argMaximumSize

	return store.stored, store.putError
}

func (store *recordingContentStore) Delete(
	argContext context.Context,
	argStorageKey string,
) error {
	if err := argContext.Err(); err != nil {
		return err
	}
	store.deletedKey = argStorageKey

	return store.deleteError
}

type recordingManagedCreator struct {
	item     domainmedia.Item
	location content.Location
	err      error
	calls    int
}

func (creator *recordingManagedCreator) CreateManaged(
	_ context.Context,
	argItem domainmedia.Item,
	argLocation content.Location,
) error {
	creator.calls++
	creator.item = argItem
	creator.location = argLocation

	return creator.err
}

func TestUploadItemPersistsServerDerivedContentMetadata(t *testing.T) {
	data := []byte("uploaded content")
	checksum := sha256.Sum256(data)
	store := &recordingContentStore{stored: content.Stored{
		StorageKey: "12/" + serviceMediaID,
		Size:       int64(len(data)),
		Checksum:   checksum,
	}}
	repository := &recordingManagedCreator{}
	now := serviceTime()
	service := newUploadService(t, repository, store, now, 4096)
	actor := serviceUser(serviceOwnerID, identity.RoleEditor, true)
	source := bytes.NewReader(data)

	item, err := service.UploadItem(
		context.Background(),
		actor,
		uploadInput(source),
	)
	if err != nil {
		t.Fatalf("upload media: %v", err)
	}
	if store.putCalls != 1 || store.source != source || store.maximumSize != 4096 {
		t.Fatalf("unexpected store call: %+v", store)
	}
	if repository.calls != 1 || !reflect.DeepEqual(repository.item, item) {
		t.Fatalf("expected persisted item %+v, got %+v", item, repository.item)
	}
	if item.ID != serviceMediaID || item.OwnerID != actor.ID ||
		item.Size != int64(len(data)) || item.Checksum != checksum ||
		!item.CreatedAt.Equal(now) || !item.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected uploaded media: %+v", item)
	}
	if repository.location.MediaID != item.ID ||
		repository.location.StorageKey != store.stored.StorageKey ||
		!repository.location.StoredAt.Equal(now) {
		t.Fatalf("unexpected content location: %+v", repository.location)
	}
	if store.deletedKey != "" {
		t.Fatalf("expected no compensation, deleted %q", store.deletedKey)
	}
}

func TestUploadItemRejectsUnauthorizedActorBeforeStorage(t *testing.T) {
	for name, actor := range map[string]identity.User{
		"viewer":   serviceUser(serviceOwnerID, identity.RoleViewer, true),
		"inactive": serviceUser(serviceOwnerID, identity.RoleEditor, false),
	} {
		t.Run(name, func(t *testing.T) {
			store := &recordingContentStore{}
			service := newUploadService(
				t,
				&recordingManagedCreator{},
				store,
				serviceTime(),
				4096,
			)

			_, err := service.UploadItem(
				context.Background(),
				actor,
				uploadInput(bytes.NewReader([]byte("content"))),
			)
			if !errors.Is(err, appmedia.ErrCreationForbidden) {
				t.Fatalf("expected ErrCreationForbidden, got %v", err)
			}
			if store.putCalls != 0 {
				t.Fatalf("expected no storage call, got %d", store.putCalls)
			}
		})
	}
}

func TestUploadItemReturnsStoreFailureWithoutPersistence(t *testing.T) {
	storeError := errors.New("store unavailable")
	store := &recordingContentStore{putError: storeError}
	repository := &recordingManagedCreator{}
	service := newUploadService(t, repository, store, serviceTime(), 4096)

	_, err := service.UploadItem(
		context.Background(),
		serviceUser(serviceOwnerID, identity.RoleAdmin, true),
		uploadInput(bytes.NewReader([]byte("content"))),
	)
	if !errors.Is(err, storeError) {
		t.Fatalf("expected storage error, got %v", err)
	}
	if repository.calls != 0 || store.deletedKey != "" {
		t.Fatalf("expected no persistence or compensation: %+v %+v", repository, store)
	}
}

func TestUploadItemCompensatesValidationFailure(t *testing.T) {
	store := successfulRecordingContentStore()
	repository := &recordingManagedCreator{}
	service := newUploadService(t, repository, store, serviceTime(), 4096)
	input := uploadInput(bytes.NewReader([]byte("content")))
	input.Title = ""

	_, err := service.UploadItem(
		context.Background(),
		serviceUser(serviceOwnerID, identity.RoleEditor, true),
		input,
	)
	if !errors.Is(err, domainmedia.ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
	if repository.calls != 0 || store.deletedKey != store.stored.StorageKey {
		t.Fatalf("expected validation compensation: %+v %+v", repository, store)
	}
}

func TestUploadItemCompensatesPersistenceFailure(t *testing.T) {
	persistenceError := errors.New("database unavailable")
	store := successfulRecordingContentStore()
	service := newUploadService(
		t,
		&recordingManagedCreator{err: persistenceError},
		store,
		serviceTime(),
		4096,
	)

	_, err := service.UploadItem(
		context.Background(),
		serviceUser(serviceOwnerID, identity.RoleEditor, true),
		uploadInput(bytes.NewReader([]byte("content"))),
	)
	if !errors.Is(err, persistenceError) {
		t.Fatalf("expected persistence error, got %v", err)
	}
	if store.deletedKey != store.stored.StorageKey {
		t.Fatalf("expected compensation for %q, got %q", store.stored.StorageKey, store.deletedKey)
	}
}

func TestUploadItemReportsFailedCompensation(t *testing.T) {
	persistenceError := errors.New("database unavailable")
	cleanupError := errors.New("content removal failed")
	store := successfulRecordingContentStore()
	store.deleteError = cleanupError
	service := newUploadService(
		t,
		&recordingManagedCreator{err: persistenceError},
		store,
		serviceTime(),
		4096,
	)

	_, err := service.UploadItem(
		context.Background(),
		serviceUser(serviceOwnerID, identity.RoleEditor, true),
		uploadInput(bytes.NewReader([]byte("content"))),
	)
	if !errors.Is(err, persistenceError) || !errors.Is(err, cleanupError) {
		t.Fatalf("expected persistence and cleanup errors, got %v", err)
	}
}

func TestUploadItemCompensatesWithCanceledRequestContext(t *testing.T) {
	store := successfulRecordingContentStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := newUploadService(
		t,
		&recordingManagedCreator{err: context.Canceled},
		store,
		serviceTime(),
		4096,
	)

	_, err := service.UploadItem(
		ctx,
		serviceUser(serviceOwnerID, identity.RoleEditor, true),
		uploadInput(bytes.NewReader([]byte("content"))),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if store.deletedKey != store.stored.StorageKey {
		t.Fatalf("expected cleanup with detached context, got %q", store.deletedKey)
	}
}

func TestNewUploadServiceRejectsInvalidMaximumSize(t *testing.T) {
	_, err := appmedia.NewUploadService(
		&recordingManagedCreator{},
		&recordingContentStore{},
		func() string { return serviceMediaID },
		time.Now,
		0,
	)
	if !errors.Is(err, appmedia.ErrInvalidMaximumUploadSize) {
		t.Fatalf("expected ErrInvalidMaximumUploadSize, got %v", err)
	}
}

func successfulRecordingContentStore() *recordingContentStore {
	checksum := sha256.Sum256([]byte("content"))

	return &recordingContentStore{stored: content.Stored{
		StorageKey: "12/" + serviceMediaID,
		Size:       7,
		Checksum:   checksum,
	}}
}

func newUploadService(
	argTest *testing.T,
	argRepository appmedia.ManagedCreator,
	argStore content.Store,
	argNow time.Time,
	argMaximumSize int64,
) *appmedia.UploadService {
	argTest.Helper()
	service, err := appmedia.NewUploadService(
		argRepository,
		argStore,
		func() string { return serviceMediaID },
		func() time.Time { return argNow },
		argMaximumSize,
	)
	if err != nil {
		argTest.Fatalf("create upload service: %v", err)
	}

	return service
}

func uploadInput(argSource io.Reader) appmedia.UploadInput {
	return appmedia.UploadInput{
		Title:            "Uploaded Security Book",
		Authors:          []string{"Example Author"},
		OriginalFilename: "security-book.pdf",
		Type:             domainmedia.TypeBook,
		MIMEType:         "application/pdf",
		Source:           argSource,
	}
}
