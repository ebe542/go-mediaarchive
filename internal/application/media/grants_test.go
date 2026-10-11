package media_test

import (
	"context"
	"errors"
	"testing"
	"time"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/audit"
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
	savedEvent   audit.Event
	saveError    error
	deletedMedia string
	deletedUser  string
	deletedEvent audit.Event
	deleteError  error
}

func (repository *recordingGrantRepository) Find(
	_ context.Context,
	mediaID string,
	userID string,
) (domainmedia.Grant, error) {
	repository.findCalls = append(repository.findCalls, mediaID+":"+userID)
	if repository.findError != nil {
		return domainmedia.Grant{}, repository.findError
	}
	grant, found := repository.grants[userID]
	if !found {
		return domainmedia.Grant{}, domainmedia.ErrGrantNotFound
	}

	return grant, nil
}

func (repository *recordingGrantRepository) Save(
	_ context.Context,
	grant domainmedia.Grant,
) error {
	repository.saved = grant

	return repository.saveError
}

func (repository *recordingGrantRepository) SaveWithAudit(
	_ context.Context,
	grant domainmedia.Grant,
	event audit.Event,
) error {
	repository.saved = grant
	repository.savedEvent = event

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
	mediaID string,
	userID string,
) error {
	repository.deletedMedia = mediaID
	repository.deletedUser = userID

	return repository.deleteError
}

func (repository *recordingGrantRepository) DeleteWithAudit(
	_ context.Context,
	mediaID string,
	userID string,
	event audit.Event,
) error {
	repository.deletedMedia = mediaID
	repository.deletedUser = userID
	repository.deletedEvent = event

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
	service := newGrantService(repository, grants, users, &recordingUploadAuditAppender{})
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
	if grants.savedEvent.Type != audit.TypeMediaGrantReplaced ||
		grants.savedEvent.Outcome != audit.OutcomeSuccess ||
		grants.savedEvent.TargetID != item.ID+":"+serviceGranteeID {
		t.Fatalf("unexpected grant replacement event: %+v", grants.savedEvent)
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
	if grants.deletedEvent.Type != audit.TypeMediaGrantRevoked ||
		grants.deletedEvent.Outcome != audit.OutcomeSuccess ||
		grants.deletedEvent.TargetID != item.ID+":"+serviceGranteeID {
		t.Fatalf("unexpected grant revocation event: %+v", grants.deletedEvent)
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
	service := newGrantService(
		&recordingMediaRepository{item: item}, grants, users,
		&recordingUploadAuditAppender{},
	)

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
	service := newGrantService(
		&recordingMediaRepository{item: item}, grants, users,
		&recordingUploadAuditAppender{},
	)
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
			appender := &recordingUploadAuditAppender{}
			service := newGrantService(
				&recordingMediaRepository{item: item}, grants, users, appender,
			)

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
			if len(appender.events) != 1 ||
				appender.events[0].Outcome != audit.OutcomeDenied {
				t.Fatalf("unexpected denied grant event: %+v", appender.events)
			}
		})
	}
}

func TestRevokeGrantPreservesRepositoryNotFound(t *testing.T) {
	item := serviceItem(t)
	grants := &recordingGrantRepository{deleteError: domainmedia.ErrGrantNotFound}
	appender := &recordingUploadAuditAppender{}
	service := newGrantService(
		&recordingMediaRepository{item: item},
		grants,
		&recordingUserFinder{},
		appender,
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
	if len(appender.events) != 1 ||
		appender.events[0].Type != audit.TypeMediaGrantRevoked ||
		appender.events[0].Outcome != audit.OutcomeDenied ||
		appender.events[0].Reason != audit.ReasonUnknownTarget {
		t.Fatalf("unexpected missing grant event: %+v", appender.events)
	}
}

func TestGrantServiceFailsClosedWhenDenialCannotBeAudited(t *testing.T) {
	auditError := errors.New("synthetic audit failure")
	item := serviceItem(t)
	service := newGrantService(
		&recordingMediaRepository{item: item},
		&recordingGrantRepository{},
		&recordingUserFinder{},
		&recordingUploadAuditAppender{err: auditError},
	)

	_, err := service.ReplaceGrant(
		context.Background(),
		serviceUser(item.OwnerID, identity.RoleViewer, true),
		item.ID,
		item.OwnerID,
		servicePermissionSet(t, domainmedia.PermissionRead),
	)
	if !errors.Is(err, auditError) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	if errors.Is(err, appmedia.ErrOwnerGrant) {
		t.Fatalf("expected audit failure to hide the denial, got %v", err)
	}
}

func newGrantService(
	mediaFinder appmedia.MediaFinder,
	grants appmedia.GrantRepository,
	users appmedia.UserFinder,
	appender audit.Appender,
) *appmedia.GrantService {
	return appmedia.NewGrantService(
		mediaFinder,
		grants,
		users,
		appender,
		func() string { return "b23e4567-e89b-12d3-a456-426614174000" },
		func() time.Time { return serviceTime().Add(time.Hour) },
	)
}
