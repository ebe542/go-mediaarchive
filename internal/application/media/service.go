// Package media coordinates authorized media metadata use cases.
package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

var (
	// ErrCreationForbidden indicates that an actor may not create media.
	ErrCreationForbidden = errors.New("media creation forbidden")
	// ErrMediaNotFound masks both unknown and unauthorized media.
	ErrMediaNotFound = errors.New("media not found")
)

// Repository persists media identities and ordered authors.
type Repository interface {
	Create(context.Context, domainmedia.Item) error
	FindByID(context.Context, string) (domainmedia.Item, error)
	Update(context.Context, domainmedia.Item) error
	Delete(context.Context, string) error
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
	clock      Clock
}

// NewService creates a storage-independent media application service.
func NewService(
	argRepository Repository,
	argGrants GrantFinder,
	argIDGenerator IDGenerator,
	argClock Clock,
) *Service {
	return &Service{
		repository: argRepository,
		grants:     argGrants,
		generateID: argIDGenerator,
		clock:      argClock,
	}
}

// CreateItem validates, owns, and persists new media metadata.
func (service *Service) CreateItem(
	argContext context.Context,
	argActor identity.User,
	argInput CreateItemInput,
) (domainmedia.Item, error) {
	if !argActor.Active ||
		(argActor.Role != identity.RoleEditor && argActor.Role != identity.RoleAdmin) {
		return domainmedia.Item{}, ErrCreationForbidden
	}

	now := service.clock()
	item, err := domainmedia.NewItem(
		service.generateID(),
		argInput.Title,
		argInput.Authors,
		argInput.OriginalFilename,
		argInput.Type,
		argInput.MIMEType,
		argInput.Size,
		argInput.Checksum,
		argActor.ID,
		now,
		now,
	)
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("create media identity: %w", err)
	}
	if err := service.repository.Create(argContext, item); err != nil {
		return domainmedia.Item{}, fmt.Errorf("persist media identity: %w", err)
	}

	return item, nil
}

// ItemByID returns discoverable metadata while masking unauthorized items.
func (service *Service) ItemByID(
	argContext context.Context,
	argActor identity.User,
	argID string,
) (domainmedia.Item, error) {
	return service.authorizedItem(
		argContext,
		argActor,
		argID,
		domainmedia.PermissionDiscover,
	)
}

// UpdateItem replaces authorized mutable metadata.
func (service *Service) UpdateItem(
	argContext context.Context,
	argActor identity.User,
	argID string,
	argInput UpdateItemInput,
) (domainmedia.Item, error) {
	existing, err := service.authorizedItem(
		argContext,
		argActor,
		argID,
		domainmedia.PermissionUpdate,
	)
	if err != nil {
		return domainmedia.Item{}, err
	}

	updated, err := domainmedia.NewItem(
		existing.ID,
		argInput.Title,
		argInput.Authors,
		argInput.OriginalFilename,
		argInput.Type,
		argInput.MIMEType,
		argInput.Size,
		argInput.Checksum,
		existing.OwnerID,
		existing.CreatedAt,
		service.clock(),
	)
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("update media identity: %w", err)
	}
	if err := service.repository.Update(argContext, updated); err != nil {
		if errors.Is(err, domainmedia.ErrItemNotFound) {
			return domainmedia.Item{}, ErrMediaNotFound
		}

		return domainmedia.Item{}, fmt.Errorf("persist media update: %w", err)
	}

	return updated, nil
}

// DeleteItem removes authorized media metadata and its dependent records.
func (service *Service) DeleteItem(
	argContext context.Context,
	argActor identity.User,
	argID string,
) error {
	if _, err := service.authorizedItem(
		argContext,
		argActor,
		argID,
		domainmedia.PermissionDelete,
	); err != nil {
		return err
	}
	if err := service.repository.Delete(argContext, argID); err != nil {
		if errors.Is(err, domainmedia.ErrItemNotFound) {
			return ErrMediaNotFound
		}

		return fmt.Errorf("delete media identity: %w", err)
	}

	return nil
}

func (service *Service) authorizedItem(
	argContext context.Context,
	argActor identity.User,
	argID string,
	argPermission domainmedia.Permission,
) (domainmedia.Item, error) {
	item, err := service.repository.FindByID(argContext, argID)
	if errors.Is(err, domainmedia.ErrItemNotFound) {
		return domainmedia.Item{}, ErrMediaNotFound
	}
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("retrieve media for authorization: %w", err)
	}

	var grants []domainmedia.Grant
	if argActor.ID != item.OwnerID {
		grant, grantErr := service.grants.Find(argContext, item.ID, argActor.ID)
		switch {
		case grantErr == nil:
			grants = []domainmedia.Grant{grant}
		case errors.Is(grantErr, domainmedia.ErrGrantNotFound):
		default:
			return domainmedia.Item{}, fmt.Errorf("retrieve actor media grant: %w", grantErr)
		}
	}

	allowed, err := domainmedia.Authorize(argActor, item, argPermission, grants)
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("authorize media operation: %w", err)
	}
	if !allowed {
		return domainmedia.Item{}, ErrMediaNotFound
	}

	return item, nil
}
