package media_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const (
	serviceMediaID = "123e4567-e89b-12d3-a456-426614174000"
	serviceOwnerID = "223e4567-e89b-12d3-a456-426614174000"
	serviceActorID = "323e4567-e89b-12d3-a456-426614174000"
)

type recordingMediaRepository struct {
	item        domainmedia.Item
	findError   error
	createError error
	updateError error
	deleteError error
	created     domainmedia.Item
	updated     domainmedia.Item
	deletedID   string
}

func (repository *recordingMediaRepository) Create(_ context.Context, argItem domainmedia.Item) error {
	repository.created = argItem

	return repository.createError
}

func (repository *recordingMediaRepository) FindByID(_ context.Context, _ string) (domainmedia.Item, error) {
	return repository.item, repository.findError
}

func (repository *recordingMediaRepository) Update(_ context.Context, argItem domainmedia.Item) error {
	repository.updated = argItem

	return repository.updateError
}

func (repository *recordingMediaRepository) Delete(_ context.Context, argID string) error {
	repository.deletedID = argID

	return repository.deleteError
}

type recordingGrantFinder struct {
	grant   domainmedia.Grant
	err     error
	calls   int
	mediaID string
	userID  string
}

func (finder *recordingGrantFinder) Find(
	_ context.Context,
	argMediaID string,
	argUserID string,
) (domainmedia.Grant, error) {
	finder.calls++
	finder.mediaID = argMediaID
	finder.userID = argUserID

	return finder.grant, finder.err
}

func TestCreateItemAssignsActiveEditorOrAdministratorAsOwner(t *testing.T) {
	for _, role := range []identity.Role{identity.RoleEditor, identity.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			repository := &recordingMediaRepository{}
			clockCalls := 0
			now := serviceTime()
			service := appmedia.NewService(
				repository,
				&recordingGrantFinder{},
				func() string { return serviceMediaID },
				func() time.Time { clockCalls++; return now },
			)
			actor := serviceUser(serviceActorID, role, true)

			item, err := service.CreateItem(context.Background(), actor, serviceInput())
			if err != nil {
				t.Fatalf("create media: %v", err)
			}
			if item.OwnerID != actor.ID || repository.created.OwnerID != actor.ID {
				t.Fatalf("expected actor %q as owner, got %+v", actor.ID, item)
			}
			if item.ID != serviceMediaID || clockCalls != 1 ||
				!item.CreatedAt.Equal(now) || !item.UpdatedAt.Equal(now) {
				t.Errorf("expected service-controlled lifecycle values, got %+v", item)
			}
		})
	}
}

func TestCreateItemRejectsUnauthorizedActors(t *testing.T) {
	for name, actor := range map[string]identity.User{
		"viewer":   serviceUser(serviceActorID, identity.RoleViewer, true),
		"inactive": serviceUser(serviceActorID, identity.RoleEditor, false),
		"bad role": serviceUser(serviceActorID, identity.Role("owner"), true),
	} {
		t.Run(name, func(t *testing.T) {
			repository := &recordingMediaRepository{}
			service := newMetadataService(repository, &recordingGrantFinder{})
			_, err := service.CreateItem(context.Background(), actor, serviceInput())
			if !errors.Is(err, appmedia.ErrCreationForbidden) {
				t.Fatalf("expected ErrCreationForbidden, got %v", err)
			}
			if repository.created.ID != "" {
				t.Fatal("expected forbidden creation not to reach repository")
			}
		})
	}
}

func TestOwnerReadsUpdatesAndDeletesWithoutGrantLookup(t *testing.T) {
	original := serviceItem(t)
	repository := &recordingMediaRepository{item: original}
	grants := &recordingGrantFinder{err: domainmedia.ErrGrantNotFound}
	service := newMetadataService(repository, grants)
	owner := serviceUser(original.OwnerID, identity.RoleViewer, true)

	if _, err := service.ItemByID(context.Background(), owner, original.ID); err != nil {
		t.Fatalf("read owned media: %v", err)
	}
	updated, err := service.UpdateItem(context.Background(), owner, original.ID, updatedServiceInput())
	if err != nil {
		t.Fatalf("update owned media: %v", err)
	}
	if updated.OwnerID != original.OwnerID || !updated.CreatedAt.Equal(original.CreatedAt) {
		t.Fatalf("expected owner and creation time to be preserved, got %+v", updated)
	}
	if err := service.DeleteItem(context.Background(), owner, original.ID); err != nil {
		t.Fatalf("delete owned media: %v", err)
	}
	if grants.calls != 0 {
		t.Fatalf("expected no owner grant lookup, got %d", grants.calls)
	}
	if repository.deletedID != original.ID {
		t.Fatalf("expected deleted ID %q, got %q", original.ID, repository.deletedID)
	}
}

func TestGranteeUsesOneExactGrantPerAuthorization(t *testing.T) {
	item := serviceItem(t)
	permissions := servicePermissionSet(
		t,
		domainmedia.PermissionDiscover,
		domainmedia.PermissionUpdate,
		domainmedia.PermissionDelete,
	)
	grant, err := domainmedia.NewGrant(item.ID, serviceActorID, permissions)
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	repository := &recordingMediaRepository{item: item}
	grants := &recordingGrantFinder{grant: grant}
	service := newMetadataService(repository, grants)
	actor := serviceUser(serviceActorID, identity.RoleViewer, true)

	if _, err := service.ItemByID(context.Background(), actor, item.ID); err != nil {
		t.Fatalf("discover granted media: %v", err)
	}
	if _, err := service.UpdateItem(context.Background(), actor, item.ID, updatedServiceInput()); err != nil {
		t.Fatalf("update granted media: %v", err)
	}
	if err := service.DeleteItem(context.Background(), actor, item.ID); err != nil {
		t.Fatalf("delete granted media: %v", err)
	}
	if grants.calls != 3 || grants.mediaID != item.ID || grants.userID != actor.ID {
		t.Fatalf("expected one actor grant lookup per decision, got %+v", grants)
	}
}

