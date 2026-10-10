// Package media coordinates authorized media metadata use cases.
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
	// ErrCreationForbidden indicates that an actor may not create media.
	ErrCreationForbidden = errors.New("media creation forbidden")
	// ErrMediaNotFound masks both unknown and unauthorized media.
	ErrMediaNotFound = errors.New("media not found")
)

// MediaFinder retrieves a media identity for authorization and inspection.
type MediaFinder interface {
	FindByID(context.Context, string) (domainmedia.Item, error)
}

// Repository persists media identities and ordered authors.
type Repository interface {
	MediaFinder
	Create(context.Context, domainmedia.Item) error
	Update(context.Context, domainmedia.Item) error
	DeleteWithAudit(context.Context, string, audit.Event) error
}

// GrantFinder retrieves only the actor-specific grant needed for a decision.
type GrantFinder interface {
	Find(context.Context, string, string) (domainmedia.Grant, error)
}

// IDGenerator creates stable media identifiers.
type IDGenerator func() string

// Clock returns the current application time.
type Clock func() time.Time

// CreateItemInput contains caller-controlled media metadata.
type CreateItemInput struct {
	Title            string
	Authors          []string
	OriginalFilename string
	Type             domainmedia.Type
	MIMEType         string
	Size             int64
	Checksum         []byte
}

// UpdateItemInput contains caller-controlled mutable media metadata.
type UpdateItemInput = CreateItemInput

// Service coordinates media metadata and authorization.
type Service struct {
	repository Repository
	grants     GrantFinder
	generateID IDGenerator
	audit      audit.Appender
	eventIDs   AuditEventIDGenerator
	clock      Clock
}

// NewService creates a storage-independent media application service.
func NewService(
	repository Repository,
	grantFinder GrantFinder,
	idGenerator IDGenerator,
	auditAppender audit.Appender,
	eventIDGenerator AuditEventIDGenerator,
	clock Clock,
) *Service {
	return &Service{
		repository: repository,
		grants:     grantFinder,
		generateID: idGenerator,
		audit:      auditAppender,
		eventIDs:   eventIDGenerator,
		clock:      clock,
	}
}

// CreateItem validates, owns, and persists new media metadata.
func (service *Service) CreateItem(
	ctx context.Context,
	actor identity.User,
	input CreateItemInput,
) (domainmedia.Item, error) {
	if !mayCreateMedia(actor) {
		return domainmedia.Item{}, ErrCreationForbidden
	}

	now := service.clock()
	item, err := domainmedia.NewItem(
		service.generateID(),
		input.Title,
		input.Authors,
		input.OriginalFilename,
		input.Type,
		input.MIMEType,
		input.Size,
		input.Checksum,
		actor.ID,
		now,
		now,
	)
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("create media identity: %w", err)
	}
	if err := service.repository.Create(ctx, item); err != nil {
		return domainmedia.Item{}, fmt.Errorf("persist media identity: %w", err)
	}

	return item, nil
}

func mayCreateMedia(actor identity.User) bool {
	return actor.Active &&
		(actor.Role == identity.RoleEditor || actor.Role == identity.RoleAdmin)
}

// ItemByID returns discoverable metadata while masking unauthorized items.
func (service *Service) ItemByID(
	ctx context.Context,
	actor identity.User,
	id string,
) (domainmedia.Item, error) {
	return service.authorizedItem(
		ctx,
		actor,
		id,
		domainmedia.PermissionDiscover,
	)
}

// UpdateItem replaces authorized mutable metadata.
func (service *Service) UpdateItem(
	ctx context.Context,
	actor identity.User,
	id string,
	input UpdateItemInput,
) (domainmedia.Item, error) {
	existing, err := service.authorizedItem(
		ctx,
		actor,
		id,
		domainmedia.PermissionUpdate,
	)
	if err != nil {
		return domainmedia.Item{}, err
	}

	updated, err := domainmedia.NewItem(
		existing.ID,
		input.Title,
		input.Authors,
		input.OriginalFilename,
		input.Type,
		input.MIMEType,
		input.Size,
		input.Checksum,
		existing.OwnerID,
		existing.CreatedAt,
		service.clock(),
	)
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("update media identity: %w", err)
	}
	if err := service.repository.Update(ctx, updated); err != nil {
		if errors.Is(err, domainmedia.ErrItemNotFound) {
			return domainmedia.Item{}, ErrMediaNotFound
		}

		return domainmedia.Item{}, fmt.Errorf("persist media update: %w", err)
	}

	return updated, nil
}

