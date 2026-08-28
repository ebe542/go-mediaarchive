package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// PasswordEnrollmentRepository stores password enrollments in SQLite.
type PasswordEnrollmentRepository struct {
	database *sql.DB
}

// Verify that PasswordEnrollmentRepository implements the credential contract.
var _ credential.PasswordEnrollmentRepository = (*PasswordEnrollmentRepository)(nil)

// NewPasswordEnrollmentRepository creates a SQLite enrollment repository.
func NewPasswordEnrollmentRepository(
	argDatabase *sql.DB,
) *PasswordEnrollmentRepository {
	return &PasswordEnrollmentRepository{
		database: argDatabase,
	}
}

// SaveForCredentiallessUser atomically replaces a current enrollment only
// while the referenced user has no password credential.
func (repository *PasswordEnrollmentRepository) SaveForCredentiallessUser(
	argContext context.Context,
	argEnrollment credential.PasswordEnrollment,
) error {
	transaction, err := repository.database.BeginTx(argContext, nil)
	if err != nil {
		return fmt.Errorf("begin password enrollment save: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var userExists bool
	if err := transaction.QueryRowContext(
		argContext,
		`SELECT EXISTS (SELECT 1 FROM users WHERE id = ?)`,
		argEnrollment.UserID,
	).Scan(&userExists); err != nil {
		return fmt.Errorf("check password enrollment user: %w", err)
	}
	if !userExists {
		return identity.ErrUserNotFound
	}

	credentialExists, err := passwordCredentialExists(
		argContext,
		transaction,
		argEnrollment.UserID,
	)
	if err != nil {
		return err
	}
	if credentialExists {
		return credential.ErrPasswordCredentialExists
	}

	_, err = transaction.ExecContext(
		argContext,
		`
			INSERT INTO password_enrollments (
				user_id,
				token_hash,
				created_at,
				expires_at
			)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id) DO UPDATE SET
				token_hash = excluded.token_hash,
				created_at = excluded.created_at,
				expires_at = excluded.expires_at
		`,
		argEnrollment.UserID,
		argEnrollment.TokenHash[:],
		argEnrollment.CreatedAt.Format(time.RFC3339Nano),
		argEnrollment.ExpiresAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save password enrollment: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit password enrollment save: %w", err)
	}

	return nil
}

// FindByTokenHash retrieves the enrollment matching a storage hash.
func (repository *PasswordEnrollmentRepository) FindByTokenHash(
	argContext context.Context,
	argTokenHash [sha256.Size]byte,
) (credential.PasswordEnrollment, error) {
	var enrollment credential.PasswordEnrollment
	var storedTokenHash []byte
	var createdAt string
	var expiresAt string

	err := repository.database.QueryRowContext(
		argContext,
		`
			SELECT
				user_id,
				token_hash,
				created_at,
				expires_at
			FROM password_enrollments
			WHERE token_hash = ?
		`,
		argTokenHash[:],
	).Scan(
		&enrollment.UserID,
		&storedTokenHash,
		&createdAt,
		&expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return credential.PasswordEnrollment{}, credential.ErrPasswordEnrollmentNotFound
	}
	if err != nil {
		return credential.PasswordEnrollment{}, fmt.Errorf(
			"select password enrollment: %w",
			err,
		)
	}

	if len(storedTokenHash) != sha256.Size {
		return credential.PasswordEnrollment{}, errors.New(
			"stored password enrollment token hash has invalid length",
		)
	}
	copy(enrollment.TokenHash[:], storedTokenHash)

	enrollment.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return credential.PasswordEnrollment{}, fmt.Errorf(
			"parse password enrollment creation time: %w",
			err,
		)
	}

	enrollment.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return credential.PasswordEnrollment{}, fmt.Errorf(
			"parse password enrollment expiration time: %w",
			err,
		)
	}

	return enrollment, nil
}

// CreateCredentialAndConsume atomically creates the initial password
// credential and removes the matching unexpired enrollment.
func (repository *PasswordEnrollmentRepository) CreateCredentialAndConsume(
	argContext context.Context,
	argTokenHash [sha256.Size]byte,
	argCredential credential.PasswordCredential,
	argNow time.Time,
) error {
	transaction, err := repository.database.BeginTx(argContext, nil)
	if err != nil {
		return fmt.Errorf("begin password enrollment consumption: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var userID string
	var expiresAt string
	err = transaction.QueryRowContext(
		argContext,
		`
			SELECT user_id, expires_at
			FROM password_enrollments
			WHERE token_hash = ?
		`,
		argTokenHash[:],
	).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return credential.ErrPasswordEnrollmentNotFound
	}
	if err != nil {
		return fmt.Errorf("select consumed password enrollment: %w", err)
	}

	expiration, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return fmt.Errorf("parse consumed enrollment expiration time: %w", err)
	}
	if argNow.IsZero() || !argNow.UTC().Before(expiration) {
		return credential.ErrPasswordEnrollmentNotFound
	}
	if argCredential.UserID != userID {
		return errors.New("password credential user does not match enrollment")
	}

	credentialExists, err := passwordCredentialExists(
		argContext,
		transaction,
		userID,
	)
	if err != nil {
		return err
	}
	if credentialExists {
		return credential.ErrPasswordCredentialExists
	}

	_, err = transaction.ExecContext(
		argContext,
		`
			INSERT INTO password_credentials (
				user_id,
				password_hash,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?)
		`,
		argCredential.UserID,
		argCredential.PasswordHash,
		argCredential.CreatedAt.Format(time.RFC3339Nano),
		argCredential.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert enrolled password credential: %w", err)
	}

	result, err := transaction.ExecContext(
		argContext,
		`DELETE FROM password_enrollments WHERE token_hash = ?`,
		argTokenHash[:],
	)
	if err != nil {
		return fmt.Errorf("consume password enrollment: %w", err)
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read consumed enrollment count: %w", err)
	}
	if affectedRows != 1 {
		return credential.ErrPasswordEnrollmentNotFound
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit password enrollment consumption: %w", err)
	}

	return nil
}

func passwordCredentialExists(
	argContext context.Context,
	argQuerier interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	argUserID string,
) (bool, error) {
	var exists bool
	if err := argQuerier.QueryRowContext(
		argContext,
		`SELECT EXISTS (SELECT 1 FROM password_credentials WHERE user_id = ?)`,
		argUserID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check existing password credential: %w", err)
	}

	return exists, nil
}
