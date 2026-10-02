package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// AdminBootstrapRepository stores the first administrator and credential.
type AdminBootstrapRepository struct {
	database *sql.DB
}

// Verify that AdminBootstrapRepository implements the bootstrap contract.
var _ credential.AdminBootstrapper = (*AdminBootstrapRepository)(nil)

// NewAdminBootstrapRepository creates a SQLite administrator bootstrap repository.
func NewAdminBootstrapRepository(
	database *sql.DB,
) *AdminBootstrapRepository {
	return &AdminBootstrapRepository{
		database: database,
	}
}

// BootstrapAdmin atomically stores the first administrator and credential.
func (repository *AdminBootstrapRepository) BootstrapAdmin(
	ctx context.Context,
	user identity.User,
	passwordCredential credential.PasswordCredential,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin administrator bootstrap: %w", err)
	}
	defer func() {
		_ = transaction.Rollback()
	}()

	var userExists bool
	err = transaction.QueryRowContext(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM users)`,
	).Scan(&userExists)
	if err != nil {
		return fmt.Errorf("check existing users: %w", err)
	}
	if userExists {
		return credential.ErrAlreadyBootstrapped
	}

	_, err = transaction.ExecContext(
		ctx,
		`
			INSERT INTO users (
				id,
				username,
				display_name,
				role,
				active,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`,
		user.ID,
		user.Username,
		user.DisplayName,
		user.Role,
		user.Active,
		user.CreatedAt.Format(time.RFC3339Nano),
		user.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert bootstrap administrator: %w", err)
	}

	_, err = transaction.ExecContext(
		ctx,
		`
			INSERT INTO password_credentials (
				user_id,
				password_hash,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?)
		`,
		passwordCredential.UserID,
		passwordCredential.PasswordHash,
		passwordCredential.CreatedAt.Format(time.RFC3339Nano),
		passwordCredential.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert bootstrap credential: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit administrator bootstrap: %w", err)
	}

	return nil
}
