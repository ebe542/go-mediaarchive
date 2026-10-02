package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/content"
)

// ContentLocationRepository stores managed-content associations in SQLite.
type ContentLocationRepository struct {
	database *sql.DB
}

// Verify that ContentLocationRepository implements the content contract.
var _ content.LocationRepository = (*ContentLocationRepository)(nil)

// NewContentLocationRepository creates a SQLite-backed location repository.
func NewContentLocationRepository(database *sql.DB) *ContentLocationRepository {
	return &ContentLocationRepository{database: database}
}

// Create persists one media-to-content association.
func (repository *ContentLocationRepository) Create(
	ctx context.Context,
	location content.Location,
) error {
	location, err := content.NewLocation(
		location.MediaID,
		location.StorageKey,
		location.StoredAt,
	)
	if err != nil {
		return fmt.Errorf("validate content location for creation: %w", err)
	}

	err = insertContentLocation(ctx, repository.database, location)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %w", content.ErrLocationConflict, err)
		}

		return fmt.Errorf("insert content location: %w", err)
	}

	return nil
}

func insertContentLocation(
	ctx context.Context,
	executor statementExecutor,
	location content.Location,
) error {
	_, err := executor.ExecContext(
		ctx,
		`
			INSERT INTO media_contents (media_id, storage_key, stored_at)
			VALUES (?, ?, ?)
		`,
		location.MediaID,
		location.StorageKey,
		location.StoredAt.Format(time.RFC3339Nano),
	)

	return err
}

// FindByMediaID retrieves the managed-content association for one medium.
func (repository *ContentLocationRepository) FindByMediaID(
	ctx context.Context,
	mediaID string,
) (content.Location, error) {
	var storageKey string
	var storedAt string
	err := repository.database.QueryRowContext(
		ctx,
		`
			SELECT storage_key, stored_at
			FROM media_contents
			WHERE media_id = ?
		`,
		mediaID,
	).Scan(&storageKey, &storedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return content.Location{}, fmt.Errorf(
			"%w: media ID %q",
			content.ErrLocationNotFound,
			mediaID,
		)
	}
	if err != nil {
		return content.Location{}, fmt.Errorf("select content location: %w", err)
	}

	storageTime, err := time.Parse(time.RFC3339Nano, storedAt)
	if err != nil {
		return content.Location{}, fmt.Errorf("parse content storage time: %w", err)
	}
	location, err := content.NewLocation(mediaID, storageKey, storageTime)
	if err != nil {
		return content.Location{}, fmt.Errorf("validate stored content location: %w", err)
	}

	return location, nil
}

// Delete removes the managed-content association for one medium.
func (repository *ContentLocationRepository) Delete(
	ctx context.Context,
	mediaID string,
) error {
	result, err := repository.database.ExecContext(
		ctx,
		`DELETE FROM media_contents WHERE media_id = ?`,
		mediaID,
	)
	if err != nil {
		return fmt.Errorf("delete content location: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted content location count: %w", err)
	}
	if deleted != 1 {
		return content.ErrLocationNotFound
	}

	return nil
}

func requireOneContentLocationRow(result sql.Result) error {
	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected content location row count: %w", err)
	}
	if affectedRows != 1 {
		return content.ErrLocationNotFound
	}

	return nil
}
