package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
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

// Save creates or atomically replaces one user's current enrollment.
func (repository *PasswordEnrollmentRepository) Save(
	argContext context.Context,
	argEnrollment credential.PasswordEnrollment,
) error {
	_, err := repository.database.ExecContext(
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
