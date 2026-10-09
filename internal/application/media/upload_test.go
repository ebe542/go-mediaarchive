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
	"github.com/ebe542/go-mediaarchive/internal/audit"
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
	source io.Reader,
	maximumSize int64,
) (content.Stored, error) {
	store.putCalls++
	store.source = source
	store.maximumSize = maximumSize

	return store.stored, store.putError
}

func (store *recordingContentStore) Delete(
	ctx context.Context,
	storageKey string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.deletedKey = storageKey

	return store.deleteError
}

type recordingManagedCreator struct {
	item     domainmedia.Item
	location content.Location
	event    audit.Event
	err      error
	calls    int
}

func (creator *recordingManagedCreator) CreateManagedWithAudit(
	_ context.Context,
	item domainmedia.Item,
	location content.Location,
	event audit.Event,
) error {
	creator.calls++
	creator.item = item
	creator.location = location
	creator.event = event

	return creator.err
}

type recordingUploadAuditAppender struct {
	events []audit.Event
	err    error
}

func (appender *recordingUploadAuditAppender) Append(
	_ context.Context,
	event audit.Event,
) error {
	appender.events = append(appender.events, event)

	return appender.err
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
	if repository.event.Type != audit.TypeMediaUploaded ||
		repository.event.Outcome != audit.OutcomeSuccess ||
		repository.event.ActorID != actor.ID ||
		repository.event.TargetID != item.ID ||
		repository.event.TargetName != item.Title {
		t.Fatalf("unexpected upload audit event: %+v", repository.event)
	}
}

func TestUploadItemRejectsUnauthorizedActorBeforeStorage(t *testing.T) {
	for name, actor := range map[string]identity.User{
		"viewer":   serviceUser(serviceOwnerID, identity.RoleViewer, true),
		"inactive": serviceUser(serviceOwnerID, identity.RoleEditor, false),
	} {
		t.Run(name, func(t *testing.T) {
			store := &recordingContentStore{}
			appender := &recordingUploadAuditAppender{}
			service := newUploadServiceWithAudit(
				t,
				&recordingManagedCreator{},
				store,
				appender,
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
			if len(appender.events) != 1 ||
				appender.events[0].Outcome != audit.OutcomeDenied ||
				appender.events[0].Reason != audit.ReasonInsufficientRole ||
				appender.events[0].TargetID != "" ||
				appender.events[0].TargetName != "" {
				t.Fatalf("unexpected upload denial event: %+v", appender.events)
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
	appender := &recordingUploadAuditAppender{}
	service := newUploadServiceWithAudit(
		t,
		repository,
		store,
		appender,
		serviceTime(),
		4096,
	)
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
	if len(appender.events) != 1 ||
		appender.events[0].Outcome != audit.OutcomeDenied ||
		appender.events[0].Reason != audit.ReasonInvalidInput ||
		appender.events[0].TargetName != "" {
		t.Fatalf("unexpected invalid upload event: %+v", appender.events)
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
	appender := &recordingUploadAuditAppender{}
	service := newUploadServiceWithAudit(
		t,
		&recordingManagedCreator{err: persistenceError},
		store,
		appender,
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
	if len(appender.events) != 1 ||
		appender.events[0].Outcome != audit.OutcomeFailure ||
		appender.events[0].Reason != audit.ReasonOperationFailure ||
		appender.events[0].TargetID != serviceMediaID ||
		appender.events[0].TargetName != "" {
		t.Fatalf("unexpected failed upload event: %+v", appender.events)
	}
}

func TestUploadItemFailsClosedWhenDenialCannotBeAudited(t *testing.T) {
	auditError := errors.New("synthetic audit failure")
	service := newUploadServiceWithAudit(
		t,
		&recordingManagedCreator{},
		&recordingContentStore{},
		&recordingUploadAuditAppender{err: auditError},
		serviceTime(),
		4096,
	)

	_, err := service.UploadItem(
		context.Background(),
		serviceUser(serviceOwnerID, identity.RoleViewer, true),
		uploadInput(bytes.NewReader([]byte("content"))),
	)
	if !errors.Is(err, auditError) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	if errors.Is(err, appmedia.ErrCreationForbidden) {
		t.Fatalf("expected audit failure to hide the denial, got %v", err)
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
		&recordingUploadAuditAppender{},
		func() string { return "923e4567-e89b-12d3-a456-426614174000" },
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
	test *testing.T,
	repository appmedia.ManagedCreator,
	store content.Store,
	now time.Time,
	maximumSize int64,
) *appmedia.UploadService {
	return newUploadServiceWithAudit(
		test,
		repository,
		store,
		&recordingUploadAuditAppender{},
		now,
		maximumSize,
	)
}

func newUploadServiceWithAudit(
	test *testing.T,
	repository appmedia.ManagedCreator,
	store content.Store,
	appender audit.Appender,
	now time.Time,
	maximumSize int64,
) *appmedia.UploadService {
	test.Helper()
	service, err := appmedia.NewUploadService(
		repository,
		store,
		func() string { return serviceMediaID },
		appender,
		func() string { return "923e4567-e89b-12d3-a456-426614174000" },
		func() time.Time { return now },
		maximumSize,
	)
	if err != nil {
		test.Fatalf("create upload service: %v", err)
	}

	return service
}

func uploadInput(source io.Reader) appmedia.UploadInput {
	return appmedia.UploadInput{
		Title:            "Uploaded Security Book",
		Authors:          []string{"Example Author"},
		OriginalFilename: "security-book.pdf",
		Type:             domainmedia.TypeBook,
		MIMEType:         "application/pdf",
		Source:           source,
	}
}
