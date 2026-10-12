package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/google/uuid"
)

// ContentAuditRepository deduplicates content events within one login session.
type ContentAuditRepository struct {
	database *sql.DB
}

// NewContentAuditRepository creates a SQLite-backed content audit repository.
func NewContentAuditRepository(database *sql.DB) *ContentAuditRepository {
	return &ContentAuditRepository{database: database}
}

// AppendOnce records at most one matching event per session and medium.
func (repository *ContentAuditRepository) AppendOnce(
	ctx context.Context,
	sessionHash [sha256.Size]byte,
	mediaID string,
	outcome audit.Outcome,
	reason audit.Reason,
	recordedAt time.Time,
	event audit.Event,
) (bool, error) {
	validatedEvent, err := audit.NewEvent(event)
	if err != nil {
		return false, fmt.Errorf("validate content audit event: %w", err)
	}
	if sessionHash == ([sha256.Size]byte{}) {
		return false, fmt.Errorf("validate content audit marker: empty session hash")
	}
	parsedMediaID, err := uuid.Parse(mediaID)
	if err != nil || parsedMediaID == uuid.Nil || parsedMediaID.String() != mediaID {
		return false, fmt.Errorf("validate content audit marker: invalid media ID")
	}
	if (outcome == audit.OutcomeSuccess) != (reason == "") {
		return false, fmt.Errorf("validate content audit marker: invalid outcome reason")
	}
	if validatedEvent.Type != audit.TypeMediaContentRead ||
		validatedEvent.TargetType != audit.TargetMedia ||
		validatedEvent.TargetID != mediaID ||
		validatedEvent.Outcome != outcome ||
		validatedEvent.Reason != reason ||
		!validatedEvent.OccurredAt.Equal(recordedAt) {
		return false, fmt.Errorf("validate content audit marker: event does not match marker")
	}

	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin content audit marker: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	result, err := transaction.ExecContext(
		ctx,
		`INSERT INTO content_audit_markers (
			session_hash, media_id, outcome, reason, recorded_at
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (session_hash, media_id, outcome, reason) DO NOTHING`,
		sessionHash[:],
		mediaID,
		outcome,
		reason,
		recordedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return false, fmt.Errorf("insert content audit marker: %w", err)
	}
	affectedRows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read content audit marker count: %w", err)
	}
	if affectedRows == 0 {
		if err := transaction.Commit(); err != nil {
			return false, fmt.Errorf("commit existing content audit marker: %w", err)
		}

		return false, nil
	}
	if err := insertAuditEvent(ctx, transaction, validatedEvent); err != nil {
		return false, fmt.Errorf("record content audit event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit content audit marker: %w", err)
	}

	return true, nil
}
