package users_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/application/users"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

func TestListUsersAppliesDefaultLimitWithoutNextCursor(t *testing.T) {
	listedUser := paginationUser(
		t,
		"123e4567-e89b-12d3-a456-426614174000",
		time.Date(2026, time.August, 29, 9, 0, 0, 0, time.UTC),
	)
	repository := &recordingUserRepository{
		listedUsers: []identity.User{listedUser},
	}
	service := users.NewService(repository, func() string { return "" }, time.Now)

	page, err := service.ListUsers(
		context.Background(),
		users.ListUsersInput{},
	)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}

	if repository.listedLimit != users.DefaultPageLimit+1 {
		t.Fatalf(
			"expected repository limit %d, got %d",
			users.DefaultPageLimit+1,
			repository.listedLimit,
		)
	}
	if len(page.Users) != 1 || page.Users[0] != listedUser {
		t.Fatalf("unexpected user page: %+v", page)
	}
	if page.NextCursor != nil {
		t.Fatal("expected no cursor for the final page")
	}
}

func TestListUsersUsesLookaheadToCreateNextCursor(t *testing.T) {
	createdAt := time.Date(2026, time.August, 29, 9, 0, 0, 0, time.UTC)
	firstUser := paginationUser(
		t,
		"123e4567-e89b-12d3-a456-426614174000",
		createdAt,
	)
	lookaheadUser := paginationUser(
		t,
		"223e4567-e89b-12d3-a456-426614174000",
		createdAt.Add(time.Minute),
	)
	repository := &recordingUserRepository{
		listedUsers: []identity.User{firstUser, lookaheadUser},
	}
	service := users.NewService(repository, func() string { return "" }, time.Now)

	page, err := service.ListUsers(
		context.Background(),
		users.ListUsersInput{Limit: 1},
	)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}

	if len(page.Users) != 1 || page.Users[0] != firstUser {
		t.Fatalf("unexpected bounded page: %+v", page.Users)
	}
	if page.NextCursor == nil ||
		page.NextCursor.ID != firstUser.ID ||
		page.NextCursor.CreatedAt != firstUser.CreatedAt {
		t.Fatalf("unexpected next cursor: %+v", page.NextCursor)
	}
}

func TestListUsersPassesValidatedCursor(t *testing.T) {
	cursor, err := users.NewCursor(
		time.Date(
			2026,
			time.August,
			29,
			11,
			0,
			0,
			0,
			time.FixedZone("test", 2*60*60),
		),
		"123e4567-e89b-12d3-a456-426614174000",
	)
	if err != nil {
		t.Fatalf("create cursor: %v", err)
	}
	repository := &recordingUserRepository{}
	service := users.NewService(repository, func() string { return "" }, time.Now)

	_, err = service.ListUsers(
		context.Background(),
		users.ListUsersInput{Limit: 25, Cursor: &cursor},
	)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}

	if repository.listedLimit != 26 || repository.listedCursor == nil {
		t.Fatalf(
			"expected cursor and lookahead limit, got cursor=%+v limit=%d",
			repository.listedCursor,
			repository.listedLimit,
		)
	}
	if repository.listedCursor.CreatedAt.Location() != time.UTC {
		t.Fatal("expected cursor timestamp to be normalized to UTC")
	}
}

func TestListUsersRejectsInvalidLimitsAndCursors(t *testing.T) {
	testCases := []struct {
		name        string
		input       users.ListUsersInput
		expectedErr error
	}{
		{
			name:        "negative limit",
			input:       users.ListUsersInput{Limit: -1},
			expectedErr: users.ErrInvalidPageLimit,
		},
		{
			name: "limit above maximum",
			input: users.ListUsersInput{
				Limit: users.MaximumPageLimit + 1,
			},
			expectedErr: users.ErrInvalidPageLimit,
		},
		{
			name: "zero-time cursor",
			input: users.ListUsersInput{
				Limit:  10,
				Cursor: &users.Cursor{ID: "123e4567-e89b-12d3-a456-426614174000"},
			},
			expectedErr: users.ErrInvalidCursor,
		},
		{
			name: "invalid-ID cursor",
			input: users.ListUsersInput{
				Limit: 10,
				Cursor: &users.Cursor{
					CreatedAt: time.Now(),
					ID:        "not-a-uuid",
				},
			},
			expectedErr: users.ErrInvalidCursor,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &recordingUserRepository{}
			service := users.NewService(
				repository,
				func() string { return "" },
				time.Now,
			)

			_, err := service.ListUsers(context.Background(), testCase.input)
			if !errors.Is(err, testCase.expectedErr) {
				t.Fatalf("expected %v, got %v", testCase.expectedErr, err)
			}
			if repository.listedLimit != 0 {
				t.Fatal("expected invalid page not to reach repository")
			}
		})
	}
}

func paginationUser(
	t *testing.T,
	argID string,
	argCreatedAt time.Time,
) identity.User {
	t.Helper()

	user, err := identity.NewUser(
		argID,
		"user_"+argID[:4],
		"Pagination User",
		identity.RoleViewer,
		argCreatedAt,
	)
	if err != nil {
		t.Fatalf("create pagination user: %v", err)
	}

	return user
}
