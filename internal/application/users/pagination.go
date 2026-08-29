package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

const (
	// DefaultPageLimit is used when a caller does not select a page size.
	DefaultPageLimit = 50
	// MaximumPageLimit bounds database and response work for one request.
	MaximumPageLimit = 100
)

var (
	// ErrInvalidPageLimit indicates a page size outside the supported range.
	ErrInvalidPageLimit = errors.New("invalid user page limit")
	// ErrInvalidCursor indicates an unusable user-directory position.
	ErrInvalidCursor = errors.New("invalid user page cursor")
)

// Cursor identifies the immutable ordering key after the last returned user.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// NewCursor validates and creates a user-directory cursor.
func NewCursor(
	argCreatedAt time.Time,
	argID string,
) (Cursor, error) {
	if argCreatedAt.IsZero() {
		return Cursor{}, fmt.Errorf(
			"%w: creation time must not be zero",
			ErrInvalidCursor,
		)
	}
	if err := identity.ValidateUserID(argID); err != nil {
		return Cursor{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}

	return Cursor{
		CreatedAt: argCreatedAt.UTC(),
		ID:        argID,
	}, nil
}

// ListUsersInput selects a bounded user-directory page.
type ListUsersInput struct {
	Limit  int
	Cursor *Cursor
}

// UserPage contains users and an optional continuation position.
type UserPage struct {
	Users      []identity.User
	NextCursor *Cursor
}

// UserPageRepository loads users after an optional immutable ordering key.
type UserPageRepository interface {
	ListUsers(
		argContext context.Context,
		argCursor *Cursor,
		argLimit int,
	) ([]identity.User, error)
}

// Repository contains every persistence operation required by Service.
type Repository interface {
	identity.UserRepository
	UserPageRepository
}

// ListUsers retrieves a validated keyset-paginated directory page.
func (service *Service) ListUsers(
	argContext context.Context,
	argInput ListUsersInput,
) (UserPage, error) {
	limit := argInput.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit < 1 || limit > MaximumPageLimit {
		return UserPage{}, fmt.Errorf(
			"%w: expected a value from 1 through %d",
			ErrInvalidPageLimit,
			MaximumPageLimit,
		)
	}

	if argInput.Cursor != nil {
		validatedCursor, err := NewCursor(
			argInput.Cursor.CreatedAt,
			argInput.Cursor.ID,
		)
		if err != nil {
			return UserPage{}, err
		}

		argInput.Cursor = &validatedCursor
	}

	users, err := service.repository.ListUsers(
		argContext,
		argInput.Cursor,
		limit+1,
	)
	if err != nil {
		return UserPage{}, fmt.Errorf("retrieve user page: %w", err)
	}

	page := UserPage{Users: users}
	if len(users) <= limit {
		return page, nil
	}

	page.Users = users[:limit]
	lastUser := page.Users[len(page.Users)-1]
	nextCursor, err := NewCursor(lastUser.CreatedAt, lastUser.ID)
	if err != nil {
		return UserPage{}, fmt.Errorf("create next user cursor: %w", err)
	}
	page.NextCursor = &nextCursor

	return page, nil
}
