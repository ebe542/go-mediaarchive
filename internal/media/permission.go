package media

import (
	"errors"
	"fmt"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// Permission represents one independently testable media operation.
type Permission uint8

const (
	// PermissionDiscover permits media metadata to appear in search results.
	PermissionDiscover Permission = 1 << iota
	// PermissionRead permits content streaming for viewing.
	PermissionRead
	// PermissionDownload permits an explicit persistent content download.
	PermissionDownload
	// PermissionUpdate permits replacement of mutable metadata or content.
	PermissionUpdate
	// PermissionDelete permits permanent media removal.
	PermissionDelete
	// PermissionShare permits granting or revoking media permissions.
	PermissionShare
)

const allPermissions = PermissionSet(
	PermissionDiscover |
		PermissionRead |
		PermissionDownload |
		PermissionUpdate |
		PermissionDelete |
		PermissionShare,
)

var (
	// ErrInvalidPermission indicates an unknown or combined requested operation.
	ErrInvalidPermission = errors.New("invalid media permission")
	// ErrInvalidPermissionSet indicates an empty set or one with unknown bits.
	ErrInvalidPermissionSet = errors.New("invalid media permission set")
	// ErrInvalidGrant indicates a malformed explicit media permission grant.
	ErrInvalidGrant = errors.New("invalid media grant")
)

// Valid reports whether the value represents exactly one supported operation.
func (permission Permission) Valid() bool {
	switch permission {
	case PermissionDiscover,
		PermissionRead,
		PermissionDownload,
		PermissionUpdate,
		PermissionDelete,
		PermissionShare:
		return true
	default:
		return false
	}
}

// String returns the stable external name of one permission.
func (permission Permission) String() string {
	switch permission {
	case PermissionDiscover:
		return "discover"
	case PermissionRead:
		return "read"
	case PermissionDownload:
		return "download"
	case PermissionUpdate:
		return "update"
	case PermissionDelete:
		return "delete"
	case PermissionShare:
		return "share"
	default:
		return fmt.Sprintf("Permission(%d)", permission)
	}
}

// PermissionSet stores one or more permissions as a validated bitmask.
type PermissionSet uint8

// NewPermissionSet combines distinct permissions into one validated set.
func NewPermissionSet(argPermissions ...Permission) (PermissionSet, error) {
	var permissions PermissionSet
	for _, permission := range argPermissions {
		if !permission.Valid() {
			return 0, fmt.Errorf(
				"%w: %w: %s",
				ErrInvalidPermissionSet,
				ErrInvalidPermission,
				permission,
			)
		}
		permissionBit := PermissionSet(permission)
		if permissions&permissionBit != 0 {
			return 0, fmt.Errorf(
				"%w: duplicate permission %s",
				ErrInvalidPermissionSet,
				permission,
			)
		}
		permissions |= permissionBit
	}

	if !permissions.Valid() {
		return 0, fmt.Errorf(
			"%w: at least one known permission is required",
			ErrInvalidPermissionSet,
		)
	}

	return permissions, nil
}

// Valid reports whether the set is non-empty and contains no unknown bits.
func (permissions PermissionSet) Valid() bool {
	return permissions != 0 && permissions&^allPermissions == 0
}

// Has reports whether the set contains one requested permission.
func (permissions PermissionSet) Has(argPermission Permission) bool {
	return permissions.Valid() &&
		argPermission.Valid() &&
		permissions&PermissionSet(argPermission) != 0
}

// Values returns permissions in stable external presentation order.
func (permissions PermissionSet) Values() []Permission {
	if !permissions.Valid() {
		return nil
	}

	values := make([]Permission, 0, 6)
	for _, permission := range orderedPermissions() {
		if permissions.Has(permission) {
			values = append(values, permission)
		}
	}

	return values
}

func orderedPermissions() []Permission {
	return []Permission{
		PermissionDiscover,
		PermissionRead,
		PermissionDownload,
		PermissionUpdate,
		PermissionDelete,
		PermissionShare,
	}
}

// Grant assigns one permission set to one user for one media item.
type Grant struct {
	MediaID     string
	UserID      string
	Permissions PermissionSet
}

// NewGrant validates and creates an explicit media permission grant.
func NewGrant(
	argMediaID string,
	argUserID string,
	argPermissions PermissionSet,
) (Grant, error) {
	grant := Grant{
		MediaID:     argMediaID,
		UserID:      argUserID,
		Permissions: argPermissions,
	}
	if err := grant.Validate(); err != nil {
		return Grant{}, err
	}

	return grant, nil
}

// Validate checks a grant reconstructed from an external data source.
func (grant Grant) Validate() error {
	if err := validateCanonicalUUID(grant.MediaID, ErrInvalidMediaID); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidGrant, err)
	}
	if err := identity.ValidateUserID(grant.UserID); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidGrant, err)
	}
	if !grant.Permissions.Valid() {
		return fmt.Errorf(
			"%w: %w: %d",
			ErrInvalidGrant,
			ErrInvalidPermissionSet,
			grant.Permissions,
		)
	}

	return nil
}
