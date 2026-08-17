// Package sqlite provides SQLite persistence for Media Archive.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const (
	busyTimeoutMilliseconds = 5000
	maxOpenConnections      = 1
)

// Open opens and configures a file-backed SQLite database.
func Open(ctx context.Context, databasePath string) (*sql.DB, error) {
	dataSourceName, err := buildDataSourceName(databasePath)
	if err != nil {
		return nil, fmt.Errorf("build SQLite data source name: %w", err)
	}

	database, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}

	// A single connection provides predictable transaction and PRAGMA behavior
	// while the initial application uses one SQLite writer.
	database.SetMaxOpenConns(maxOpenConnections)
	database.SetMaxIdleConns(maxOpenConnections)

	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()

		return nil, fmt.Errorf("connect to SQLite database: %w", err)
	}

	return database, nil
}

func buildDataSourceName(databasePath string) (string, error) {
	absolutePath, err := filepath.Abs(databasePath)
	if err != nil {
		return "", fmt.Errorf("resolve database path: %w", err)
	}

	parameters := url.Values{}
	parameters.Add(
		"_pragma",
		fmt.Sprintf("busy_timeout(%d)", busyTimeoutMilliseconds),
	)
	parameters.Add("_pragma", "foreign_keys(ON)")
	parameters.Add("_pragma", "journal_mode(WAL)")

	return "file:" + filepath.ToSlash(absolutePath) +
		"?" + parameters.Encode(), nil
}
