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

var (
	// ErrInvalidMaximumUploadSize indicates a non-positive service size limit.
	ErrInvalidMaximumUploadSize = errors.New("invalid maximum upload size")

	// ErrUploadCompensationFailed indicates that failed upload persistence also
	// left newly stored content requiring operational reconciliation.
	ErrUploadCompensationFailed = errors.New("upload compensation failed")
)

// ManagedCreator atomically persists a media identity and content location.
type ManagedCreator interface {
	CreateManaged(
		ctx context.Context,
		item domainmedia.Item,
		location content.Location,
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
	repository ManagedCreator,
	store content.Store,
	idGenerator IDGenerator,
	clock Clock,
	maximumSize int64,
) (*UploadService, error) {
	if maximumSize <= 0 {
		return nil, ErrInvalidMaximumUploadSize
	}

	return &UploadService{
		repository:  repository,
		store:       store,
		generateID:  idGenerator,
		clock:       clock,
		maximumSize: maximumSize,
	}, nil
}

// UploadItem stores a file and atomically persists its derived metadata and
// location. A database or validation failure removes the newly stored file.
func (service *UploadService) UploadItem(
	ctx context.Context,
	actor identity.User,
	input UploadInput,
) (domainmedia.Item, error) {
	if !mayCreateMedia(actor) {
		return domainmedia.Item{}, ErrCreationForbidden
	}

	mediaID := service.generateID()
	stored, err := service.store.Put(
		ctx,
		mediaID,
		input.Source,
		service.maximumSize,
	)
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("store uploaded content: %w", err)
	}

	now := service.clock()
	item, err := domainmedia.NewItem(
		mediaID,
		input.Title,
		input.Authors,
		input.OriginalFilename,
		input.Type,
		input.MIMEType,
		stored.Size,
		stored.Checksum[:],
		actor.ID,
		now,
		now,
	)
	if err != nil {
		return domainmedia.Item{}, service.compensateStoredContent(
			ctx,
			stored.StorageKey,
			fmt.Errorf("create uploaded media identity: %w", err),
		)
	}

	location, err := content.NewLocation(item.ID, stored.StorageKey, now)
	if err != nil {
		return domainmedia.Item{}, service.compensateStoredContent(
			ctx,
			stored.StorageKey,
			fmt.Errorf("create uploaded content location: %w", err),
		)
	}
	if err := service.repository.CreateManaged(ctx, item, location); err != nil {
		return domainmedia.Item{}, service.compensateStoredContent(
			ctx,
			stored.StorageKey,
			fmt.Errorf("persist uploaded media: %w", err),
		)
	}

	return item, nil
}

func (service *UploadService) compensateStoredContent(
	ctx context.Context,
	storageKey string,
	cause error,
) error {
	// Cleanup must still be attempted when the request context was canceled
	// after the content store completed its write.
	if err := service.store.Delete(
		context.WithoutCancel(ctx),
		storageKey,
	); err != nil {
		return errors.Join(
			ErrUploadCompensationFailed,
			cause,
			fmt.Errorf("remove stored content after failure: %w", err),
		)
	}

	return cause
}
