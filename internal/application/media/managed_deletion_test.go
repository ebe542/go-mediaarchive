package media_test

import (
	"context"
	"errors"
	"testing"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingLocationFinder struct {
	location content.Location
	err      error
	calls    int
}

func (finder *recordingLocationFinder) FindByMediaID(
	_ context.Context,
	_ string,
) (content.Location, error) {
	finder.calls++

	return finder.location, finder.err
}

type recordingManagedDeletionRepository struct {
	mediaID    string
	storageKey string
	event      audit.Event
	err        error
}

func (repository *recordingManagedDeletionRepository) DeleteManagedWithAudit(
	_ context.Context,
	mediaID string,
	storageKey string,
	event audit.Event,
) error {
	repository.mediaID = mediaID
	repository.storageKey = storageKey
	repository.event = event

	return repository.err
}

type recordingStagedDeletion struct {
	commitError   error
	rollbackError error
	committed     bool
	rolledBack    bool
}

func (deletion *recordingStagedDeletion) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deletion.committed = true

	return deletion.commitError
}

func (deletion *recordingStagedDeletion) Rollback(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deletion.rolledBack = true

	return deletion.rollbackError
}

type recordingDeletionStore struct {
	deletion   *recordingStagedDeletion
	storageKey string
	err        error
}

func (store *recordingDeletionStore) StageDelete(
	_ context.Context,
	storageKey string,
) (content.StagedDeletion, error) {
	store.storageKey = storageKey

	return store.deletion, store.err
}

func TestManagedServiceDeletesContentAndRecords(t *testing.T) {
	item := serviceItem(t)
	baseRepository := &recordingMediaRepository{item: item}
	base := newMetadataService(baseRepository, &recordingGrantFinder{})
	location, err := content.NewLocation(item.ID, "12/"+item.ID, item.CreatedAt)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	finder := &recordingLocationFinder{location: location}
	repository := &recordingManagedDeletionRepository{}
	deletion := &recordingStagedDeletion{}
	store := &recordingDeletionStore{deletion: deletion}
	service := appmedia.NewManagedService(base, finder, repository, store)

	if err := service.DeleteItem(
		context.Background(),
		serviceUser(item.OwnerID, identity.RoleViewer, true),
		item.ID,
	); err != nil {
		t.Fatalf("delete managed media: %v", err)
	}
	if store.storageKey != location.StorageKey ||
		repository.mediaID != item.ID || repository.storageKey != location.StorageKey ||
		!deletion.committed || deletion.rolledBack {
		t.Fatalf("unexpected managed deletion: %+v %+v %+v", store, repository, deletion)
	}
	if baseRepository.deletedID != "" {
		t.Fatal("expected managed repository to own database deletion")
	}
	if repository.event.Type != audit.TypeMediaDeleted ||
		repository.event.Outcome != audit.OutcomeSuccess ||
		repository.event.TargetID != item.ID ||
		repository.event.TargetName != item.Title {
		t.Fatalf("unexpected managed deletion event: %+v", repository.event)
	}
}

func TestManagedServiceRollsBackContentOnDatabaseFailure(t *testing.T) {
	databaseError := errors.New("database unavailable")
	item := serviceItem(t)
	location, err := content.NewLocation(item.ID, "12/"+item.ID, item.CreatedAt)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	deletion := &recordingStagedDeletion{}
	service := appmedia.NewManagedService(
		newMetadataService(&recordingMediaRepository{item: item}, &recordingGrantFinder{}),
		&recordingLocationFinder{location: location},
		&recordingManagedDeletionRepository{err: databaseError},
		&recordingDeletionStore{deletion: deletion},
	)

	err = service.DeleteItem(
		context.Background(),
		serviceUser(item.OwnerID, identity.RoleViewer, true),
		item.ID,
	)
	if !errors.Is(err, databaseError) || !deletion.rolledBack || deletion.committed {
		t.Fatalf("expected database error and rollback, got %v %+v", err, deletion)
	}
}

