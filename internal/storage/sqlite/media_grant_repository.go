package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ebe542/go-mediaarchive/internal/media"
)

// MediaGrantRepository stores per-user media permission masks in SQLite.
type MediaGrantRepository struct {
	database *sql.DB
}

// Verify that MediaGrantRepository implements the media grant contract.
var _ media.GrantRepository = (*MediaGrantRepository)(nil)

// NewMediaGrantRepository creates a SQLite-backed grant repository.
func NewMediaGrantRepository(argDatabase *sql.DB) *MediaGrantRepository {
	return &MediaGrantRepository{database: argDatabase}
}

// Save inserts a grant or replaces its complete permission mask.
func (repository *MediaGrantRepository) Save(
	argContext context.Context,
	argGrant media.Grant,
) error {
	if err := argGrant.Validate(); err != nil {
		return fmt.Errorf("validate media grant for save: %w", err)
	}

	_, err := repository.database.ExecContext(
		argContext,
		`
			INSERT INTO media_grants (media_id, user_id, permissions)
			VALUES (?, ?, ?)
			ON CONFLICT (media_id, user_id)
			DO UPDATE SET permissions = excluded.permissions
		`,
		argGrant.MediaID,
		argGrant.UserID,
		argGrant.Permissions,
	)
	if err != nil {
		return fmt.Errorf("save media grant: %w", err)
	}

	return nil
}

// Find retrieves one grant by its media and grantee IDs.
func (repository *MediaGrantRepository) Find(
	argContext context.Context,
	argMediaID string,
	argUserID string,
) (media.Grant, error) {
	var permissions int64
	err := repository.database.QueryRowContext(
		argContext,
		`
			SELECT permissions
			FROM media_grants
			WHERE media_id = ? AND user_id = ?
		`,
		argMediaID,
		argUserID,
	).Scan(&permissions)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Grant{}, fmt.Errorf(
			"%w: media ID %q and user ID %q",
			media.ErrGrantNotFound,
			argMediaID,
			argUserID,
		)
	}
	if err != nil {
		return media.Grant{}, fmt.Errorf("select media grant: %w", err)
	}

	return validateStoredGrant(argMediaID, argUserID, permissions)
}

// ListByMedia returns grants in deterministic grantee-ID order.
func (repository *MediaGrantRepository) ListByMedia(
	argContext context.Context,
	argMediaID string,
) ([]media.Grant, error) {
	rows, err := repository.database.QueryContext(
		argContext,
		`
			SELECT user_id, permissions
			FROM media_grants
			WHERE media_id = ?
			ORDER BY user_id
		`,
		argMediaID,
	)
	if err != nil {
		return nil, fmt.Errorf("list media grants: %w", err)
	}
	defer rows.Close()

	grants := make([]media.Grant, 0)
	for rows.Next() {
		var userID string
		var permissions int64
		if err := rows.Scan(&userID, &permissions); err != nil {
			return nil, fmt.Errorf("scan media grant: %w", err)
		}
		grant, err := validateStoredGrant(argMediaID, userID, permissions)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media grants: %w", err)
	}

	return grants, nil
}

// Delete explicitly removes one grant instead of storing an empty mask.
func (repository *MediaGrantRepository) Delete(
	argContext context.Context,
	argMediaID string,
	argUserID string,
) error {
	result, err := repository.database.ExecContext(
		argContext,
		`DELETE FROM media_grants WHERE media_id = ? AND user_id = ?`,
		argMediaID,
		argUserID,
	)
	if err != nil {
		return fmt.Errorf("delete media grant: %w", err)
	}
	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted media grant count: %w", err)
	}
	if affectedRows != 1 {
		return media.ErrGrantNotFound
	}

	return nil
}

func validateStoredGrant(
	argMediaID string,
	argUserID string,
	argPermissions int64,
) (media.Grant, error) {
	if argPermissions < 0 || argPermissions > 255 {
		return media.Grant{}, fmt.Errorf(
			"validate stored media grant: %w: permission value %d",
			media.ErrInvalidGrant,
			argPermissions,
		)
	}
	grant, err := media.NewGrant(
		argMediaID,
		argUserID,
		media.PermissionSet(argPermissions),
	)
	if err != nil {
		return media.Grant{}, fmt.Errorf("validate stored media grant: %w", err)
	}

	return grant, nil
}
