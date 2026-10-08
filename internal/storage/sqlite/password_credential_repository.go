package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/credential"
)

// PasswordCredentialRepository loads password credentials from SQLite.
type PasswordCredentialRepository struct {
	database *sql.DB
}

// Verify that PasswordCredentialRepository implements the credential contract.
var _ credential.PasswordCredentialRepository = (*PasswordCredentialRepository)(nil)

// NewPasswordCredentialRepository creates a SQLite credential repository.
func NewPasswordCredentialRepository(
	database *sql.DB,
) *PasswordCredentialRepository {
	return &PasswordCredentialRepository{
		database: database,
	}
}

// FindByUserID retrieves a password credential by its user ID.
func (repository *PasswordCredentialRepository) FindByUserID(
	ctx context.Context,
	userID string,
) (credential.PasswordCredential, error) {
	var storedCredential credential.PasswordCredential
	var createdAt string
	var updatedAt string

	err := repository.database.QueryRowContext(
		ctx,
		`
			SELECT
				user_id,
				password_hash,
				created_at,
				updated_at
			FROM password_credentials
			WHERE user_id = ?
		`,
		userID,
	).Scan(
		&storedCredential.UserID,
		&storedCredential.PasswordHash,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return credential.PasswordCredential{}, fmt.Errorf(
			"%w: user ID %q",
			credential.ErrPasswordCredentialNotFound,
			userID,
		)
	}
	if err != nil {
		return credential.PasswordCredential{}, fmt.Errorf(
			"select password credential: %w",
			err,
		)
	}

	storedCredential.CreatedAt, err = time.Parse(
		time.RFC3339Nano,
		createdAt,
	)
	if err != nil {
		return credential.PasswordCredential{}, fmt.Errorf(
			"parse credential creation time: %w",
			err,
		)
	}

	storedCredential.UpdatedAt, err = time.Parse(
		time.RFC3339Nano,
		updatedAt,
	)
	if err != nil {
		return credential.PasswordCredential{}, fmt.Errorf(
			"parse credential update time: %w",
			err,
		)
	}

	return storedCredential, nil
}

// ChangePasswordAndRevokeSessions atomically replaces a password hash and
// revokes every session belonging to the credential's user.
func (repository *PasswordCredentialRepository) ChangePasswordAndRevokeSessions(
	ctx context.Context,
	passwordCredential credential.PasswordCredential,
) error {
	return repository.changePasswordAndRevokeSessions(
		ctx,
		passwordCredential,
		nil,
	)
}

// ChangePasswordAndRevokeSessionsWithAudit atomically replaces a password,
// revokes its user's sessions, and records the successful change.
func (repository *PasswordCredentialRepository) ChangePasswordAndRevokeSessionsWithAudit(
	ctx context.Context,
	passwordCredential credential.PasswordCredential,
	event audit.Event,
) error {
	return repository.changePasswordAndRevokeSessions(
		ctx,
		passwordCredential,
		&event,
	)
}

func (repository *PasswordCredentialRepository) changePasswordAndRevokeSessions(
	ctx context.Context,
	passwordCredential credential.PasswordCredential,
	event *audit.Event,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password change: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	result, err := transaction.ExecContext(
		ctx,
		`
			UPDATE password_credentials
			SET password_hash = ?, updated_at = ?
			WHERE user_id = ?
		`,
		passwordCredential.PasswordHash,
		passwordCredential.UpdatedAt.Format(time.RFC3339Nano),
		passwordCredential.UserID,
	)
	if err != nil {
		return fmt.Errorf("update password credential: %w", err)
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read changed credential count: %w", err)
	}
	if affectedRows != 1 {
		return credential.ErrPasswordCredentialNotFound
	}

	_, err = transaction.ExecContext(
		ctx,
		`
			UPDATE sessions
			SET revoked_at = COALESCE(revoked_at, ?)
			WHERE user_id = ?
		`,
		passwordCredential.UpdatedAt.Format(time.RFC3339Nano),
		passwordCredential.UserID,
	)
	if err != nil {
		return fmt.Errorf("revoke password change sessions: %w", err)
	}
	if event != nil {
		if err := insertAuditEvent(ctx, transaction, *event); err != nil {
			return fmt.Errorf("record password change: %w", err)
		}
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit password change: %w", err)
	}

	return nil
}
