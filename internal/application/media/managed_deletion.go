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
	argService *Service,
	argLocations LocationFinder,
	argDeletions ManagedDeletionRepository,
	argStore content.DeletionStore,
) *ManagedService {
	return &ManagedService{
		Service:   argService,
		locations: argLocations,
		deletions: argDeletions,
		store:     argStore,
	}
}

// DeleteItem removes either metadata-only media or coordinated managed content.
func (service *ManagedService) DeleteItem(
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

	location, err := service.locations.FindByMediaID(argContext, argID)
	if errors.Is(err, content.ErrLocationNotFound) {
		return service.deleteAuthorizedMetadata(argContext, argID)
	}
	if err != nil {
		return fmt.Errorf("retrieve content location for deletion: %w", err)
	}

	staged, err := service.store.StageDelete(argContext, location.StorageKey)
	if err != nil {
		return fmt.Errorf("stage managed content deletion: %w", err)
	}
	if err := service.deletions.DeleteManaged(
		argContext,
		argID,
		location.StorageKey,
	); err != nil {
		rollbackErr := staged.Rollback(context.WithoutCancel(argContext))
		if rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("delete managed media records: %w", err),
				fmt.Errorf("restore staged content: %w", rollbackErr),
			)
		}

		return fmt.Errorf("delete managed media records: %w", err)
	}
	if err := staged.Commit(context.WithoutCancel(argContext)); err != nil {
		return fmt.Errorf("finalize managed content deletion: %w", err)
	}

	return nil
}

func (service *ManagedService) deleteAuthorizedMetadata(
	argContext context.Context,
	argID string,
) error {
	if err := service.repository.Delete(argContext, argID); err != nil {
		if errors.Is(err, domainmedia.ErrItemNotFound) {
			return ErrMediaNotFound
		}

		return fmt.Errorf("delete media identity: %w", err)
	}

	return nil
}
