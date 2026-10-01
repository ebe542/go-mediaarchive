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
func NewContentLocationRepository(argDatabase *sql.DB) *ContentLocationRepository {
	return &ContentLocationRepository{database: argDatabase}
}

// Create persists one media-to-content association.
func (repository *ContentLocationRepository) Create(
	argContext context.Context,
	argLocation content.Location,
) error {
	location, err := content.NewLocation(
		argLocation.MediaID,
		argLocation.StorageKey,
		argLocation.StoredAt,
	)
	if err != nil {
		return fmt.Errorf("validate content location for creation: %w", err)
	}

	err = insertContentLocation(argContext, repository.database, location)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %w", content.ErrLocationConflict, err)
		}

		return fmt.Errorf("insert content location: %w", err)
	}

	return nil
}

func insertContentLocation(
	argContext context.Context,
	argExecutor statementExecutor,
	argLocation content.Location,
) error {
	_, err := argExecutor.ExecContext(
		argContext,
		`
			INSERT INTO media_contents (media_id, storage_key, stored_at)
			VALUES (?, ?, ?)
		`,
		argLocation.MediaID,
		argLocation.StorageKey,
		argLocation.StoredAt.Format(time.RFC3339Nano),
	)

	return err
}

// FindByMediaID retrieves the managed-content association for one medium.
func (repository *ContentLocationRepository) FindByMediaID(
	argContext context.Context,
	argMediaID string,
) (content.Location, error) {
	var storageKey string
	var storedAt string
	err := repository.database.QueryRowContext(
		argContext,
		`
			SELECT storage_key, stored_at
			FROM media_contents
			WHERE media_id = ?
		`,
		argMediaID,
	).Scan(&storageKey, &storedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return content.Location{}, fmt.Errorf(
			"%w: media ID %q",
			content.ErrLocationNotFound,
			argMediaID,
		)
	}
	if err != nil {
		return content.Location{}, fmt.Errorf("select content location: %w", err)
	}

	storageTime, err := time.Parse(time.RFC3339Nano, storedAt)
	if err != nil {
		return content.Location{}, fmt.Errorf("parse content storage time: %w", err)
	}
	location, err := content.NewLocation(argMediaID, storageKey, storageTime)
	if err != nil {
		return content.Location{}, fmt.Errorf("validate stored content location: %w", err)
	}

	return location, nil
}

// Delete removes the managed-content association for one medium.
func (repository *ContentLocationRepository) Delete(
	argContext context.Context,
	argMediaID string,
) error {
	result, err := repository.database.ExecContext(
		argContext,
		`DELETE FROM media_contents WHERE media_id = ?`,
		argMediaID,
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
