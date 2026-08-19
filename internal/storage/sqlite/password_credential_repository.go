package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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
	argDatabase *sql.DB,
) *PasswordCredentialRepository {
	return &PasswordCredentialRepository{
		database: argDatabase,
	}
}

// FindByUserID retrieves a password credential by its user ID.
func (repository *PasswordCredentialRepository) FindByUserID(
	argContext context.Context,
	argUserID string,
) (credential.PasswordCredential, error) {
	var storedCredential credential.PasswordCredential
	var createdAt string
	var updatedAt string

	err := repository.database.QueryRowContext(
		argContext,
		`
			SELECT
				user_id,
				password_hash,
				created_at,
				updated_at
			FROM password_credentials
			WHERE user_id = ?
		`,
		argUserID,
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
			argUserID,
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
