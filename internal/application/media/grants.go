package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
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
	SaveWithAudit(context.Context, domainmedia.Grant, audit.Event) error
	ListByMedia(context.Context, string) ([]domainmedia.Grant, error)
	DeleteWithAudit(context.Context, string, string, audit.Event) error
}

// UserFinder resolves grant recipients without exposing broader user operations.
type UserFinder interface {
	FindByID(context.Context, string) (identity.User, error)
}

// GrantService coordinates authorized media grant operations.
type GrantService struct {
	media    MediaFinder
	grants   GrantRepository
	users    UserFinder
	audit    audit.Appender
	eventIDs AuditEventIDGenerator
	clock    Clock
}

// NewGrantService creates a storage-independent media grant service.
func NewGrantService(
	media MediaFinder,
	grants GrantRepository,
	users UserFinder,
	auditAppender audit.Appender,
	eventIDGenerator AuditEventIDGenerator,
	clock Clock,
) *GrantService {
	return &GrantService{
		media:    media,
		grants:   grants,
		users:    users,
		audit:    auditAppender,
		eventIDs: eventIDGenerator,
		clock:    clock,
	}
}

// ReplaceGrant replaces one active user's complete permission set.
func (service *GrantService) ReplaceGrant(
	ctx context.Context,
	actor identity.User,
	mediaID string,
	userID string,
	permissions domainmedia.PermissionSet,
) (domainmedia.Grant, error) {
	now := service.clock()
	item, err := service.sharedItem(ctx, actor, mediaID)
	if err != nil {
		if !errors.Is(err, ErrMediaNotFound) {
			return domainmedia.Grant{}, err
		}

		return domainmedia.Grant{}, service.recordGrantEvent(
			ctx, actor, audit.TypeMediaGrantReplaced, "",
			audit.OutcomeDenied, audit.ReasonUnknownTarget, now, err,
		)
	}
	if userID == item.OwnerID {
		return domainmedia.Grant{}, service.recordGrantEvent(
			ctx, actor, audit.TypeMediaGrantReplaced,
			grantTargetID(mediaID, userID), audit.OutcomeDenied,
			audit.ReasonResourceConflict, now, ErrOwnerGrant,
		)
	}

	grant, err := domainmedia.NewGrant(mediaID, userID, permissions)
	if err != nil {
		operationErr := fmt.Errorf("create media grant: %w", err)

		return domainmedia.Grant{}, service.recordGrantEvent(
			ctx, actor, audit.TypeMediaGrantReplaced, "",
			audit.OutcomeDenied, audit.ReasonInvalidInput, now, operationErr,
		)
	}

	user, err := service.users.FindByID(ctx, userID)
	if err != nil {
		operationErr := fmt.Errorf("retrieve media grantee: %w", err)
		if errors.Is(err, identity.ErrUserNotFound) {
			return domainmedia.Grant{}, service.recordGrantEvent(
				ctx, actor, audit.TypeMediaGrantReplaced,
				grantTargetID(mediaID, userID), audit.OutcomeDenied,
				audit.ReasonUnknownTarget, now, operationErr,
			)
		}

		return domainmedia.Grant{}, operationErr
	}
	if !user.Active {
		return domainmedia.Grant{}, service.recordGrantEvent(
			ctx, actor, audit.TypeMediaGrantReplaced,
			grantTargetID(mediaID, userID), audit.OutcomeDenied,
			audit.ReasonInactiveUser, now, ErrInactiveGrantee,
		)
	}
	event, err := service.newGrantEvent(
		actor, audit.TypeMediaGrantReplaced, grantTargetID(mediaID, userID),
		audit.OutcomeSuccess, "", now,
	)
	if err != nil {
		return domainmedia.Grant{}, err
	}
	if err := service.grants.SaveWithAudit(ctx, grant, event); err != nil {
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
	now := service.clock()
	item, err := service.sharedItem(ctx, actor, mediaID)
	if err != nil {
		if !errors.Is(err, ErrMediaNotFound) {
			return err
		}

		return service.recordGrantEvent(
			ctx, actor, audit.TypeMediaGrantRevoked, "",
			audit.OutcomeDenied, audit.ReasonUnknownTarget, now, err,
		)
	}
	if userID == item.OwnerID {
		return service.recordGrantEvent(
			ctx, actor, audit.TypeMediaGrantRevoked,
			grantTargetID(mediaID, userID), audit.OutcomeDenied,
			audit.ReasonResourceConflict, now, ErrOwnerGrant,
		)
	}
	if err := identity.ValidateUserID(userID); err != nil {
		operationErr := fmt.Errorf("validate revoked grant user: %w", err)

		return service.recordGrantEvent(
			ctx, actor, audit.TypeMediaGrantRevoked, "",
			audit.OutcomeDenied, audit.ReasonInvalidInput, now, operationErr,
		)
	}
	targetID := grantTargetID(mediaID, userID)
	event, err := service.newGrantEvent(
		actor, audit.TypeMediaGrantRevoked, targetID,
		audit.OutcomeSuccess, "", now,
	)
	if err != nil {
		return err
	}
	if err := service.grants.DeleteWithAudit(ctx, mediaID, userID, event); err != nil {
		if errors.Is(err, domainmedia.ErrGrantNotFound) {
			return service.recordGrantEvent(
				ctx, actor, audit.TypeMediaGrantRevoked, targetID,
				audit.OutcomeDenied, audit.ReasonUnknownTarget, now, err,
			)
		}

		return fmt.Errorf("delete media grant: %w", err)
	}

	return nil
}

func (service *GrantService) recordGrantEvent(
	ctx context.Context,
	actor identity.User,
	eventType audit.Type,
	targetID string,
	outcome audit.Outcome,
	reason audit.Reason,
	now time.Time,
	operationErr error,
) error {
	event, err := service.newGrantEvent(actor, eventType, targetID, outcome, reason, now)
	if err != nil {
		return err
	}
	if err := service.audit.Append(ctx, event); err != nil {
		return fmt.Errorf("record media grant event: %w", err)
	}

	return operationErr
}

func (service *GrantService) newGrantEvent(
	actor identity.User,
	eventType audit.Type,
	targetID string,
	outcome audit.Outcome,
	reason audit.Reason,
	now time.Time,
) (audit.Event, error) {
	event, err := audit.NewEvent(audit.Event{
		ID: service.eventIDs(), OccurredAt: now.UTC(), Type: eventType,
		Outcome: outcome, ActorID: actor.ID, ActorUsername: actor.Username,
		ActorRole: string(actor.Role), TargetType: audit.TargetGrant,
		TargetID: targetID, Reason: reason,
	})
	if err != nil {
		return audit.Event{}, fmt.Errorf("create media grant audit event: %w", err)
	}

	return event, nil
}

func grantTargetID(mediaID string, userID string) string {
	return mediaID + ":" + userID
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
