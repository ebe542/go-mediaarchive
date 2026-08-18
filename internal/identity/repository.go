package identity

import (
	"context"
	"errors"
)

// ErrUserNotFound indicates that no user exists for the requested identifier.
var ErrUserNotFound = errors.New("user not found")

// ErrUserConflict indicates that a user ID or username already exists.
var ErrUserConflict = errors.New("user conflict")

// UserRepository defines persistence operations required by user services.
type UserRepository interface {
	Create(argContext context.Context, argUser User) error
	FindByID(argContext context.Context, argID string) (User, error)
	FindByUsername(
		argContext context.Context,
		argUsername string,
	) (User, error)
	Update(argContext context.Context, argUser User) error
}