func TestManagedServiceKeepsRollbackFailure(t *testing.T) {
	databaseError := errors.New("database unavailable")
	rollbackError := errors.New("restore failed")
	item := serviceItem(t)
	location, err := content.NewLocation(item.ID, "12/"+item.ID, item.CreatedAt)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	appender := &recordingUploadAuditAppender{}
	service := appmedia.NewManagedService(
		newMetadataServiceWithAudit(
			&recordingMediaRepository{item: item},
			&recordingGrantFinder{},
			appender,
		),
		&recordingLocationFinder{location: location},
		&recordingManagedDeletionRepository{err: databaseError},
		&recordingDeletionStore{deletion: &recordingStagedDeletion{rollbackError: rollbackError}},
	)

	err = service.DeleteItem(
		context.Background(),
		serviceUser(item.OwnerID, identity.RoleViewer, true),
		item.ID,
	)
	if !errors.Is(err, databaseError) || !errors.Is(err, rollbackError) {
		t.Fatalf("expected database and rollback errors, got %v", err)
	}
	if len(appender.events) != 1 ||
		appender.events[0].Outcome != audit.OutcomeFailure ||
		appender.events[0].Reason != audit.ReasonOperationFailure {
		t.Fatalf("unexpected rollback failure event: %+v", appender.events)
	}
}

func TestManagedServiceAuditsStagingFailure(t *testing.T) {
	stageError := errors.New("staging failed")
	item := serviceItem(t)
	location, err := content.NewLocation(item.ID, "12/"+item.ID, item.CreatedAt)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	appender := &recordingUploadAuditAppender{}
	service := appmedia.NewManagedService(
		newMetadataServiceWithAudit(
			&recordingMediaRepository{item: item},
			&recordingGrantFinder{},
			appender,
		),
		&recordingLocationFinder{location: location},
		&recordingManagedDeletionRepository{},
		&recordingDeletionStore{err: stageError},
	)

	err = service.DeleteItem(
		context.Background(),
		serviceUser(item.OwnerID, identity.RoleViewer, true),
		item.ID,
	)
	if !errors.Is(err, stageError) {
		t.Fatalf("expected staging error, got %v", err)
	}
	if len(appender.events) != 1 ||
		appender.events[0].Outcome != audit.OutcomeFailure ||
		appender.events[0].Reason != audit.ReasonOperationFailure {
		t.Fatalf("unexpected staging failure event: %+v", appender.events)
	}
}

func TestManagedServiceAuditsFinalizationFailure(t *testing.T) {
	commitError := errors.New("finalization failed")
	item := serviceItem(t)
	location, err := content.NewLocation(item.ID, "12/"+item.ID, item.CreatedAt)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	appender := &recordingUploadAuditAppender{}
	deletion := &recordingStagedDeletion{commitError: commitError}
	service := appmedia.NewManagedService(
		newMetadataServiceWithAudit(
			&recordingMediaRepository{item: item},
			&recordingGrantFinder{},
			appender,
		),
		&recordingLocationFinder{location: location},
		&recordingManagedDeletionRepository{},
		&recordingDeletionStore{deletion: deletion},
	)

	err = service.DeleteItem(
		context.Background(),
		serviceUser(item.OwnerID, identity.RoleViewer, true),
		item.ID,
	)
	if !errors.Is(err, commitError) {
		t.Fatalf("expected finalization error, got %v", err)
	}
	if len(appender.events) != 1 ||
		appender.events[0].Outcome != audit.OutcomeFailure ||
		appender.events[0].Reason != audit.ReasonOperationFailure {
		t.Fatalf("unexpected finalization failure event: %+v", appender.events)
	}
}

func TestManagedServiceDeletesMetadataOnlyItem(t *testing.T) {
	item := serviceItem(t)
	repository := &recordingMediaRepository{item: item}
	service := appmedia.NewManagedService(
		newMetadataService(repository, &recordingGrantFinder{}),
		&recordingLocationFinder{err: content.ErrLocationNotFound},
		&recordingManagedDeletionRepository{},
		&recordingDeletionStore{},
	)

	if err := service.DeleteItem(
		context.Background(),
		serviceUser(item.OwnerID, identity.RoleViewer, true),
		item.ID,
	); err != nil {
		t.Fatalf("delete metadata-only item: %v", err)
	}
	if repository.deletedID != item.ID {
		t.Fatalf("expected metadata deletion for %q, got %q", item.ID, repository.deletedID)
	}
}

func TestManagedServiceAuthorizesBeforeContentLookup(t *testing.T) {
	item := serviceItem(t)
	finder := &recordingLocationFinder{}
	service := appmedia.NewManagedService(
		newMetadataService(
			&recordingMediaRepository{item: item},
			&recordingGrantFinder{err: errors.New("grant failure")},
		),
		finder,
		&recordingManagedDeletionRepository{},
		&recordingDeletionStore{},
	)

	err := service.DeleteItem(
		context.Background(),
		serviceUser(serviceActorID, identity.RoleViewer, true),
		item.ID,
	)
	if err == nil || finder.calls != 0 {
		t.Fatalf("expected authorization failure before location lookup, got %v", err)
	}
}
