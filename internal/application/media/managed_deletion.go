package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

// LocationFinder retrieves the content location attached to one medium.
type LocationFinder interface {
	FindByMediaID(context.Context, string) (content.Location, error)
}

// ManagedDeletionRepository atomically removes managed media database records.
type ManagedDeletionRepository interface {
	DeleteManagedWithAudit(context.Context, string, string, audit.Event) error
}

// ManagedService adds coordinated content deletion to metadata operations.
type ManagedService struct {
	*Service
	locations LocationFinder
	deletions ManagedDeletionRepository
	store     content.DeletionStore
}

// NewManagedService decorates a metadata service with managed deletion.
func NewManagedService(
	service *Service,
	locations LocationFinder,
	deletions ManagedDeletionRepository,
	store content.DeletionStore,
) *ManagedService {
	return &ManagedService{
		Service:   service,
		locations: locations,
		deletions: deletions,
		store:     store,
	}
}

// DeleteItem removes either metadata-only media or coordinated managed content.
func (service *ManagedService) DeleteItem(
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

	location, err := service.locations.FindByMediaID(ctx, id)
	if errors.Is(err, content.ErrLocationNotFound) {
		return service.deleteAuthorizedMetadata(ctx, actor, item, now)
	}
	if err != nil {
		return fmt.Errorf("retrieve content location for deletion: %w", err)
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
	staged, err := service.store.StageDelete(ctx, location.StorageKey)
	if err != nil {
		operationErr := fmt.Errorf("stage managed content deletion: %w", err)

		return service.recordDeletionEvent(
			ctx,
			actor,
			item,
			audit.OutcomeFailure,
			audit.ReasonOperationFailure,
			now,
			operationErr,
		)
	}
	if err := service.deletions.DeleteManagedWithAudit(
		ctx,
		id,
		location.StorageKey,
		event,
	); err != nil {
		rollbackErr := staged.Rollback(context.WithoutCancel(ctx))
		if rollbackErr != nil {
			operationErr := errors.Join(
				fmt.Errorf("delete managed media records: %w", err),
				fmt.Errorf("restore staged content: %w", rollbackErr),
			)

			return service.recordDeletionEvent(
				context.WithoutCancel(ctx),
				actor,
				item,
				audit.OutcomeFailure,
				audit.ReasonOperationFailure,
				service.clock(),
				operationErr,
			)
		}

		return fmt.Errorf("delete managed media records: %w", err)
	}
	if err := staged.Commit(context.WithoutCancel(ctx)); err != nil {
		operationErr := fmt.Errorf("finalize managed content deletion: %w", err)

		return service.recordDeletionEvent(
			context.WithoutCancel(ctx),
			actor,
			item,
			audit.OutcomeFailure,
			audit.ReasonOperationFailure,
			service.clock(),
			operationErr,
		)
	}

	return nil
}

func (service *ManagedService) deleteAuthorizedMetadata(
	ctx context.Context,
	actor identity.User,
	item domainmedia.Item,
	now time.Time,
) error {
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
	if err := service.repository.DeleteWithAudit(ctx, item.ID, event); err != nil {
		if errors.Is(err, domainmedia.ErrItemNotFound) {
			return ErrMediaNotFound
		}

		return fmt.Errorf("delete media identity: %w", err)
	}

	return nil
}
