package media

import (
	"context"
	"errors"
)

// ErrItemNotFound indicates that no media item exists for an identifier.
var ErrItemNotFound = errors.New("media item not found")

// ErrItemConflict indicates that a media item ID already exists.
var ErrItemConflict = errors.New("media item conflict")

// Repository defines persistence operations for media identities and authors.
type Repository interface {
	Create(argContext context.Context, argItem Item) error
	FindByID(argContext context.Context, argID string) (Item, error)
	Update(argContext context.Context, argItem Item) error
	Delete(argContext context.Context, argID string) error
}
