package media

import (
	"context"
	"errors"
	"fmt"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

var (
	// ErrOwnerGrant rejects redundant permissions for a media owner.
	ErrOwnerGrant = errors.New("media owner cannot receive an explicit grant")
	// ErrInactiveGrantee rejects grants to an inactive user identity.
	ErrInactiveGrantee = errors.New("media grantee is inactive")
)

// GrantRepository persists complete per-user media permission sets.
type GrantRepository interface {
	GrantFinder
	Save(context.Context, domainmedia.Grant) error
	ListByMedia(context.Context, string) ([]domainmedia.Grant, error)
	Delete(context.Context, string, string) error
}

// UserFinder resolves grant recipients without exposing broader user operations.
type UserFinder interface {
	FindByID(context.Context, string) (identity.User, error)
}

// GrantService coordinates authorized media grant operations.
type GrantService struct {
	media  MediaFinder
	grants GrantRepository
	users  UserFinder
}

// NewGrantService creates a storage-independent media grant service.
func NewGrantService(
	argMedia MediaFinder,
	argGrants GrantRepository,
	argUsers UserFinder,
) *GrantService {
	return &GrantService{media: argMedia, grants: argGrants, users: argUsers}
}

// ReplaceGrant replaces one active user's complete permission set.
func (service *GrantService) ReplaceGrant(
	argContext context.Context,
	argActor identity.User,
	argMediaID string,
	argUserID string,
	argPermissions domainmedia.PermissionSet,
) (domainmedia.Grant, error) {
	item, err := service.sharedItem(argContext, argActor, argMediaID)
	if err != nil {
		return domainmedia.Grant{}, err
	}
	if argUserID == item.OwnerID {
		return domainmedia.Grant{}, ErrOwnerGrant
	}

	grant, err := domainmedia.NewGrant(argMediaID, argUserID, argPermissions)
	if err != nil {
		return domainmedia.Grant{}, fmt.Errorf("create media grant: %w", err)
	}

	user, err := service.users.FindByID(argContext, argUserID)
	if err != nil {
		return domainmedia.Grant{}, fmt.Errorf("retrieve media grantee: %w", err)
	}
	if !user.Active {
		return domainmedia.Grant{}, ErrInactiveGrantee
	}
	if err := service.grants.Save(argContext, grant); err != nil {
		return domainmedia.Grant{}, fmt.Errorf("persist media grant: %w", err)
	}

	return grant, nil
}

// GrantByUser returns one grant after authorizing its inspection.
func (service *GrantService) GrantByUser(
	argContext context.Context,
	argActor identity.User,
	argMediaID string,
	argUserID string,
) (domainmedia.Grant, error) {
	if _, err := service.sharedItem(argContext, argActor, argMediaID); err != nil {
		return domainmedia.Grant{}, err
	}

	grant, err := service.grants.Find(argContext, argMediaID, argUserID)
	if err != nil {
		return domainmedia.Grant{}, fmt.Errorf("retrieve media grant: %w", err)
	}

	return grant, nil
}

// GrantsByMedia lists all grants after authorizing their inspection.
func (service *GrantService) GrantsByMedia(
	argContext context.Context,
	argActor identity.User,
	argMediaID string,
) ([]domainmedia.Grant, error) {
	if _, err := service.sharedItem(argContext, argActor, argMediaID); err != nil {
		return nil, err
	}

	grants, err := service.grants.ListByMedia(argContext, argMediaID)
	if err != nil {
		return nil, fmt.Errorf("list media grants: %w", err)
	}

	return grants, nil
}

// RevokeGrant explicitly deletes one stored grant.
func (service *GrantService) RevokeGrant(
	argContext context.Context,
	argActor identity.User,
	argMediaID string,
	argUserID string,
) error {
	item, err := service.sharedItem(argContext, argActor, argMediaID)
	if err != nil {
		return err
	}
	if argUserID == item.OwnerID {
		return ErrOwnerGrant
	}
	if err := service.grants.Delete(argContext, argMediaID, argUserID); err != nil {
		return fmt.Errorf("delete media grant: %w", err)
	}

	return nil
}

func (service *GrantService) sharedItem(
	argContext context.Context,
	argActor identity.User,
	argMediaID string,
) (domainmedia.Item, error) {
	return authorizeItem(
		argContext,
		argActor,
		argMediaID,
		domainmedia.PermissionShare,
		service.media,
		service.grants,
	)
}
