package media_test

import (
	"context"
	"errors"
	"testing"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const serviceGranteeID = "423e4567-e89b-12d3-a456-426614174000"

type recordingGrantRepository struct {
	grants       map[string]domainmedia.Grant
	findCalls    []string
	findError    error
	listCalls    int
	listError    error
	saved        domainmedia.Grant
	saveError    error
	deletedMedia string
	deletedUser  string
	deleteError  error
}

func (repository *recordingGrantRepository) Find(
	_ context.Context,
	argMediaID string,
	argUserID string,
) (domainmedia.Grant, error) {
	repository.findCalls = append(repository.findCalls, argMediaID+":"+argUserID)
	if repository.findError != nil {
		return domainmedia.Grant{}, repository.findError
	}
	grant, found := repository.grants[argUserID]
	if !found {
		return domainmedia.Grant{}, domainmedia.ErrGrantNotFound
	}

	return grant, nil
}

func (repository *recordingGrantRepository) Save(
	_ context.Context,
	argGrant domainmedia.Grant,
) error {
	repository.saved = argGrant

	return repository.saveError
}

func (repository *recordingGrantRepository) ListByMedia(
	_ context.Context,
	_ string,
) ([]domainmedia.Grant, error) {
	repository.listCalls++
	if repository.listError != nil {
		return nil, repository.listError
	}

	grants := make([]domainmedia.Grant, 0, len(repository.grants))
	for _, grant := range repository.grants {
		grants = append(grants, grant)
	}

	return grants, nil
}

func (repository *recordingGrantRepository) Delete(
	_ context.Context,
	argMediaID string,
	argUserID string,
) error {
	repository.deletedMedia = argMediaID
	repository.deletedUser = argUserID

	return repository.deleteError
}

type recordingUserFinder struct {
	user  identity.User
	err   error
	calls int
}

func (finder *recordingUserFinder) FindByID(
	_ context.Context,
	_ string,
) (identity.User, error) {
	finder.calls++

	return finder.user, finder.err
}

func TestOwnerReplacesInspectsListsAndRevokesGrant(t *testing.T) {
	item := serviceItem(t)
	permissions := servicePermissionSet(t, domainmedia.PermissionDiscover, domainmedia.PermissionRead)
	existing, err := domainmedia.NewGrant(item.ID, serviceGranteeID, permissions)
	if err != nil {
		t.Fatalf("create grant fixture: %v", err)
	}
	repository := &recordingMediaRepository{item: item}
	grants := &recordingGrantRepository{grants: map[string]domainmedia.Grant{serviceGranteeID: existing}}
	users := &recordingUserFinder{user: serviceUser(serviceGranteeID, identity.RoleViewer, true)}
	service := appmedia.NewGrantService(repository, grants, users)
	owner := serviceUser(item.OwnerID, identity.RoleViewer, true)

	replaced, err := service.ReplaceGrant(
		context.Background(),
		owner,
		item.ID,
		serviceGranteeID,
		permissions,
	)
	if err != nil {
		t.Fatalf("replace grant: %v", err)
	}
	if replaced != existing || grants.saved != existing {
		t.Fatalf("expected complete replacement grant %+v, got %+v", existing, replaced)
	}
	if _, err := service.GrantByUser(context.Background(), owner, item.ID, serviceGranteeID); err != nil {
		t.Fatalf("inspect grant: %v", err)
	}
	if _, err := service.GrantsByMedia(context.Background(), owner, item.ID); err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if err := service.RevokeGrant(context.Background(), owner, item.ID, serviceGranteeID); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	if grants.deletedMedia != item.ID || grants.deletedUser != serviceGranteeID {
		t.Fatalf("expected explicit grant deletion, got %+v", grants)
	}
	// The sole Find call inspects the requested grant; owner authorization performs no lookup.
	if len(grants.findCalls) != 1 || grants.listCalls != 1 {
		t.Fatalf("expected no owner authorization lookup, got %+v", grants)
	}
}

func TestExplicitSharePermissionAuthorizesGrantReplacement(t *testing.T) {
	item := serviceItem(t)
	shareGrant, err := domainmedia.NewGrant(
		item.ID,
		serviceActorID,
		servicePermissionSet(t, domainmedia.PermissionShare),
	)
	if err != nil {
		t.Fatalf("create share grant: %v", err)
	}
	permissions := servicePermissionSet(t, domainmedia.PermissionDownload)
	grants := &recordingGrantRepository{grants: map[string]domainmedia.Grant{serviceActorID: shareGrant}}
	users := &recordingUserFinder{user: serviceUser(serviceGranteeID, identity.RoleViewer, true)}
	service := appmedia.NewGrantService(&recordingMediaRepository{item: item}, grants, users)

	if _, err := service.ReplaceGrant(
		context.Background(),
		serviceUser(serviceActorID, identity.RoleViewer, true),
		item.ID,
		serviceGranteeID,
		permissions,
	); err != nil {
		t.Fatalf("replace grant with share permission: %v", err)
	}
	if len(grants.findCalls) != 1 || grants.findCalls[0] != item.ID+":"+serviceActorID {
		t.Fatalf("expected exactly one actor-specific authorization lookup, got %v", grants.findCalls)
	}
}

func TestGrantOperationsRequireShareAndMaskMedia(t *testing.T) {
	item := serviceItem(t)
	discoverGrant, err := domainmedia.NewGrant(
		item.ID,
		serviceActorID,
		servicePermissionSet(t, domainmedia.PermissionDiscover),
	)
	if err != nil {
		t.Fatalf("create discover grant: %v", err)
	}
	grants := &recordingGrantRepository{grants: map[string]domainmedia.Grant{serviceActorID: discoverGrant}}
	users := &recordingUserFinder{user: serviceUser(serviceGranteeID, identity.RoleViewer, true)}
	service := appmedia.NewGrantService(&recordingMediaRepository{item: item}, grants, users)
	actor := serviceUser(serviceActorID, identity.RoleAdmin, true)

	_, replaceErr := service.ReplaceGrant(
		context.Background(), actor, item.ID, serviceGranteeID,
		servicePermissionSet(t, domainmedia.PermissionRead),
	)
	_, inspectErr := service.GrantByUser(context.Background(), actor, item.ID, serviceGranteeID)
	_, listErr := service.GrantsByMedia(context.Background(), actor, item.ID)
	revokeErr := service.RevokeGrant(context.Background(), actor, item.ID, serviceGranteeID)
	for name, operationErr := range map[string]error{
		"replace": replaceErr,
		"inspect": inspectErr,
		"list":    listErr,
		"revoke":  revokeErr,
	} {
		if !errors.Is(operationErr, appmedia.ErrMediaNotFound) {
			t.Errorf("%s: expected masked ErrMediaNotFound, got %v", name, operationErr)
		}
	}
	if grants.saved.MediaID != "" || grants.listCalls != 0 || grants.deletedMedia != "" || users.calls != 0 {
		t.Fatalf("expected unauthorized operations to have no downstream effects, got %+v", grants)
	}
}

func TestReplaceGrantRejectsInvalidRecipientsAndPermissions(t *testing.T) {
	item := serviceItem(t)
	owner := serviceUser(item.OwnerID, identity.RoleViewer, true)
	valid := servicePermissionSet(t, domainmedia.PermissionRead)

	for _, testCase := range []struct {
		name        string
		userID      string
		permissions domainmedia.PermissionSet
		user        identity.User
		userError   error
		expected    error
	}{
		{"owner", item.OwnerID, valid, identity.User{}, nil, appmedia.ErrOwnerGrant},
		{"empty permissions", serviceGranteeID, 0, identity.User{}, nil, domainmedia.ErrInvalidPermissionSet},
		{"inactive", serviceGranteeID, valid, serviceUser(serviceGranteeID, identity.RoleViewer, false), nil, appmedia.ErrInactiveGrantee},
		{"unknown", serviceGranteeID, valid, identity.User{}, identity.ErrUserNotFound, identity.ErrUserNotFound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			grants := &recordingGrantRepository{}
			users := &recordingUserFinder{user: testCase.user, err: testCase.userError}
			service := appmedia.NewGrantService(&recordingMediaRepository{item: item}, grants, users)

			_, err := service.ReplaceGrant(
				context.Background(),
				owner,
				item.ID,
				testCase.userID,
				testCase.permissions,
			)
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("expected %v, got %v", testCase.expected, err)
			}
			if grants.saved.MediaID != "" {
				t.Fatal("expected rejected grant not to be persisted")
			}
		})
	}
}

func TestRevokeGrantPreservesRepositoryNotFound(t *testing.T) {
	item := serviceItem(t)
	grants := &recordingGrantRepository{deleteError: domainmedia.ErrGrantNotFound}
	service := appmedia.NewGrantService(
		&recordingMediaRepository{item: item},
		grants,
		&recordingUserFinder{},
	)

	err := service.RevokeGrant(
		context.Background(),
		serviceUser(item.OwnerID, identity.RoleViewer, true),
		item.ID,
		serviceGranteeID,
	)
	if !errors.Is(err, domainmedia.ErrGrantNotFound) {
		t.Fatalf("expected ErrGrantNotFound, got %v", err)
	}
}
