package media

import (
	"fmt"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// Authorize decides whether an active user may perform one media operation.
// Global roles never imply access to licensed media content.
func Authorize(
	user identity.User,
	item Item,
	permission Permission,
	grants []Grant,
) (bool, error) {
	if !permission.Valid() {
		return false, fmt.Errorf(
			"%w: %q",
			ErrInvalidPermission,
			permission,
		)
	}
	if !user.Active {
		return false, nil
	}
	if err := identity.ValidateUserID(user.ID); err != nil {
		return false, fmt.Errorf("validate authorization user: %w", err)
	}
	if err := validateCanonicalUUID(item.ID, ErrInvalidMediaID); err != nil {
		return false, fmt.Errorf("validate authorization media: %w", err)
	}
	if err := validateCanonicalUUID(item.OwnerID, ErrInvalidOwnerID); err != nil {
		return false, fmt.Errorf("validate authorization owner: %w", err)
	}

	if user.ID == item.OwnerID {
		return true, nil
	}
	allowed := false
	for _, grant := range grants {
		if err := grant.Validate(); err != nil {
			return false, fmt.Errorf("validate authorization grant: %w", err)
		}
		if grant.MediaID == item.ID &&
			grant.UserID == user.ID &&
			grant.Permissions.Has(permission) {
			allowed = true
		}
	}

	return allowed, nil
}
