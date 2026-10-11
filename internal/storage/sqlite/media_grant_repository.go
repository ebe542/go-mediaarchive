package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/media"
)

// MediaGrantRepository stores per-user media permission masks in SQLite.
type MediaGrantRepository struct {
	database *sql.DB
}

// Verify that MediaGrantRepository implements the media grant contract.
var _ media.GrantRepository = (*MediaGrantRepository)(nil)

// NewMediaGrantRepository creates a SQLite-backed grant repository.
func NewMediaGrantRepository(database *sql.DB) *MediaGrantRepository {
	return &MediaGrantRepository{database: database}
}

// Save inserts a grant or replaces its complete permission mask.
func (repository *MediaGrantRepository) Save(
	ctx context.Context,
	grant media.Grant,
) error {
	return repository.save(ctx, grant, nil)
}

// SaveWithAudit atomically replaces a grant and records the change.
func (repository *MediaGrantRepository) SaveWithAudit(
	ctx context.Context,
	grant media.Grant,
	event audit.Event,
) error {
	return repository.save(ctx, grant, &event)
}

func (repository *MediaGrantRepository) save(
	ctx context.Context,
	grant media.Grant,
	event *audit.Event,
) error {
	if err := grant.Validate(); err != nil {
		return fmt.Errorf("validate media grant for save: %w", err)
	}

	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media grant save: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	_, err = transaction.ExecContext(
		ctx,
		`
			INSERT INTO media_grants (media_id, user_id, permissions)
			VALUES (?, ?, ?)
			ON CONFLICT (media_id, user_id)
			DO UPDATE SET permissions = excluded.permissions
		`,
		grant.MediaID,
		grant.UserID,
		grant.Permissions,
	)
	if err != nil {
		return fmt.Errorf("save media grant: %w", err)
	}
	if event != nil {
		if err := insertAuditEvent(ctx, transaction, *event); err != nil {
			return fmt.Errorf("record media grant replacement: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit media grant save: %w", err)
	}

	return nil
}

// Find retrieves one grant by its media and grantee IDs.
func (repository *MediaGrantRepository) Find(
	ctx context.Context,
	mediaID string,
	userID string,
) (media.Grant, error) {
	var permissions int64
	err := repository.database.QueryRowContext(
		ctx,
		`
			SELECT permissions
			FROM media_grants
			WHERE media_id = ? AND user_id = ?
		`,
		mediaID,
		userID,
	).Scan(&permissions)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Grant{}, fmt.Errorf(
			"%w: media ID %q and user ID %q",
			media.ErrGrantNotFound,
			mediaID,
			userID,
		)
	}
	if err != nil {
		return media.Grant{}, fmt.Errorf("select media grant: %w", err)
	}

	return validateStoredGrant(mediaID, userID, permissions)
}

// ListByMedia returns grants in deterministic grantee-ID order.
func (repository *MediaGrantRepository) ListByMedia(
	ctx context.Context,
	mediaID string,
) ([]media.Grant, error) {
	rows, err := repository.database.QueryContext(
		ctx,
		`
			SELECT user_id, permissions
			FROM media_grants
			WHERE media_id = ?
			ORDER BY user_id
		`,
		mediaID,
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
		grant, err := validateStoredGrant(mediaID, userID, permissions)
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
	ctx context.Context,
	mediaID string,
	userID string,
) error {
	return repository.delete(ctx, mediaID, userID, nil)
}

// DeleteWithAudit atomically removes a grant and records its revocation.
func (repository *MediaGrantRepository) DeleteWithAudit(
	ctx context.Context,
	mediaID string,
	userID string,
	event audit.Event,
) error {
	return repository.delete(ctx, mediaID, userID, &event)
}

func (repository *MediaGrantRepository) delete(
	ctx context.Context,
	mediaID string,
	userID string,
	event *audit.Event,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media grant deletion: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	result, err := transaction.ExecContext(
		ctx,
		`DELETE FROM media_grants WHERE media_id = ? AND user_id = ?`,
		mediaID,
		userID,
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
	if event != nil {
		if err := insertAuditEvent(ctx, transaction, *event); err != nil {
			return fmt.Errorf("record media grant revocation: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit media grant deletion: %w", err)
	}

	return nil
}

func validateStoredGrant(
	mediaID string,
	userID string,
	permissions int64,
) (media.Grant, error) {
	if permissions < 0 || permissions > 255 {
		return media.Grant{}, fmt.Errorf(
			"validate stored media grant: %w: permission value %d",
			media.ErrInvalidGrant,
			permissions,
		)
	}
	grant, err := media.NewGrant(
		mediaID,
		userID,
		media.PermissionSet(permissions),
	)
	if err != nil {
		return media.Grant{}, fmt.Errorf("validate stored media grant: %w", err)
	}

	return grant, nil
}
