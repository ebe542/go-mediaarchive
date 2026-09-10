package media

import (
	"fmt"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// Authorize decides whether an active user may perform one media operation.
// Global roles never imply access to licensed media content.
func Authorize(
	argUser identity.User,
	argItem Item,
	argPermission Permission,
	argGrants []Grant,
) (bool, error) {
	if !argPermission.Valid() {
		return false, fmt.Errorf(
			"%w: %q",
			ErrInvalidPermission,
			argPermission,
		)
	}
	if !argUser.Active {
		return false, nil
	}
	if err := identity.ValidateUserID(argUser.ID); err != nil {
		return false, fmt.Errorf("validate authorization user: %w", err)
	}
	if err := validateCanonicalUUID(argItem.ID, ErrInvalidMediaID); err != nil {
		return false, fmt.Errorf("validate authorization media: %w", err)
	}
	if err := validateCanonicalUUID(argItem.OwnerID, ErrInvalidOwnerID); err != nil {
		return false, fmt.Errorf("validate authorization owner: %w", err)
	}

	if argUser.ID == argItem.OwnerID {
		return true, nil
	}
	allowed := false
	for _, grant := range argGrants {
		if err := grant.Validate(); err != nil {
			return false, fmt.Errorf("validate authorization grant: %w", err)
		}
		if grant.MediaID == argItem.ID &&
			grant.UserID == argUser.ID &&
			grant.Permissions.Has(argPermission) {
			allowed = true
		}
	}

	return allowed, nil
}
