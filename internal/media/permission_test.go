package media_test

import (
	"errors"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/media"
)

func TestPermissionValidAndString(t *testing.T) {
	tests := []struct {
		permission media.Permission
		name       string
	}{
		{media.PermissionDiscover, "discover"},
		{media.PermissionRead, "read"},
		{media.PermissionDownload, "download"},
		{media.PermissionUpdate, "update"},
		{media.PermissionDelete, "delete"},
		{media.PermissionShare, "share"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if !testCase.permission.Valid() {
				t.Fatalf("expected %s to be valid", testCase.name)
			}
			if actual := testCase.permission.String(); actual != testCase.name {
				t.Errorf("expected name %q, got %q", testCase.name, actual)
			}
		})
	}

	for _, permission := range []media.Permission{0, 3, 64} {
		if permission.Valid() {
			t.Errorf("expected combined or unknown permission %d to be invalid", permission)
		}
	}
}

func TestNewPermissionSetCombinesAndListsPermissions(t *testing.T) {
	permissions, err := media.NewPermissionSet(
		media.PermissionDownload,
		media.PermissionDiscover,
		media.PermissionRead,
	)
	if err != nil {
		t.Fatalf("create permission set: %v", err)
	}

	for _, permission := range []media.Permission{
		media.PermissionDiscover,
		media.PermissionRead,
		media.PermissionDownload,
	} {
		if !permissions.Has(permission) {
			t.Errorf("expected permission set to contain %s", permission)
		}
	}
	if permissions.Has(media.PermissionShare) {
		t.Fatal("expected permission set not to contain share")
	}

	values := permissions.Values()
	if len(values) != 3 ||
		values[0] != media.PermissionDiscover ||
		values[1] != media.PermissionRead ||
		values[2] != media.PermissionDownload {
		t.Errorf("unexpected stable permission order: %v", values)
	}
}

func TestNewPermissionSetRejectsInvalidInput(t *testing.T) {
	tests := map[string][]media.Permission{
		"empty":     nil,
		"unknown":   {media.Permission(64)},
		"combined":  {media.PermissionDiscover | media.PermissionRead},
		"duplicate": {media.PermissionRead, media.PermissionRead},
	}

	for name, permissions := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := media.NewPermissionSet(permissions...)
			if !errors.Is(err, media.ErrInvalidPermissionSet) {
				t.Fatalf("expected ErrInvalidPermissionSet, got %v", err)
			}
		})
	}
}

func TestNewGrantValidatesIdentifiersAndPermissionSet(t *testing.T) {
	readPermissions := permissionSet(t, media.PermissionRead)
	tests := map[string]struct {
		mediaID       string
		userID        string
		permissions   media.PermissionSet
		expectedError error
	}{
		"valid multiple permissions": {
			mediaID:     validMediaID,
			userID:      validOwnerID,
			permissions: permissionSet(t, media.PermissionDiscover, media.PermissionRead),
		},
		"invalid media ID": {
			mediaID:       "media-id",
			userID:        validOwnerID,
			permissions:   readPermissions,
			expectedError: media.ErrInvalidMediaID,
		},
		"invalid user ID": {
			mediaID:       validMediaID,
			userID:        "user-id",
			permissions:   readPermissions,
			expectedError: identity.ErrInvalidUserID,
		},
		"empty permissions": {
			mediaID:       validMediaID,
			userID:        validOwnerID,
			permissions:   0,
			expectedError: media.ErrInvalidPermissionSet,
		},
		"unknown permission bit": {
			mediaID:       validMediaID,
			userID:        validOwnerID,
			permissions:   media.PermissionSet(64),
			expectedError: media.ErrInvalidPermissionSet,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			grant, err := media.NewGrant(
				testCase.mediaID,
				testCase.userID,
				testCase.permissions,
			)
			if testCase.expectedError != nil {
				if !errors.Is(err, media.ErrInvalidGrant) ||
					!errors.Is(err, testCase.expectedError) {
					t.Fatalf("expected ErrInvalidGrant and %v, got %v", testCase.expectedError, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("create grant: %v", err)
			}
			if grant.MediaID != testCase.mediaID ||
				grant.UserID != testCase.userID ||
				grant.Permissions != testCase.permissions {
				t.Errorf("unexpected grant: %+v", grant)
			}
		})
	}
}

func permissionSet(
	argTest *testing.T,
	argPermissions ...media.Permission,
) media.PermissionSet {
	argTest.Helper()

	permissions, err := media.NewPermissionSet(argPermissions...)
	if err != nil {
		argTest.Fatalf("create permission set: %v", err)
	}

	return permissions
}
