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
	Create(argContext context.Context, argItem Item) error
	FindByID(argContext context.Context, argID string) (Item, error)
	Update(argContext context.Context, argItem Item) error
	Delete(argContext context.Context, argID string) error
}

// GrantRepository defines persistence operations for explicit media grants.
type GrantRepository interface {
	Save(argContext context.Context, argGrant Grant) error
	Find(
		argContext context.Context,
		argMediaID string,
		argUserID string,
	) (Grant, error)
	ListByMedia(argContext context.Context, argMediaID string) ([]Grant, error)
	Delete(argContext context.Context, argMediaID string, argUserID string) error
}
