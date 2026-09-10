package media_test

import (
	"errors"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/media"
)

const (
	granteeID   = "22c3c390-b5f2-41d4-9292-0c988fbaba0b"
	unrelatedID = "d2719b84-bb50-47a7-ab2f-5f34651b610c"
)

func TestAuthorizeGrantsEveryPermissionToActiveOwner(t *testing.T) {
	item := validPolicyItem(t)
	owner := policyUser(validOwnerID, identity.RoleViewer, true)

	for _, permission := range validPermissions() {
		allowed, err := media.Authorize(owner, item, permission, nil)
		if err != nil {
			t.Fatalf("authorize owner for %q: %v", permission, err)
		}
		if !allowed {
			t.Errorf("expected owner to receive %q", permission)
		}
	}
}

func TestAuthorizeDeniesInactiveOwnerAndGrantee(t *testing.T) {
	item := validPolicyItem(t)
	readGrant := validPolicyGrant(t, item.ID, granteeID, media.PermissionRead)

	for name, user := range map[string]identity.User{
		"owner":   policyUser(validOwnerID, identity.RoleEditor, false),
		"grantee": policyUser(granteeID, identity.RoleViewer, false),
	} {
		t.Run(name, func(t *testing.T) {
			allowed, err := media.Authorize(user, item, media.PermissionRead, []media.Grant{readGrant})
			if err != nil {
				t.Fatalf("authorize inactive user: %v", err)
			}
			if allowed {
				t.Fatal("expected inactive user to be denied")
			}
		})
	}
}

func TestAuthorizeGrantsOnlyExactExplicitPermission(t *testing.T) {
	item := validPolicyItem(t)
	user := policyUser(granteeID, identity.RoleViewer, true)
	readGrant := validPolicyGrant(t, item.ID, user.ID, media.PermissionRead)

	for _, testCase := range []struct {
		permission media.Permission
		allowed    bool
	}{
		{media.PermissionRead, true},
		{media.PermissionDiscover, false},
		{media.PermissionDownload, false},
	} {
		allowed, err := media.Authorize(
			user,
			item,
			testCase.permission,
			[]media.Grant{readGrant},
		)
		if err != nil {
			t.Fatalf("authorize %q: %v", testCase.permission, err)
		}
		if allowed != testCase.allowed {
			t.Errorf(
				"expected %q authorization %t, got %t",
				testCase.permission,
				testCase.allowed,
				allowed,
			)
		}
	}
}

func TestAuthorizeReadsMultiplePermissionsFromOneGrant(t *testing.T) {
	item := validPolicyItem(t)
	user := policyUser(granteeID, identity.RoleViewer, true)
	permissions := permissionSet(
		t,
		media.PermissionDiscover,
		media.PermissionRead,
	)
	grant, err := media.NewGrant(item.ID, user.ID, permissions)
	if err != nil {
		t.Fatalf("create multi-permission grant: %v", err)
	}

	for _, permission := range []media.Permission{
		media.PermissionDiscover,
		media.PermissionRead,
	} {
		allowed, authorizeErr := media.Authorize(
			user,
			item,
			permission,
			[]media.Grant{grant},
		)
		if authorizeErr != nil {
			t.Fatalf("authorize %s: %v", permission, authorizeErr)
		}
		if !allowed {
			t.Errorf("expected combined grant to authorize %s", permission)
		}
	}
}

func TestAuthorizeDoesNotDeriveReadFromDownload(t *testing.T) {
	item := validPolicyItem(t)
	user := policyUser(granteeID, identity.RoleViewer, true)
	downloadGrant := validPolicyGrant(
		t,
		item.ID,
		user.ID,
		media.PermissionDownload,
	)

	allowed, err := media.Authorize(
		user,
		item,
		media.PermissionRead,
		[]media.Grant{downloadGrant},
	)
	if err != nil {
		t.Fatalf("authorize read: %v", err)
	}
	if allowed {
		t.Fatal("expected download not to imply read")
	}
}

