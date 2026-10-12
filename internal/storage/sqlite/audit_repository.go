package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appauditing "github.com/ebe542/go-mediaarchive/internal/application/auditing"
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

// ListAuditEvents retrieves a filtered page in reverse chronological order.
func (repository *AuditRepository) ListAuditEvents(
	ctx context.Context,
	filter appauditing.Filter,
	cursor *appauditing.Cursor,
	limit int,
) ([]audit.Event, error) {
	if limit < 1 {
		return nil, appauditing.ErrInvalidPageLimit
	}

	query := `SELECT id, occurred_at, event_type, outcome,
		actor_id, actor_username, actor_role,
		target_type, target_id, target_name, reason,
		range_start, range_end, range_total
		FROM audit_events`
	conditions := make([]string, 0, 6)
	arguments := make([]any, 0, 9)
	if filter.From != nil {
		conditions = append(conditions, "occurred_at >= ?")
		arguments = append(arguments, filter.From.UTC().Format(time.RFC3339Nano))
	}
	if filter.To != nil {
		conditions = append(conditions, "occurred_at < ?")
		arguments = append(arguments, filter.To.UTC().Format(time.RFC3339Nano))
	}
	if filter.ActorID != "" {
		conditions = append(conditions, "actor_id = ?")
		arguments = append(arguments, filter.ActorID)
	}
	if filter.Type != "" {
		conditions = append(conditions, "event_type = ?")
		arguments = append(arguments, filter.Type)
	}
	if filter.Outcome != "" {
		conditions = append(conditions, "outcome = ?")
		arguments = append(arguments, filter.Outcome)
	}
	if cursor != nil {
		conditions = append(
			conditions,
			"(occurred_at < ? OR (occurred_at = ? AND id < ?))",
		)
		occurredAt := cursor.OccurredAt.UTC().Format(time.RFC3339Nano)
		arguments = append(arguments, occurredAt, occurredAt, cursor.ID)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY occurred_at DESC, id DESC LIMIT ?"
	arguments = append(arguments, limit)

	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("select audit event page: %w", err)
	}
	defer func() { _ = rows.Close() }()

	events := make([]audit.Event, 0, limit)
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit event page: %w", err)
	}

	return events, nil
}

func scanAuditEvent(scanner rowScanner) (audit.Event, error) {
	var event audit.Event
	var occurredAt string
	var actorID, actorUsername, actorRole sql.NullString
	var targetID, targetName, reason sql.NullString
	var rangeStart, rangeEnd, rangeTotal sql.NullInt64
	if err := scanner.Scan(
		&event.ID, &occurredAt, &event.Type, &event.Outcome,
		&actorID, &actorUsername, &actorRole,
		&event.TargetType, &targetID, &targetName, &reason,
		&rangeStart, &rangeEnd, &rangeTotal,
	); err != nil {
		return audit.Event{}, fmt.Errorf("scan audit event: %w", err)
	}
	parsedTime, err := time.Parse(time.RFC3339Nano, occurredAt)
	if err != nil || parsedTime.UTC().Format(time.RFC3339Nano) != occurredAt {
		return audit.Event{}, fmt.Errorf("scan audit event: invalid timestamp")
	}
	event.OccurredAt = parsedTime
	event.ActorID = actorID.String
	event.ActorUsername = actorUsername.String
	event.ActorRole = actorRole.String
	event.TargetID = targetID.String
	event.TargetName = targetName.String
	event.Reason = audit.Reason(reason.String)
	if rangeStart.Valid || rangeEnd.Valid || rangeTotal.Valid {
		if !rangeStart.Valid || !rangeEnd.Valid || !rangeTotal.Valid {
			return audit.Event{}, fmt.Errorf("scan audit event: incomplete range")
		}
		event.Range = &audit.ByteRange{
			Start: rangeStart.Int64,
			End:   rangeEnd.Int64,
			Total: rangeTotal.Int64,
		}
	}
	validated, err := audit.NewEvent(event)
	if err != nil {
		return audit.Event{}, fmt.Errorf("validate stored audit event: %w", err)
	}

	return validated, nil
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