// DeleteItem removes authorized media metadata and its dependent records.
func (service *Service) DeleteItem(
	ctx context.Context,
	actor identity.User,
	id string,
) error {
	now := service.clock()
	item, err := service.authorizeDeletion(
		ctx,
		actor,
		id,
		now,
	)
	if err != nil {
		return err
	}
	event, err := service.newDeletionEvent(
		actor,
		item,
		audit.OutcomeSuccess,
		"",
		now,
	)
	if err != nil {
		return err
	}
	if err := service.repository.DeleteWithAudit(ctx, id, event); err != nil {
		if errors.Is(err, domainmedia.ErrItemNotFound) {
			return ErrMediaNotFound
		}

		return fmt.Errorf("delete media identity: %w", err)
	}

	return nil
}

func (service *Service) authorizeDeletion(
	ctx context.Context,
	actor identity.User,
	id string,
	now time.Time,
) (domainmedia.Item, error) {
	item, err := service.authorizedItem(
		ctx,
		actor,
		id,
		domainmedia.PermissionDelete,
	)
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, ErrMediaNotFound) {
		return domainmedia.Item{}, err
	}

	return domainmedia.Item{}, service.recordDeletionEvent(
		ctx,
		actor,
		domainmedia.Item{},
		audit.OutcomeDenied,
		audit.ReasonUnknownTarget,
		now,
		err,
	)
}

func (service *Service) recordDeletionEvent(
	ctx context.Context,
	actor identity.User,
	item domainmedia.Item,
	outcome audit.Outcome,
	reason audit.Reason,
	now time.Time,
	operationErr error,
) error {
	event, err := service.newDeletionEvent(actor, item, outcome, reason, now)
	if err != nil {
		return err
	}
	if err := service.audit.Append(ctx, event); err != nil {
		return fmt.Errorf("record media deletion event: %w", err)
	}

	return operationErr
}

func (service *Service) newDeletionEvent(
	actor identity.User,
	item domainmedia.Item,
	outcome audit.Outcome,
	reason audit.Reason,
	now time.Time,
) (audit.Event, error) {
	event, err := audit.NewEvent(audit.Event{
		ID:            service.eventIDs(),
		OccurredAt:    now.UTC(),
		Type:          audit.TypeMediaDeleted,
		Outcome:       outcome,
		ActorID:       actor.ID,
		ActorUsername: actor.Username,
		ActorRole:     string(actor.Role),
		TargetType:    audit.TargetMedia,
		TargetID:      item.ID,
		TargetName:    item.Title,
		Reason:        reason,
	})
	if err != nil {
		return audit.Event{}, fmt.Errorf("create media deletion audit event: %w", err)
	}

	return event, nil
}

func (service *Service) authorizedItem(
	ctx context.Context,
	actor identity.User,
	id string,
	permission domainmedia.Permission,
) (domainmedia.Item, error) {
	return authorizeItem(
		ctx,
		actor,
		id,
		permission,
		service.repository,
		service.grants,
	)
}

func authorizeItem(
	ctx context.Context,
	actor identity.User,
	id string,
	permission domainmedia.Permission,
	repository MediaFinder,
	grantFinder GrantFinder,
) (domainmedia.Item, error) {
	item, err := repository.FindByID(ctx, id)
	if errors.Is(err, domainmedia.ErrItemNotFound) {
		return domainmedia.Item{}, ErrMediaNotFound
	}
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("retrieve media for authorization: %w", err)
	}

	var grants []domainmedia.Grant
	if actor.ID != item.OwnerID {
		grant, grantErr := grantFinder.Find(ctx, item.ID, actor.ID)
		switch {
		case grantErr == nil:
			grants = []domainmedia.Grant{grant}
		case errors.Is(grantErr, domainmedia.ErrGrantNotFound):
		default:
			return domainmedia.Item{}, fmt.Errorf("retrieve actor media grant: %w", grantErr)
		}
	}

	allowed, err := domainmedia.Authorize(actor, item, permission, grants)
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("authorize media operation: %w", err)
	}
	if !allowed {
		return domainmedia.Item{}, ErrMediaNotFound
	}

	return item, nil
}
