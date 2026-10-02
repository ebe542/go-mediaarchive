package media

import (
	"context"
	"errors"
)

// ErrItemNotFound indicates that no media item exists for an identifier.
var ErrItemNotFound = errors.New("media item not found")

// ErrItemConflict indicates that a media item ID already exists.
var ErrItemConflict = errors.New("media item conflict")

// ErrGrantNotFound indicates that no grant exists for a media-user pair.
var ErrGrantNotFound = errors.New("media grant not found")

// Repository defines persistence operations for media identities and authors.
type Repository interface {
	Create(ctx context.Context, item Item) error
	FindByID(ctx context.Context, id string) (Item, error)
	Update(ctx context.Context, item Item) error
	Delete(ctx context.Context, id string) error
}

// GrantRepository defines persistence operations for explicit media grants.
type GrantRepository interface {
	Save(ctx context.Context, grant Grant) error
	Find(
		ctx context.Context,
		mediaID string,
		userID string,
	) (Grant, error)
	ListByMedia(ctx context.Context, mediaID string) ([]Grant, error)
	Delete(ctx context.Context, mediaID string, userID string) error
}
