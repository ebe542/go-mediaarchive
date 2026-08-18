package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

const (
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// UserRepository stores and retrieves user identities in SQLite.
type UserRepository struct {
	database *sql.DB
}

// Verify at compile time that UserRepository implements the domain contract.
var _ identity.UserRepository = (*UserRepository)(nil)

type sqliteCodedError interface {
	Code() int
}

type rowScanner interface {
	Scan(argDestinations ...any) error
}

func scanUser(argRow rowScanner) (identity.User, error) {
	var storedUser identity.User
	var role string
	var active bool
	var createdAt string
	var updatedAt string

	err := argRow.Scan(
		&storedUser.ID,
		&storedUser.Username,
		&storedUser.DisplayName,
		&role,
		&active,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return identity.User{}, err
	}

	storedUser.Role = identity.Role(role)
	storedUser.Active = active

	storedUser.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"parse user creation time: %w",
			err,
		)
	}

	storedUser.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"parse user update time: %w",
			err,
		)
	}

	return storedUser, nil
}

// NewUserRepository creates a SQLite-backed user repository.
func NewUserRepository(argDatabase *sql.DB) *UserRepository {
	return &UserRepository{
		database: argDatabase,
	}
}

// Create persists a new user identity.
func (repository *UserRepository) Create(
	argContext context.Context,
	argUser identity.User,
) error {
	_, err := repository.database.ExecContext(
		argContext,
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
		argUser.ID,
		argUser.Username,
		argUser.DisplayName,
		argUser.Role,
		argUser.Active,
		argUser.CreatedAt.Format(time.RFC3339Nano),
		argUser.UpdatedAt.Format(time.RFC3339Nano),
	)
	if isUniqueConstraintError(err) {
		return fmt.Errorf(
			"%w: %w",
			identity.ErrUserConflict,
			err,
		)
	}

	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}

// Update changes a persisted user while preserving its ID and creation time.
func (repository *UserRepository) Update(
	argContext context.Context,
	argUser identity.User,
) error {
	result, err := repository.database.ExecContext(
		argContext,
		`
			UPDATE users
			SET
				username = ?,
				display_name = ?,
				role = ?,
				active = ?,
				updated_at = ?
			WHERE id = ?
		`,
		argUser.Username,
		argUser.DisplayName,
		argUser.Role,
		argUser.Active,
		argUser.UpdatedAt.Format(time.RFC3339Nano),
		argUser.ID,
	)
	if isUniqueConstraintError(err) {
		return fmt.Errorf(
			"%w: %w",
			identity.ErrUserConflict,
			err,
		)
	}
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated user count: %w", err)
	}
	if affectedRows == 0 {
		return fmt.Errorf(
			"%w: ID %q",
			identity.ErrUserNotFound,
			argUser.ID,
		)
	}

	return nil
}

// FindByID retrieves a user identity by its canonical ID.
func (repository *UserRepository) FindByID(
	argContext context.Context,
	argID string,
) (identity.User, error) {
	row := repository.database.QueryRowContext(
		argContext,
		`
			SELECT
				id,
				username,
				display_name,
				role,
				active,
				created_at,
				updated_at
			FROM users
			WHERE id = ?
		`,
		argID,
	)

	storedUser, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.User{}, fmt.Errorf(
			"%w: ID %q",
			identity.ErrUserNotFound,
			argID,
		)
	}
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"select user by ID: %w",
			err,
		)
	}

	return storedUser, nil
}

// FindByUsername retrieves a user identity by its normalized username.
func (repository *UserRepository) FindByUsername(
	argContext context.Context,
	argUsername string,
) (identity.User, error) {
	normalizedUsername, err := identity.NormalizeUsername(argUsername)
	if err != nil {
		return identity.User{}, err
	}

	row := repository.database.QueryRowContext(
		argContext,
		`
			SELECT
				id,
				username,
				display_name,
				role,
				active,
				created_at,
				updated_at
			FROM users
			WHERE username = ?
		`,
		normalizedUsername,
	)

	storedUser, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.User{}, fmt.Errorf(
			"%w: username %q",
			identity.ErrUserNotFound,
			normalizedUsername,
		)
	}
	if err != nil {
		return identity.User{}, fmt.Errorf(
			"select user by username: %w",
			err,
		)
	}

	return storedUser, nil
}

func isUniqueConstraintError(argError error) bool {
	var sqliteError sqliteCodedError

	if !errors.As(argError, &sqliteError) {
		return false
	}

	switch sqliteError.Code() {
	case sqliteConstraintPrimaryKey, sqliteConstraintUnique:
		return true
	default:
		return false
	}
}
