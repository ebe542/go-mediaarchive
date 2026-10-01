package media

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

// ErrInvalidMaximumUploadSize indicates a non-positive service size limit.
var ErrInvalidMaximumUploadSize = errors.New("invalid maximum upload size")

// ManagedCreator atomically persists a media identity and content location.
type ManagedCreator interface {
	CreateManaged(
		argContext context.Context,
		argItem domainmedia.Item,
		argLocation content.Location,
	) error
}

// UploadInput contains caller-controlled metadata and a streaming source.
type UploadInput struct {
	Title            string
	Authors          []string
	OriginalFilename string
	Type             domainmedia.Type
	MIMEType         string
	Source           io.Reader
}

// UploadService coordinates managed content and atomic metadata creation.
type UploadService struct {
	repository  ManagedCreator
	store       content.Store
	generateID  IDGenerator
	clock       Clock
	maximumSize int64
}

// NewUploadService creates a managed-content upload service.
func NewUploadService(
	argRepository ManagedCreator,
	argStore content.Store,
	argIDGenerator IDGenerator,
	argClock Clock,
	argMaximumSize int64,
) (*UploadService, error) {
	if argMaximumSize <= 0 {
		return nil, ErrInvalidMaximumUploadSize
	}

	return &UploadService{
		repository:  argRepository,
		store:       argStore,
		generateID:  argIDGenerator,
		clock:       argClock,
		maximumSize: argMaximumSize,
	}, nil
}

// UploadItem stores a file and atomically persists its derived metadata and
// location. A database or validation failure removes the newly stored file.
func (service *UploadService) UploadItem(
	argContext context.Context,
	argActor identity.User,
	argInput UploadInput,
) (domainmedia.Item, error) {
	if !mayCreateMedia(argActor) {
		return domainmedia.Item{}, ErrCreationForbidden
	}

	mediaID := service.generateID()
	stored, err := service.store.Put(
		argContext,
		mediaID,
		argInput.Source,
		service.maximumSize,
	)
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("store uploaded content: %w", err)
	}

	now := service.clock()
	item, err := domainmedia.NewItem(
		mediaID,
		argInput.Title,
		argInput.Authors,
		argInput.OriginalFilename,
		argInput.Type,
		argInput.MIMEType,
		stored.Size,
		stored.Checksum[:],
		argActor.ID,
		now,
		now,
	)
	if err != nil {
		return domainmedia.Item{}, service.compensateStoredContent(
			argContext,
			stored.StorageKey,
			fmt.Errorf("create uploaded media identity: %w", err),
		)
	}

	location, err := content.NewLocation(item.ID, stored.StorageKey, now)
	if err != nil {
		return domainmedia.Item{}, service.compensateStoredContent(
			argContext,
			stored.StorageKey,
			fmt.Errorf("create uploaded content location: %w", err),
		)
	}
	if err := service.repository.CreateManaged(argContext, item, location); err != nil {
		return domainmedia.Item{}, service.compensateStoredContent(
			argContext,
			stored.StorageKey,
			fmt.Errorf("persist uploaded media: %w", err),
		)
	}

	return item, nil
}

func (service *UploadService) compensateStoredContent(
	argContext context.Context,
	argStorageKey string,
	argCause error,
) error {
	// Cleanup must still be attempted when the request context was canceled
	// after the content store completed its write.
	if err := service.store.Delete(
		context.WithoutCancel(argContext),
		argStorageKey,
	); err != nil {
		return errors.Join(
			argCause,
			fmt.Errorf("remove stored content after failure: %w", err),
		)
	}

	return argCause
}
