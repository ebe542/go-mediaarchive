package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies all pending embedded database migrations.
func Migrate(ctx context.Context, database *sql.DB) error {
	return migrate(ctx, database, embeddedMigrations)
}

func migrate(
	ctx context.Context,
	database *sql.DB,
	migrationFiles fs.FS,
) error {
	if err := createMigrationTable(ctx, database); err != nil {
		return err
	}

	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return err
	}

	for _, migration := range migrations {
		if err := applyMigration(ctx, database, migration); err != nil {
			return err
		}
	}

	return nil
}

func createMigrationTable(
	ctx context.Context,
	database *sql.DB,
) error {
	const statement = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			applied_at TEXT NOT NULL DEFAULT (
				strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			)
		)
	`

	if _, err := database.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	return nil
}

func loadMigrations(migrationFiles fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	versions := make(map[int]string, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		version, err := migrationVersion(entry.Name())
		if err != nil {
			return nil, err
		}

		if existingName, exists := versions[version]; exists {
			return nil, fmt.Errorf(
				"duplicate migration version %d in %q and %q",
				version,
				existingName,
				entry.Name(),
			)
		}
		versions[version] = entry.Name()

		content, err := fs.ReadFile(
			migrationFiles,
			"migrations/"+entry.Name(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"read migration %q: %w",
				entry.Name(),
				err,
			)
		}

		migrations = append(migrations, migration{
			version: version,
			name:    entry.Name(),
			sql:     string(content),
		})
	}

	sort.Slice(migrations, func(first int, second int) bool {
		return migrations[first].version < migrations[second].version
	})

	return migrations, nil
}

func migrationVersion(name string) (int, error) {
	versionText, _, found := strings.Cut(name, "_")
	if !found || !strings.HasSuffix(name, ".sql") {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}

	if len(versionText) != 3 {
		return 0, fmt.Errorf(
			"migration version in %q must contain exactly three digits",
			name,
		)
	}

	for _, character := range versionText {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf(
				"migration version in %q must contain only digits",
				name,
			)
		}
	}

	version, err := strconv.Atoi(versionText)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("invalid migration version in %q", name)
	}

	return version, nil
}

func applyMigration(
	ctx context.Context,
	database *sql.DB,
	migration migration,
) error {
	var applied bool

	if err := database.QueryRowContext(
		ctx,
		"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)",
		migration.version,
	).Scan(&applied); err != nil {
		return fmt.Errorf(
			"check migration %q: %w",
			migration.name,
			err,
		)
	}

	if applied {
		return nil
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf(
			"begin migration %q: %w",
			migration.name,
			err,
		)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, migration.sql); err != nil {
		return fmt.Errorf(
			"execute migration %q: %w",
			migration.name,
			err,
		)
	}

	if _, err := transaction.ExecContext(
		ctx,
		`
			INSERT INTO schema_migrations (version, name)
			VALUES (?, ?)
		`,
		migration.version,
		migration.name,
	); err != nil {
		return fmt.Errorf(
			"record migration %q: %w",
			migration.name,
			err,
		)
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf(
			"commit migration %q: %w",
			migration.name,
			err,
		)
	}

	return nil
}
