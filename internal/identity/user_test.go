package identity_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

func TestRoleValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		role  identity.Role
		valid bool
	}{
		{
			name:  "viewer",
			role:  identity.RoleViewer,
			valid: true,
		},
		{
			name:  "editor",
			role:  identity.RoleEditor,
			valid: true,
		},
		{
			name:  "admin",
			role:  identity.RoleAdmin,
			valid: true,
		},
		{
			name:  "empty",
			role:  identity.Role(""),
			valid: false,
		},
		{
			name:  "unknown",
			role:  identity.Role("owner"),
			valid: false,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if actual := test.role.Valid(); actual != test.valid {
				t.Errorf(
					"expected role validity %t, got %t",
					test.valid,
					actual,
				)
			}
		})
	}
}

func TestNormalizeUsername(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		username      string
		expected      string
		expectedError bool
	}{
		{
			name:     "lowercase username",
			username: "alice",
			expected: "alice",
		},
		{
			name:     "trimmed and normalized username",
			username: "  Alice-Smith_2  ",
			expected: "alice-smith_2",
		},
		{
			name:          "too short",
			username:      "ab",
			expectedError: true,
		},
		{
			name:          "too long",
			username:      "abcdefghijklmnopqrstuvwxyz1234567",
			expectedError: true,
		},
		{
			name:          "starts with hyphen",
			username:      "-alice",
			expectedError: true,
		},
		{
			name:          "contains period",
			username:      "alice.smith",
			expectedError: true,
		},
		{
			name:          "contains whitespace",
			username:      "alice smith",
			expectedError: true,
		},
		{
			name:          "contains non-ASCII characters",
			username:      "älîce",
			expectedError: true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			actual, err := identity.NormalizeUsername(test.username)

			if test.expectedError {
				if err == nil {
					t.Fatal("expected username validation error")
				}

				return
			}

			if err != nil {
				t.Fatalf("normalize username: %v", err)
			}

			if actual != test.expected {
				t.Errorf(
					"expected normalized username %q, got %q",
					test.expected,
					actual,
				)
			}
		})
	}
}

func TestNewUser(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("test", 2*60*60)
	now := time.Date(
		2026,
		time.August,
		18,
		12,
		30,
		0,
		0,
		location,
	)

	user, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"  Alice-Smith  ",
		"  Alice Smith  ",
		identity.RoleEditor,
		now,
	)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if user.ID != "123e4567-e89b-12d3-a456-426614174000" {
		t.Errorf("unexpected user ID %q", user.ID)
	}

	if user.Username != "alice-smith" {
		t.Errorf("expected normalized username %q, got %q", "alice-smith", user.Username)
	}

	if user.DisplayName != "Alice Smith" {
		t.Errorf(
			"expected trimmed display name %q, got %q",
			"Alice Smith",
			user.DisplayName,
		)
	}

	if user.Role != identity.RoleEditor {
		t.Errorf(
			"expected role %q, got %q",
			identity.RoleEditor,
			user.Role,
		)
	}

	if !user.Active {
		t.Error("expected new user to be active")
	}

	expectedTime := now.UTC()

	if !user.CreatedAt.Equal(expectedTime) {
		t.Errorf(
			"expected creation time %v, got %v",
			expectedTime,
			user.CreatedAt,
		)
	}

	if user.CreatedAt.Location() != time.UTC {
		t.Errorf(
			"expected creation time location UTC, got %v",
			user.CreatedAt.Location(),
		)
	}

	if !user.UpdatedAt.Equal(expectedTime) {
		t.Errorf(
			"expected update time %v, got %v",
			expectedTime,
			user.UpdatedAt,
		)
	}
}

func TestNewUserRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	validID := "123e4567-e89b-12d3-a456-426614174000"
	validTime := time.Date(
		2026,
		time.August,
		18,
		12,
		30,
		0,
		0,
		time.UTC,
	)

	tests := []struct {
		name          string
		id            string
		username      string
		displayName   string
		role          identity.Role
		now           time.Time
		expectedError error
	}{
		{
			name:          "invalid UUID",
			id:            "not-a-uuid",
			username:      "alice",
			displayName:   "Alice",
			role:          identity.RoleViewer,
			now:           validTime,
			expectedError: identity.ErrInvalidUserID,
		},
		{
			name:          "uppercase UUID",
			id:            "123E4567-E89B-12D3-A456-426614174000",
			username:      "alice",
			displayName:   "Alice",
			role:          identity.RoleViewer,
			now:           validTime,
			expectedError: identity.ErrInvalidUserID,
		},
		{
			name:          "invalid username",
			id:            validID,
			username:      "a",
			displayName:   "Alice",
			role:          identity.RoleViewer,
			now:           validTime,
			expectedError: identity.ErrInvalidUsername,
		},
		{
			name:          "empty display name",
			id:            validID,
			username:      "alice",
			displayName:   "   ",
			role:          identity.RoleViewer,
			now:           validTime,
			expectedError: identity.ErrInvalidDisplayName,
		},
		{
			name:          "display name too long",
			id:            validID,
			username:      "alice",
			displayName:   strings.Repeat("a", 101),
			role:          identity.RoleViewer,
			now:           validTime,
			expectedError: identity.ErrInvalidDisplayName,
		},
		{
			name:          "invalid role",
			id:            validID,
			username:      "alice",
			displayName:   "Alice",
			role:          identity.Role("owner"),
			now:           validTime,
			expectedError: identity.ErrInvalidRole,
		},
		{
			name:          "zero timestamp",
			id:            validID,
			username:      "alice",
			displayName:   "Alice",
			role:          identity.RoleViewer,
			now:           time.Time{},
			expectedError: identity.ErrInvalidTimestamp,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := identity.NewUser(
				test.id,
				test.username,
				test.displayName,
				test.role,
				test.now,
			)

			if !errors.Is(err, test.expectedError) {
				t.Errorf(
					"expected error %v, got %v",
					test.expectedError,
					err,
				)
			}
		})
	}
}
