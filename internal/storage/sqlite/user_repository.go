package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	appusers "github.com/ebe542/go-mediaarchive/internal/application/users"
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

type statementExecutor interface {
	ExecContext(
		argContext context.Context,
		argQuery string,
		argArguments ...any,
	) (sql.Result, error)
}

type userQueryer interface {
	QueryRowContext(
		argContext context.Context,
		argQuery string,
		argArguments ...any,
	) *sql.Row
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
	return repository.UpdatePreservingLastAdministrator(argContext, argUser)
}

// UpdatePreservingLastAdministrator atomically prevents removal of the last
// active administrator.
func (repository *UserRepository) UpdatePreservingLastAdministrator(
	argContext context.Context,
	argUser identity.User,
) error {
	transaction, err := repository.database.BeginTx(argContext, nil)
	if err != nil {
		return fmt.Errorf("begin protected user update: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	existingUser, err := findUserByID(
		argContext,
		transaction,
		argUser.ID,
	)
	if err != nil {
		return err
	}

	removesActiveAdministrator := existingUser.Active &&
		existingUser.Role == identity.RoleAdmin &&
		(!argUser.Active || argUser.Role != identity.RoleAdmin)
	if removesActiveAdministrator {
		var activeAdministratorCount int

		if err := transaction.QueryRowContext(
			argContext,
			`SELECT COUNT(*) FROM users WHERE role = 'admin' AND active = 1`,
		).Scan(&activeAdministratorCount); err != nil {
			return fmt.Errorf("count active administrators: %w", err)
		}

		if activeAdministratorCount <= 1 {
			return identity.ErrLastAdministrator
		}
	}

	if err := updateUser(argContext, transaction, argUser); err != nil {
		return err
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit protected user update: %w", err)
	}

	return nil
}

// DeletePreservingLastAdministrator atomically deletes authentication records
// and a user without removing the last active administrator.
func (repository *UserRepository) DeletePreservingLastAdministrator(
	argContext context.Context,
	argID string,
) error {
	transaction, err := repository.database.BeginTx(argContext, nil)
	if err != nil {
		return fmt.Errorf("begin protected user deletion: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	existingUser, err := findUserByID(argContext, transaction, argID)
	if err != nil {
		return err
	}
	if existingUser.Active && existingUser.Role == identity.RoleAdmin {
		var activeAdministratorCount int
		if err := transaction.QueryRowContext(
			argContext,
			`SELECT COUNT(*) FROM users WHERE role = 'admin' AND active = 1`,
		).Scan(&activeAdministratorCount); err != nil {
			return fmt.Errorf("count active administrators: %w", err)
		}
		if activeAdministratorCount <= 1 {
			return identity.ErrLastAdministrator
		}
	}

	relatedDeletes := []struct {
		name  string
		query string
	}{
		{"password_enrollments", `DELETE FROM password_enrollments WHERE user_id = ?`},
		{"sessions", `DELETE FROM sessions WHERE user_id = ?`},
		{"password_credentials", `DELETE FROM password_credentials WHERE user_id = ?`},
	}
	for _, relatedDelete := range relatedDeletes {
		if _, err := transaction.ExecContext(
			argContext,
			relatedDelete.query,
			argID,
		); err != nil {
			return fmt.Errorf(
				"delete user records from %s: %w",
				relatedDelete.name,
				err,
			)
		}
	}

	if _, err := transaction.ExecContext(
		argContext,
		`DELETE FROM users WHERE id = ?`,
		argID,
	); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit protected user deletion: %w", err)
	}

	return nil
}

func updateUser(
	argContext context.Context,
	argExecutor statementExecutor,
	argUser identity.User,
) error {
	result, err := argExecutor.ExecContext(
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
	return findUserByID(argContext, repository.database, argID)
}

func findUserByID(
	argContext context.Context,
	argQueryer userQueryer,
	argID string,
) (identity.User, error) {
	row := argQueryer.QueryRowContext(
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

// ListUsers retrieves a bounded page in immutable creation-time and ID order.
func (repository *UserRepository) ListUsers(
	argContext context.Context,
	argCursor *appusers.Cursor,
	argLimit int,
) ([]identity.User, error) {
	if argLimit < 1 {
		return nil, appusers.ErrInvalidPageLimit
	}

	query := `
		SELECT
			id,
			username,
			display_name,
			role,
			active,
			created_at,
			updated_at
		FROM users
	`
	arguments := make([]any, 0, 4)

	if argCursor != nil {
		cursor, err := appusers.NewCursor(
			argCursor.CreatedAt,
			argCursor.ID,
		)
		if err != nil {
			return nil, err
		}

		createdAt := cursor.CreatedAt.Format(time.RFC3339Nano)
		query += `
			WHERE created_at > ?
			   OR (created_at = ? AND id > ?)
		`
		arguments = append(
			arguments,
			createdAt,
			createdAt,
			cursor.ID,
		)
	}

	query += `
		ORDER BY created_at ASC, id ASC
		LIMIT ?
	`
	arguments = append(arguments, argLimit)

	rows, err := repository.database.QueryContext(
		argContext,
		query,
		arguments...,
	)
	if err != nil {
		return nil, fmt.Errorf("select user page: %w", err)
	}
	defer func() { _ = rows.Close() }()

	listedUsers := make([]identity.User, 0, argLimit)
	for rows.Next() {
		storedUser, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user page: %w", err)
		}

		listedUsers = append(listedUsers, storedUser)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user page: %w", err)
	}

	return listedUsers, nil
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
