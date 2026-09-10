package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/media"
)

// MediaRepository stores media identities and ordered authors in SQLite.
type MediaRepository struct {
	database *sql.DB
}

// Verify at compile time that MediaRepository implements the domain contract.
var _ media.Repository = (*MediaRepository)(nil)

// NewMediaRepository creates a SQLite-backed media repository.
func NewMediaRepository(argDatabase *sql.DB) *MediaRepository {
	return &MediaRepository{database: argDatabase}
}

// Create atomically persists a media identity and its ordered authors.
func (repository *MediaRepository) Create(
	argContext context.Context,
	argItem media.Item,
) error {
	item, err := validateMediaItem(argItem)
	if err != nil {
		return fmt.Errorf("validate media for creation: %w", err)
	}

	transaction, err := repository.database.BeginTx(argContext, nil)
	if err != nil {
		return fmt.Errorf("begin media creation: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	if err := insertMediaItem(argContext, transaction, item); err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %w", media.ErrItemConflict, err)
		}

		return fmt.Errorf("insert media item: %w", err)
	}
	if err := insertMediaAuthors(argContext, transaction, item.ID, item.Authors); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit media creation: %w", err)
	}

	return nil
}

// FindByID retrieves a media identity and authors from one read snapshot.
func (repository *MediaRepository) FindByID(
	argContext context.Context,
	argID string,
) (media.Item, error) {
	transaction, err := repository.database.BeginTx(
		argContext,
		&sql.TxOptions{ReadOnly: true},
	)
	if err != nil {
		return media.Item{}, fmt.Errorf("begin media read: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	item, err := findMediaItemByID(argContext, transaction, argID)
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
	argContext context.Context,
	argItem media.Item,
) error {
	item, err := validateMediaItem(argItem)
	if err != nil {
		return fmt.Errorf("validate media for update: %w", err)
	}

	transaction, err := repository.database.BeginTx(argContext, nil)
	if err != nil {
		return fmt.Errorf("begin media update: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	result, err := transaction.ExecContext(
		argContext,
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
		argContext,
		`DELETE FROM media_authors WHERE media_id = ?`,
		item.ID,
	); err != nil {
		return fmt.Errorf("delete replaced media authors: %w", err)
	}
	if err := insertMediaAuthors(argContext, transaction, item.ID, item.Authors); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit media update: %w", err)
	}

	return nil
}

// Delete atomically removes grants, authors, and one media identity.
func (repository *MediaRepository) Delete(
	argContext context.Context,
	argID string,
) error {
	transaction, err := repository.database.BeginTx(argContext, nil)
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
			argContext,
			relatedDelete.query,
			argID,
		); err != nil {
			return fmt.Errorf("delete media %s: %w", relatedDelete.name, err)
		}
	}
	result, err := transaction.ExecContext(
		argContext,
		`DELETE FROM media_items WHERE id = ?`,
		argID,
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

func validateMediaItem(argItem media.Item) (media.Item, error) {
	return media.NewItem(
		argItem.ID,
		argItem.Title,
		argItem.Authors,
		argItem.OriginalFilename,
		argItem.Type,
		argItem.MIMEType,
		argItem.Size,
		argItem.Checksum[:],
		argItem.OwnerID,
		argItem.CreatedAt,
		argItem.UpdatedAt,
	)
}

func insertMediaItem(
	argContext context.Context,
	argExecutor statementExecutor,
	argItem media.Item,
) error {
	_, err := argExecutor.ExecContext(
		argContext,
		`
			INSERT INTO media_items (
				id, title, original_filename, media_type, mime_type, size,
				checksum, owner_id, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
		argItem.ID,
		argItem.Title,
		argItem.OriginalFilename,
		argItem.Type,
		argItem.MIMEType,
		argItem.Size,
		argItem.Checksum[:],
		argItem.OwnerID,
		argItem.CreatedAt.Format(time.RFC3339Nano),
		argItem.UpdatedAt.Format(time.RFC3339Nano),
	)

	return err
}

func insertMediaAuthors(
	argContext context.Context,
	argExecutor statementExecutor,
	argMediaID string,
	argAuthors []string,
) error {
	for position, author := range argAuthors {
		if _, err := argExecutor.ExecContext(
			argContext,
			`INSERT INTO media_authors (media_id, position, name) VALUES (?, ?, ?)`,
			argMediaID,
			position,
			author,
		); err != nil {
			return fmt.Errorf("insert media author %d: %w", position, err)
		}
	}

	return nil
}

func findMediaItemByID(
	argContext context.Context,
	argTransaction *sql.Tx,
	argID string,
) (media.Item, error) {
	var stored media.Item
	var mediaType string
	var checksum []byte
	var createdAt string
	var updatedAt string
	err := argTransaction.QueryRowContext(
		argContext,
		`
			SELECT id, title, original_filename, media_type, mime_type, size,
				checksum, owner_id, created_at, updated_at
			FROM media_items
			WHERE id = ?
		`,
		argID,
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
		return media.Item{}, fmt.Errorf("%w: ID %q", media.ErrItemNotFound, argID)
	}
	if err != nil {
		return media.Item{}, fmt.Errorf("select media item: %w", err)
	}

	rows, err := argTransaction.QueryContext(
		argContext,
		`SELECT name FROM media_authors WHERE media_id = ? ORDER BY position`,
		argID,
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

func requireOneMediaRow(argResult sql.Result) error {
	affectedRows, err := argResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected media row count: %w", err)
	}
	if affectedRows != 1 {
		return media.ErrItemNotFound
	}

	return nil
}
