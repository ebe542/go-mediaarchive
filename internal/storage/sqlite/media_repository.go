package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/media"
)

// MediaRepository stores media identities and ordered authors in SQLite.
type MediaRepository struct {
	database *sql.DB
}

// Verify at compile time that MediaRepository implements the domain contract.
var _ media.Repository = (*MediaRepository)(nil)

// NewMediaRepository creates a SQLite-backed media repository.
func NewMediaRepository(database *sql.DB) *MediaRepository {
	return &MediaRepository{database: database}
}

// Create atomically persists a media identity and its ordered authors.
func (repository *MediaRepository) Create(
	ctx context.Context,
	item media.Item,
) error {
	item, err := validateMediaItem(item)
	if err != nil {
		return fmt.Errorf("validate media for creation: %w", err)
	}

	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media creation: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	if err := insertMediaItem(ctx, transaction, item); err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %w", media.ErrItemConflict, err)
		}

		return fmt.Errorf("insert media item: %w", err)
	}
	if err := insertMediaAuthors(ctx, transaction, item.ID, item.Authors); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit media creation: %w", err)
	}

	return nil
}

// CreateManaged atomically persists a media identity, ordered authors, and its
// managed-content location.
func (repository *MediaRepository) CreateManaged(
	ctx context.Context,
	candidateItem media.Item,
	candidateLocation content.Location,
) error {
	return repository.createManaged(ctx, candidateItem, candidateLocation, nil)
}

// CreateManagedWithAudit atomically persists managed media and its upload event.
func (repository *MediaRepository) CreateManagedWithAudit(
	ctx context.Context,
	candidateItem media.Item,
	candidateLocation content.Location,
	event audit.Event,
) error {
	return repository.createManaged(ctx, candidateItem, candidateLocation, &event)
}

func (repository *MediaRepository) createManaged(
	ctx context.Context,
	candidateItem media.Item,
	candidateLocation content.Location,
	event *audit.Event,
) error {
	item, err := validateMediaItem(candidateItem)
	if err != nil {
		return fmt.Errorf("validate managed media for creation: %w", err)
	}
	location, err := content.NewLocation(
		candidateLocation.MediaID,
		candidateLocation.StorageKey,
		candidateLocation.StoredAt,
	)
	if err != nil {
		return fmt.Errorf("validate managed content location: %w", err)
	}
	if location.MediaID != item.ID {
		return content.ErrLocationMediaMismatch
	}

	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin managed media creation: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	if err := insertMediaItem(ctx, transaction, item); err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %w", media.ErrItemConflict, err)
		}

		return fmt.Errorf("insert managed media item: %w", err)
	}
	if err := insertMediaAuthors(ctx, transaction, item.ID, item.Authors); err != nil {
		return err
	}
	if err := insertContentLocation(ctx, transaction, location); err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %w", content.ErrLocationConflict, err)
		}

		return fmt.Errorf("insert managed content location: %w", err)
	}
	if event != nil {
		if err := insertAuditEvent(ctx, transaction, *event); err != nil {
			return fmt.Errorf("record managed media creation: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit managed media creation: %w", err)
	}

	return nil
}

// FindByID retrieves a media identity and authors from one read snapshot.
func (repository *MediaRepository) FindByID(
	ctx context.Context,
	id string,
) (media.Item, error) {
	transaction, err := repository.database.BeginTx(
		ctx,
		&sql.TxOptions{ReadOnly: true},
	)
	if err != nil {
		return media.Item{}, fmt.Errorf("begin media read: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	item, err := findMediaItemByID(ctx, transaction, id)
	if err != nil {
		return media.Item{}, err
	}
	if err := transaction.Commit(); err != nil {
		return media.Item{}, fmt.Errorf("commit media read: %w", err)
	}

	return item, nil
}

// Update atomically replaces mutable metadata and ordered authors while
// preserving ownership and creation time.
func (repository *MediaRepository) Update(
	ctx context.Context,
	item media.Item,
) error {
	item, err := validateMediaItem(item)
	if err != nil {
		return fmt.Errorf("validate media for update: %w", err)
	}

	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media update: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	result, err := transaction.ExecContext(
		ctx,
		`
			UPDATE media_items
			SET title = ?, original_filename = ?, media_type = ?, mime_type = ?,
				size = ?, checksum = ?, updated_at = ?
			WHERE id = ?
		`,
		item.Title,
		item.OriginalFilename,
		item.Type,
		item.MIMEType,
		item.Size,
		item.Checksum[:],
		item.UpdatedAt.Format(time.RFC3339Nano),
		item.ID,
	)
	if err != nil {
		return fmt.Errorf("update media item: %w", err)
	}
	if err := requireOneMediaRow(result); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM media_authors WHERE media_id = ?`,
		item.ID,
	); err != nil {
		return fmt.Errorf("delete replaced media authors: %w", err)
	}
	if err := insertMediaAuthors(ctx, transaction, item.ID, item.Authors); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit media update: %w", err)
	}

	return nil
}

// Delete atomically removes grants, authors, and one media identity.
func (repository *MediaRepository) Delete(
	ctx context.Context,
	id string,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media deletion: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	for _, relatedDelete := range []struct {
		name  string
		query string
	}{
		{"grants", `DELETE FROM media_grants WHERE media_id = ?`},
		{"authors", `DELETE FROM media_authors WHERE media_id = ?`},
	} {
		if _, err := transaction.ExecContext(
			ctx,
			relatedDelete.query,
			id,
		); err != nil {
			return fmt.Errorf("delete media %s: %w", relatedDelete.name, err)
		}
	}
	result, err := transaction.ExecContext(
		ctx,
		`DELETE FROM media_items WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete media item: %w", err)
	}
	if err := requireOneMediaRow(result); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit media deletion: %w", err)
	}

	return nil
}

