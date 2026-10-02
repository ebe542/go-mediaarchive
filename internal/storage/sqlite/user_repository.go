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
	Scan(destinations ...any) error
}

type statementExecutor interface {
	ExecContext(
		ctx context.Context,
		query string,
		arguments ...any,
	) (sql.Result, error)
}

type userQueryer interface {
	QueryRowContext(
		ctx context.Context,
		query string,
		arguments ...any,
	) *sql.Row
}

func scanUser(row rowScanner) (identity.User, error) {
	var storedUser identity.User
	var role string
	var active bool
	var createdAt string
	var updatedAt string

	err := row.Scan(
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
func NewUserRepository(database *sql.DB) *UserRepository {
	return &UserRepository{
		database: database,
	}
}

// Create persists a new user identity.
func (repository *UserRepository) Create(
	ctx context.Context,
	user identity.User,
) error {
	_, err := repository.database.ExecContext(
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
	ctx context.Context,
	user identity.User,
) error {
	return repository.UpdatePreservingLastAdministrator(ctx, user)
}

// UpdatePreservingLastAdministrator atomically prevents removal of the last
// active administrator.
func (repository *UserRepository) UpdatePreservingLastAdministrator(
	ctx context.Context,
	user identity.User,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin protected user update: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	existingUser, err := findUserByID(
		ctx,
		transaction,
		user.ID,
	)
	if err != nil {
		return err
	}

	removesActiveAdministrator := existingUser.Active &&
		existingUser.Role == identity.RoleAdmin &&
		(!user.Active || user.Role != identity.RoleAdmin)
	if removesActiveAdministrator {
		var activeAdministratorCount int

		if err := transaction.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM users WHERE role = 'admin' AND active = 1`,
		).Scan(&activeAdministratorCount); err != nil {
			return fmt.Errorf("count active administrators: %w", err)
		}

		if activeAdministratorCount <= 1 {
			return identity.ErrLastAdministrator
		}
	}

	if err := updateUser(ctx, transaction, user); err != nil {
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
	ctx context.Context,
	id string,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin protected user deletion: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	existingUser, err := findUserByID(ctx, transaction, id)
	if err != nil {
		return err
	}
	if existingUser.Active && existingUser.Role == identity.RoleAdmin {
		var activeAdministratorCount int
		if err := transaction.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM users WHERE role = 'admin' AND active = 1`,
		).Scan(&activeAdministratorCount); err != nil {
			return fmt.Errorf("count active administrators: %w", err)
		}
		if activeAdministratorCount <= 1 {
			return identity.ErrLastAdministrator
		}
	}

	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM media_grants WHERE user_id = ?`,
		id,
	); err != nil {
		return fmt.Errorf("delete user records from media_grants: %w", err)
	}

	var ownsMedia bool
	if err := transaction.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM media_items WHERE owner_id = ?)`,
		id,
	).Scan(&ownsMedia); err != nil {
		return fmt.Errorf("check user media ownership: %w", err)
	}
	if ownsMedia {
		return identity.ErrUserOwnsMedia
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
			ctx,
			relatedDelete.query,
			id,
		); err != nil {
			return fmt.Errorf(
				"delete user records from %s: %w",
				relatedDelete.name,
				err,
			)
		}
	}

	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM users WHERE id = ?`,
		id,
	); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit protected user deletion: %w", err)
	}

	return nil
}

func updateUser(
	ctx context.Context,
	executor statementExecutor,
	user identity.User,
) error {
	result, err := executor.ExecContext(
		ctx,
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
		user.Username,
		user.DisplayName,
		user.Role,
		user.Active,
		user.UpdatedAt.Format(time.RFC3339Nano),
		user.ID,
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
			user.ID,
		)
	}

	return nil
}

// FindByID retrieves a user identity by its canonical ID.
func (repository *UserRepository) FindByID(
	ctx context.Context,
	id string,
) (identity.User, error) {
	return findUserByID(ctx, repository.database, id)
}

func findUserByID(
	ctx context.Context,
	queryer userQueryer,
	id string,
) (identity.User, error) {
	row := queryer.QueryRowContext(
		ctx,
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
		id,
	)

	storedUser, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.User{}, fmt.Errorf(
			"%w: ID %q",
			identity.ErrUserNotFound,
			id,
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
	ctx context.Context,
	username string,
) (identity.User, error) {
	normalizedUsername, err := identity.NormalizeUsername(username)
	if err != nil {
		return identity.User{}, err
	}

	row := repository.database.QueryRowContext(
		ctx,
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
	ctx context.Context,
	cursor *appusers.Cursor,
	limit int,
) ([]identity.User, error) {
	if limit < 1 {
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

	if cursor != nil {
		cursor, err := appusers.NewCursor(
			cursor.CreatedAt,
			cursor.ID,
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
	arguments = append(arguments, limit)

	rows, err := repository.database.QueryContext(
		ctx,
		query,
		arguments...,
	)
	if err != nil {
		return nil, fmt.Errorf("select user page: %w", err)
	}
	defer func() { _ = rows.Close() }()

	listedUsers := make([]identity.User, 0, limit)
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

func isUniqueConstraintError(inputError error) bool {
	var sqliteError sqliteCodedError

	if !errors.As(inputError, &sqliteError) {
		return false
	}

	switch sqliteError.Code() {
	case sqliteConstraintPrimaryKey, sqliteConstraintUnique:
		return true
	default:
		return false
	}
}
