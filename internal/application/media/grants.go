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
	media MediaFinder,
	grants GrantRepository,
	users UserFinder,
) *GrantService {
	return &GrantService{media: media, grants: grants, users: users}
}

// ReplaceGrant replaces one active user's complete permission set.
func (service *GrantService) ReplaceGrant(
	ctx context.Context,
	actor identity.User,
	mediaID string,
	userID string,
	permissions domainmedia.PermissionSet,
) (domainmedia.Grant, error) {
	item, err := service.sharedItem(ctx, actor, mediaID)
	if err != nil {
		return domainmedia.Grant{}, err
	}
	if userID == item.OwnerID {
		return domainmedia.Grant{}, ErrOwnerGrant
	}

	grant, err := domainmedia.NewGrant(mediaID, userID, permissions)
	if err != nil {
		return domainmedia.Grant{}, fmt.Errorf("create media grant: %w", err)
	}

	user, err := service.users.FindByID(ctx, userID)
	if err != nil {
		return domainmedia.Grant{}, fmt.Errorf("retrieve media grantee: %w", err)
	}
	if !user.Active {
		return domainmedia.Grant{}, ErrInactiveGrantee
	}
	if err := service.grants.Save(ctx, grant); err != nil {
		return domainmedia.Grant{}, fmt.Errorf("persist media grant: %w", err)
	}

	return grant, nil
}

// GrantByUser returns one grant after authorizing its inspection.
func (service *GrantService) GrantByUser(
	ctx context.Context,
	actor identity.User,
	mediaID string,
	userID string,
) (domainmedia.Grant, error) {
	if _, err := service.sharedItem(ctx, actor, mediaID); err != nil {
		return domainmedia.Grant{}, err
	}

	grant, err := service.grants.Find(ctx, mediaID, userID)
	if err != nil {
		return domainmedia.Grant{}, fmt.Errorf("retrieve media grant: %w", err)
	}

	return grant, nil
}

// GrantsByMedia lists all grants after authorizing their inspection.
func (service *GrantService) GrantsByMedia(
	ctx context.Context,
	actor identity.User,
	mediaID string,
) ([]domainmedia.Grant, error) {
	if _, err := service.sharedItem(ctx, actor, mediaID); err != nil {
		return nil, err
	}

	grants, err := service.grants.ListByMedia(ctx, mediaID)
	if err != nil {
		return nil, fmt.Errorf("list media grants: %w", err)
	}

	return grants, nil
}

// RevokeGrant explicitly deletes one stored grant.
func (service *GrantService) RevokeGrant(
	ctx context.Context,
	actor identity.User,
	mediaID string,
	userID string,
) error {
	item, err := service.sharedItem(ctx, actor, mediaID)
	if err != nil {
		return err
	}
	if userID == item.OwnerID {
		return ErrOwnerGrant
	}
	if err := service.grants.Delete(ctx, mediaID, userID); err != nil {
		return fmt.Errorf("delete media grant: %w", err)
	}

	return nil
}

func (service *GrantService) sharedItem(
	ctx context.Context,
	actor identity.User,
	mediaID string,
) (domainmedia.Item, error) {
	return authorizeItem(
		ctx,
		actor,
		mediaID,
		domainmedia.PermissionShare,
		service.media,
		service.grants,
	)
}
