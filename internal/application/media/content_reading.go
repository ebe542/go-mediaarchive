package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

var (
	// ErrContentIntegrity indicates inconsistent managed-content metadata or
	// storage state. It must be treated as an internal operational error.
	ErrContentIntegrity = errors.New("managed content integrity failure")
)

// ContentLocationFinder retrieves a managed location after media access has
// been authorized.
type ContentLocationFinder interface {
	FindByMediaID(ctx context.Context, mediaID string) (content.Location, error)
}

// ReadableContent contains authorized metadata and an open content stream. The
// caller owns Reader and must close it.
type ReadableContent struct {
	Item         domainmedia.Item
	Reader       content.ReadSeekCloser
	Size         int64
	LastModified time.Time
}

// ContentReadService authorizes media access before consulting content
// locations or managed storage.
type ContentReadService struct {
	media     MediaFinder
	grants    GrantFinder
	locations ContentLocationFinder
	store     content.ReadStore
}

// NewContentReadService creates an authorized managed-content reader.
func NewContentReadService(
	media MediaFinder,
	grants GrantFinder,
	locations ContentLocationFinder,
	store content.ReadStore,
) *ContentReadService {
	return &ContentReadService{
		media:     media,
		grants:    grants,
		locations: locations,
		store:     store,
	}
}

// Open authorizes read access and opens the corresponding managed content.
func (service *ContentReadService) Open(
	ctx context.Context,
	actor identity.User,
	mediaID string,
) (ReadableContent, error) {
	item, err := authorizeItem(
		ctx,
		actor,
		mediaID,
		domainmedia.PermissionRead,
		service.media,
		service.grants,
	)
	if err != nil {
		return ReadableContent{}, err
	}

	location, err := service.locations.FindByMediaID(ctx, item.ID)
	if errors.Is(err, content.ErrLocationNotFound) {
		return ReadableContent{}, ErrMediaNotFound
	}
	if err != nil {
		return ReadableContent{}, fmt.Errorf("retrieve content location: %w", err)
	}
	if location.MediaID != item.ID {
		return ReadableContent{}, fmt.Errorf(
			"%w: location belongs to a different medium",
			ErrContentIntegrity,
		)
	}

	opened, err := service.store.Open(ctx, location.StorageKey)
	if errors.Is(err, content.ErrNotFound) {
		return ReadableContent{}, ErrMediaNotFound
	}
	if err != nil {
		return ReadableContent{}, fmt.Errorf("open managed content: %w", err)
	}
	if opened.Reader == nil {
		return ReadableContent{}, fmt.Errorf(
			"%w: storage returned no reader",
			ErrContentIntegrity,
		)
	}
	if opened.Size != item.Size {
		integrityErr := fmt.Errorf(
			"%w: stored size does not match media metadata",
			ErrContentIntegrity,
		)
		if closeErr := opened.Reader.Close(); closeErr != nil {
			return ReadableContent{}, errors.Join(
				integrityErr,
				fmt.Errorf("close inconsistent managed content: %w", closeErr),
			)
		}

		return ReadableContent{}, integrityErr
	}

	return ReadableContent{
		Item:         item,
		Reader:       opened.Reader,
		Size:         opened.Size,
		LastModified: opened.LastModified,
	}, nil
}
