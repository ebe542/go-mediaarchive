package media

import (
	"context"
	"errors"
	"fmt"

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
	DeleteManaged(context.Context, string, string) error
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
	if _, err := service.authorizedItem(
		ctx,
		actor,
		id,
		domainmedia.PermissionDelete,
	); err != nil {
		return err
	}

	location, err := service.locations.FindByMediaID(ctx, id)
	if errors.Is(err, content.ErrLocationNotFound) {
		return service.deleteAuthorizedMetadata(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("retrieve content location for deletion: %w", err)
	}

	staged, err := service.store.StageDelete(ctx, location.StorageKey)
	if err != nil {
		return fmt.Errorf("stage managed content deletion: %w", err)
	}
	if err := service.deletions.DeleteManaged(
		ctx,
		id,
		location.StorageKey,
	); err != nil {
		rollbackErr := staged.Rollback(context.WithoutCancel(ctx))
		if rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("delete managed media records: %w", err),
				fmt.Errorf("restore staged content: %w", rollbackErr),
			)
		}

		return fmt.Errorf("delete managed media records: %w", err)
	}
	if err := staged.Commit(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("finalize managed content deletion: %w", err)
	}

	return nil
}

func (service *ManagedService) deleteAuthorizedMetadata(
	ctx context.Context,
	id string,
) error {
	if err := service.repository.Delete(ctx, id); err != nil {
		if errors.Is(err, domainmedia.ErrItemNotFound) {
			return ErrMediaNotFound
		}

		return fmt.Errorf("delete media identity: %w", err)
	}

	return nil
}
