package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/session"
)

// SessionRepository stores server-side sessions in SQLite.
type SessionRepository struct {
	database *sql.DB
}

// Verify that SessionRepository implements the session contract.
var _ session.Repository = (*SessionRepository)(nil)

// NewSessionRepository creates a SQLite session repository.
func NewSessionRepository(database *sql.DB) *SessionRepository {
	return &SessionRepository{
		database: database,
	}
}

// Create persists a new server-side session.
func (repository *SessionRepository) Create(
	ctx context.Context,
	session session.Session,
) error {
	_, err := repository.database.ExecContext(
		ctx,
		`
			INSERT INTO sessions (
				token_hash,
				user_id,
				created_at,
				last_seen_at,
				expires_at,
				revoked_at
			)
			VALUES (?, ?, ?, ?, ?, ?)
		`,
		session.TokenHash[:],
		session.UserID,
		session.CreatedAt.Format(time.RFC3339Nano),
		session.LastSeenAt.Format(time.RFC3339Nano),
		session.ExpiresAt.Format(time.RFC3339Nano),
		nil,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}

	return nil
}

// FindByTokenHash retrieves a session by its token storage hash.
func (repository *SessionRepository) FindByTokenHash(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
) (session.Session, error) {
	var storedSession session.Session
	var storedTokenHash []byte
	var createdAt string
	var lastSeenAt string
	var expiresAt string
	var revokedAt sql.NullString

	err := repository.database.QueryRowContext(
		ctx,
		`
			SELECT
				token_hash,
				user_id,
				created_at,
				last_seen_at,
				expires_at,
				revoked_at
			FROM sessions
			WHERE token_hash = ?
		`,
		tokenHash[:],
	).Scan(
		&storedTokenHash,
		&storedSession.UserID,
		&createdAt,
		&lastSeenAt,
		&expiresAt,
		&revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return session.Session{}, session.ErrNotFound
	}
	if err != nil {
		return session.Session{}, fmt.Errorf(
			"select session: %w",
			err,
		)
	}

	if len(storedTokenHash) != sha256.Size {
		return session.Session{}, errors.New(
			"stored session token hash has invalid length",
		)
	}
	copy(storedSession.TokenHash[:], storedTokenHash)

	storedSession.CreatedAt, err = parseSessionTime(
		"creation",
		createdAt,
	)
	if err != nil {
		return session.Session{}, err
	}

	storedSession.LastSeenAt, err = parseSessionTime(
		"last-seen",
		lastSeenAt,
	)
	if err != nil {
		return session.Session{}, err
	}

	storedSession.ExpiresAt, err = parseSessionTime(
		"expiration",
		expiresAt,
	)
	if err != nil {
		return session.Session{}, err
	}

	if revokedAt.Valid {
		storedSession.RevokedAt, err = parseSessionTime(
			"revocation",
			revokedAt.String,
		)
		if err != nil {
			return session.Session{}, err
		}
	}

	return storedSession, nil
}

func parseSessionTime(
	name string,
	value string,
) (time.Time, error) {
	parsedTime, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"parse session %s time: %w",
			name,
			err,
		)
	}

	return parsedTime, nil
}

// Touch records recent activity for a non-revoked session.
func (repository *SessionRepository) Touch(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
	now time.Time,
) error {
	result, err := repository.database.ExecContext(
		ctx,
		`
			UPDATE sessions
			SET last_seen_at = ?
			WHERE token_hash = ?
			  AND revoked_at IS NULL
		`,
		now.UTC().Format(time.RFC3339Nano),
		tokenHash[:],
	)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf(
			"read touched session count: %w",
			err,
		)
	}
	if affectedRows == 0 {
		return session.ErrNotFound
	}

	return nil
}

// Revoke idempotently records the first session revocation time.
func (repository *SessionRepository) Revoke(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
	now time.Time,
) error {
	_, err := repository.database.ExecContext(
		ctx,
		`
			UPDATE sessions
			SET revoked_at = COALESCE(revoked_at, ?)
			WHERE token_hash = ?
		`,
		now.UTC().Format(time.RFC3339Nano),
		tokenHash[:],
	)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	return nil
}
