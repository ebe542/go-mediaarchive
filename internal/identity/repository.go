package identity

import (
	"context"
	"errors"
)

// ErrUserNotFound indicates that no user exists for the requested identifier.
var ErrUserNotFound = errors.New("user not found")

// ErrUserConflict indicates that a user ID or username already exists.
var ErrUserConflict = errors.New("user conflict")

// ErrLastAdministrator indicates that an update would remove the last active
// administrator.
var ErrLastAdministrator = errors.New("last active administrator")

// ErrUserOwnsMedia indicates that deletion would orphan owned media.
var ErrUserOwnsMedia = errors.New("user owns media")

// UserRepository defines persistence operations required by user services.
type UserRepository interface {
	Create(ctx context.Context, user User) error
	FindByID(ctx context.Context, id string) (User, error)
	FindByUsername(
		ctx context.Context,
		username string,
	) (User, error)
	Update(ctx context.Context, user User) error
	UpdatePreservingLastAdministrator(
		ctx context.Context,
		user User,
	) error
	DeletePreservingLastAdministrator(
		ctx context.Context,
		id string,
	) error
}