func TestAuthorizeIgnoresValidUnrelatedGrants(t *testing.T) {
	item := validPolicyItem(t)
	user := policyUser(granteeID, identity.RoleViewer, true)
	grants := []media.Grant{
		validPolicyGrant(t, unrelatedID, user.ID, media.PermissionRead),
		validPolicyGrant(t, item.ID, unrelatedID, media.PermissionRead),
		validPolicyGrant(t, item.ID, user.ID, media.PermissionDiscover),
	}

	allowed, err := media.Authorize(user, item, media.PermissionRead, grants)
	if err != nil {
		t.Fatalf("authorize unrelated grants: %v", err)
	}
	if allowed {
		t.Fatal("expected unrelated grants not to authorize read")
	}
}

func TestAuthorizeGivesAdministratorNoImplicitMediaPermission(t *testing.T) {
	item := validPolicyItem(t)
	administrator := policyUser(granteeID, identity.RoleAdmin, true)

	for _, permission := range validPermissions() {
		allowed, err := media.Authorize(administrator, item, permission, nil)
		if err != nil {
			t.Fatalf("authorize administrator for %q: %v", permission, err)
		}
		if allowed {
			t.Errorf("expected administrator not to receive implicit %q", permission)
		}
	}
}

func TestAuthorizeRejectsInvalidInputs(t *testing.T) {
	item := validPolicyItem(t)
	user := policyUser(granteeID, identity.RoleViewer, true)

	tests := map[string]struct {
		item          media.Item
		permission    media.Permission
		grants        []media.Grant
		expectedError error
	}{
		"invalid permission": {
			item:          item,
			permission:    media.Permission(64),
			expectedError: media.ErrInvalidPermission,
		},
		"invalid media": {
			item: func() media.Item {
				invalid := item
				invalid.ID = "media-id"

				return invalid
			}(),
			permission:    media.PermissionRead,
			expectedError: media.ErrInvalidMediaID,
		},
		"invalid owner": {
			item: func() media.Item {
				invalid := item
				invalid.OwnerID = "owner-id"

				return invalid
			}(),
			permission:    media.PermissionRead,
			expectedError: media.ErrInvalidOwnerID,
		},
		"invalid grant": {
			item:       item,
			permission: media.PermissionRead,
			grants: []media.Grant{{
				MediaID:     "media-id",
				UserID:      user.ID,
				Permissions: permissionSet(t, media.PermissionRead),
			}},
			expectedError: media.ErrInvalidGrant,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := media.Authorize(
				user,
				testCase.item,
				testCase.permission,
				testCase.grants,
			)
			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("expected %v, got %v", testCase.expectedError, err)
			}
		})
	}
}

func validPolicyItem(argTest *testing.T) media.Item {
	argTest.Helper()

	item, err := newValidItem([]string{"Archive Author"})
	if err != nil {
		argTest.Fatalf("create policy media item: %v", err)
	}

	return item
}

func policyUser(
	argID string,
	argRole identity.Role,
	argActive bool,
) identity.User {
	return identity.User{
		ID:       argID,
		Username: "policy_user",
		Role:     argRole,
		Active:   argActive,
	}
}

func validPolicyGrant(
	argTest *testing.T,
	argMediaID string,
	argUserID string,
	argPermission media.Permission,
) media.Grant {
	argTest.Helper()

	grant, err := media.NewGrant(
		argMediaID,
		argUserID,
		permissionSet(argTest, argPermission),
	)
	if err != nil {
		argTest.Fatalf("create policy grant: %v", err)
	}

	return grant
}

func validPermissions() []media.Permission {
	return []media.Permission{
		media.PermissionDiscover,
		media.PermissionRead,
		media.PermissionDownload,
		media.PermissionUpdate,
		media.PermissionDelete,
		media.PermissionShare,
	}
}