// DeleteManaged atomically removes a matching content location, grants,
// authors, and media identity.
func (repository *MediaRepository) DeleteManaged(
	ctx context.Context,
	id string,
	storageKey string,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin managed media deletion: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	result, err := transaction.ExecContext(
		ctx,
		`DELETE FROM media_contents WHERE media_id = ? AND storage_key = ?`,
		id,
		storageKey,
	)
	if err != nil {
		return fmt.Errorf("delete managed content location: %w", err)
	}
	if err := requireOneContentLocationRow(result); err != nil {
		return err
	}
	for _, relatedDelete := range []struct {
		name  string
		query string
	}{
		{"grants", `DELETE FROM media_grants WHERE media_id = ?`},
		{"authors", `DELETE FROM media_authors WHERE media_id = ?`},
	} {
		if _, err := transaction.ExecContext(
			ctx,
			relatedDelete.query,
			id,
		); err != nil {
			return fmt.Errorf("delete managed media %s: %w", relatedDelete.name, err)
		}
	}
	result, err = transaction.ExecContext(
		ctx,
		`DELETE FROM media_items WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete managed media item: %w", err)
	}
	if err := requireOneMediaRow(result); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit managed media deletion: %w", err)
	}

	return nil
}

func validateMediaItem(item media.Item) (media.Item, error) {
	return media.NewItem(
		item.ID,
		item.Title,
		item.Authors,
		item.OriginalFilename,
		item.Type,
		item.MIMEType,
		item.Size,
		item.Checksum[:],
		item.OwnerID,
		item.CreatedAt,
		item.UpdatedAt,
	)
}

func insertMediaItem(
	ctx context.Context,
	executor statementExecutor,
	item media.Item,
) error {
	_, err := executor.ExecContext(
		ctx,
		`
			INSERT INTO media_items (
				id, title, original_filename, media_type, mime_type, size,
				checksum, owner_id, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
		item.ID,
		item.Title,
		item.OriginalFilename,
		item.Type,
		item.MIMEType,
		item.Size,
		item.Checksum[:],
		item.OwnerID,
		item.CreatedAt.Format(time.RFC3339Nano),
		item.UpdatedAt.Format(time.RFC3339Nano),
	)

	return err
}

func insertMediaAuthors(
	ctx context.Context,
	executor statementExecutor,
	mediaID string,
	authors []string,
) error {
	for position, author := range authors {
		if _, err := executor.ExecContext(
			ctx,
			`INSERT INTO media_authors (media_id, position, name) VALUES (?, ?, ?)`,
			mediaID,
			position,
			author,
		); err != nil {
			return fmt.Errorf("insert media author %d: %w", position, err)
		}
	}

	return nil
}

func findMediaItemByID(
	ctx context.Context,
	transaction *sql.Tx,
	id string,
) (media.Item, error) {
	var stored media.Item
	var mediaType string
	var checksum []byte
	var createdAt string
	var updatedAt string
	err := transaction.QueryRowContext(
		ctx,
		`
			SELECT id, title, original_filename, media_type, mime_type, size,
				checksum, owner_id, created_at, updated_at
			FROM media_items
			WHERE id = ?
		`,
		id,
	).Scan(
		&stored.ID,
		&stored.Title,
		&stored.OriginalFilename,
		&mediaType,
		&stored.MIMEType,
		&stored.Size,
		&checksum,
		&stored.OwnerID,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Item{}, fmt.Errorf("%w: ID %q", media.ErrItemNotFound, id)
	}
	if err != nil {
		return media.Item{}, fmt.Errorf("select media item: %w", err)
	}

	rows, err := transaction.QueryContext(
		ctx,
		`SELECT name FROM media_authors WHERE media_id = ? ORDER BY position`,
		id,
	)
	if err != nil {
		return media.Item{}, fmt.Errorf("select media authors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var author string
		if err := rows.Scan(&author); err != nil {
			return media.Item{}, fmt.Errorf("scan media author: %w", err)
		}
		stored.Authors = append(stored.Authors, author)
	}
	if err := rows.Err(); err != nil {
		return media.Item{}, fmt.Errorf("iterate media authors: %w", err)
	}
	stored.Type = media.Type(mediaType)
	stored.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return media.Item{}, fmt.Errorf("parse media creation time: %w", err)
	}
	stored.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return media.Item{}, fmt.Errorf("parse media update time: %w", err)
	}
	validated, err := media.NewItem(
		stored.ID,
		stored.Title,
		stored.Authors,
		stored.OriginalFilename,
		stored.Type,
		stored.MIMEType,
		stored.Size,
		checksum,
		stored.OwnerID,
		stored.CreatedAt,
		stored.UpdatedAt,
	)
	if err != nil {
		return media.Item{}, fmt.Errorf("validate stored media item: %w", err)
	}
	if len(checksum) != sha256.Size {
		return media.Item{}, media.ErrInvalidChecksum
	}

	return validated, nil
}

func requireOneMediaRow(result sql.Result) error {
	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected media row count: %w", err)
	}
	if affectedRows != 1 {
		return media.ErrItemNotFound
	}

	return nil
}