func TestUnknownAndUnauthorizedMediaUseSameError(t *testing.T) {
	item := serviceItem(t)
	actor := serviceUser(serviceActorID, identity.RoleAdmin, true)

	for name, repository := range map[string]*recordingMediaRepository{
		"unknown":      {findError: domainmedia.ErrItemNotFound},
		"unauthorized": {item: item},
	} {
		t.Run(name, func(t *testing.T) {
			grants := &recordingGrantFinder{err: domainmedia.ErrGrantNotFound}
			service := newMetadataService(repository, grants)
			_, err := service.ItemByID(context.Background(), actor, item.ID)
			if !errors.Is(err, appmedia.ErrMediaNotFound) {
				t.Fatalf("expected ErrMediaNotFound, got %v", err)
			}
		})
	}
}

func TestReadPermissionDoesNotRevealMetadata(t *testing.T) {
	item := serviceItem(t)
	grant, err := domainmedia.NewGrant(
		item.ID,
		serviceActorID,
		servicePermissionSet(t, domainmedia.PermissionRead),
	)
	if err != nil {
		t.Fatalf("create read grant: %v", err)
	}
	repository := &recordingMediaRepository{item: item}
	service := newMetadataService(repository, &recordingGrantFinder{grant: grant})
	_, err = service.ItemByID(
		context.Background(),
		serviceUser(serviceActorID, identity.RoleViewer, true),
		item.ID,
	)
	if !errors.Is(err, appmedia.ErrMediaNotFound) {
		t.Fatalf("expected undiscoverable media to be masked, got %v", err)
	}
}

func TestUnauthorizedUpdateAndDeleteDoNotReachRepository(t *testing.T) {
	item := serviceItem(t)
	grant, err := domainmedia.NewGrant(
		item.ID,
		serviceActorID,
		servicePermissionSet(t, domainmedia.PermissionDiscover),
	)
	if err != nil {
		t.Fatalf("create discover grant: %v", err)
	}
	repository := &recordingMediaRepository{item: item}
	service := newMetadataService(repository, &recordingGrantFinder{grant: grant})
	actor := serviceUser(serviceActorID, identity.RoleEditor, true)

	if _, err := service.UpdateItem(
		context.Background(),
		actor,
		item.ID,
		updatedServiceInput(),
	); !errors.Is(err, appmedia.ErrMediaNotFound) {
		t.Fatalf("expected unauthorized update to be masked, got %v", err)
	}
	if err := service.DeleteItem(
		context.Background(),
		actor,
		item.ID,
	); !errors.Is(err, appmedia.ErrMediaNotFound) {
		t.Fatalf("expected unauthorized deletion to be masked, got %v", err)
	}
	if repository.updated.ID != "" || repository.deletedID != "" {
		t.Fatal("expected unauthorized mutations not to reach repository writes")
	}
}

func newMetadataService(
	argRepository *recordingMediaRepository,
	argGrants *recordingGrantFinder,
) *appmedia.Service {
	return appmedia.NewService(
		argRepository,
		argGrants,
		func() string { return serviceMediaID },
		func() time.Time { return serviceTime().Add(time.Hour) },
	)
}

func serviceInput() appmedia.CreateItemInput {
	return appmedia.CreateItemInput{
		Title:            "Security Book",
		Authors:          []string{"Archive Author"},
		OriginalFilename: "security-book.pdf",
		Type:             domainmedia.TypeBook,
		MIMEType:         "application/pdf",
		Size:             4096,
		Checksum:         bytes.Repeat([]byte{0x5a}, sha256.Size),
	}
}

func updatedServiceInput() appmedia.UpdateItemInput {
	input := serviceInput()
	input.Title = "Updated Security Book"
	input.Authors = []string{"Updated Author"}

	return input
}

func serviceItem(argTest *testing.T) domainmedia.Item {
	argTest.Helper()

	item, err := domainmedia.NewItem(
		serviceMediaID,
		serviceInput().Title,
		serviceInput().Authors,
		serviceInput().OriginalFilename,
		serviceInput().Type,
		serviceInput().MIMEType,
		serviceInput().Size,
		serviceInput().Checksum,
		serviceOwnerID,
		serviceTime(),
		serviceTime(),
	)
	if err != nil {
		argTest.Fatalf("create media fixture: %v", err)
	}

	return item
}

func serviceUser(argID string, argRole identity.Role, argActive bool) identity.User {
	return identity.User{ID: argID, Username: "service_user", Role: argRole, Active: argActive}
}

func servicePermissionSet(
	argTest *testing.T,
	argPermissions ...domainmedia.Permission,
) domainmedia.PermissionSet {
	argTest.Helper()

	permissions, err := domainmedia.NewPermissionSet(argPermissions...)
	if err != nil {
		argTest.Fatalf("create permission set: %v", err)
	}

	return permissions
}

func serviceTime() time.Time {
	return time.Date(2026, time.September, 11, 10, 0, 0, 0, time.UTC)
}
