package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
)

// AuditRepository appends immutable security events to SQLite.
type AuditRepository struct {
	database *sql.DB
}

// Verify at compile time that AuditRepository implements the domain contract.
var _ audit.Appender = (*AuditRepository)(nil)

// NewAuditRepository creates a SQLite-backed audit event appender.
func NewAuditRepository(database *sql.DB) *AuditRepository {
	return &AuditRepository{database: database}
}

// Append validates and persists one immutable audit event.
func (repository *AuditRepository) Append(ctx context.Context, candidate audit.Event) error {
	return insertAuditEvent(ctx, repository.database, candidate)
}

func insertAuditEvent(
	ctx context.Context,
	executor statementExecutor,
	candidate audit.Event,
) error {
	event, err := audit.NewEvent(candidate)
	if err != nil {
		return fmt.Errorf("validate audit event: %w", err)
	}

	var actorID any
	var actorUsername any
	var actorRole any
	if event.ActorID != "" {
		actorID = event.ActorID
		actorUsername = event.ActorUsername
		actorRole = event.ActorRole
	}
	var targetID any
	if event.TargetID != "" {
		targetID = event.TargetID
	}
	var targetName any
	if event.TargetName != "" {
		targetName = event.TargetName
	}
	var reason any
	if event.Reason != "" {
		reason = event.Reason
	}
	var rangeStart any
	var rangeEnd any
	var rangeTotal any
	if event.Range != nil {
		rangeStart = event.Range.Start
		rangeEnd = event.Range.End
		rangeTotal = event.Range.Total
	}

	_, err = executor.ExecContext(
		ctx,
		`INSERT INTO audit_events (
			id, occurred_at, event_type, outcome,
			actor_id, actor_username, actor_role,
			target_type, target_id, target_name, reason,
			range_start, range_end, range_total
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.OccurredAt.Format(time.RFC3339Nano),
		event.Type,
		event.Outcome,
		actorID,
		actorUsername,
		actorRole,
		event.TargetType,
		targetID,
		targetName,
		reason,
		rangeStart,
		rangeEnd,
		rangeTotal,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %w", audit.ErrEventConflict, err)
		}
		return fmt.Errorf("insert audit event: %w", err)
	}

	return nil
}
