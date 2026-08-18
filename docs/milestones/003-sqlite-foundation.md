# Milestone 3: SQLite foundation

## Goal

Establish a production-oriented SQLite foundation with explicit connection
settings, embedded forward-only migrations, and reproducible integration tests.
This milestone prepares persistence without introducing media repository logic.

## Scope

- Use `database/sql` with the CGO-free `modernc.org/sqlite` driver.
- Create and maintain `go.sum` through Go tooling.
- Open SQLite databases through a dedicated storage package.
- Enable foreign-key enforcement for every database connection.
- Use WAL journal mode for file-backed databases.
- Configure a busy timeout for concurrent access.
- Limit and document the connection-pool behavior appropriate for SQLite.
- Embed numbered SQL migration files in the server binary.
- Record applied migrations in a `schema_migrations` table.
- Apply pending migrations in deterministic order and within transactions.
- Make repeated migration runs idempotent.
- Use temporary database files in integration tests.
- Accept the database path primarily through a server CLI flag.
- Verify module checksums during milestone checks.

## Package structure

```text
internal/storage/sqlite/
├── database.go
├── database_test.go
├── migrate.go
├── migrate_test.go
├── migrate_internal_test.go
└── migrations/
    └── 001_initialize.sql
```

The first migration establishes the version sequence without prematurely
defining domain tables. Domain schemas and repository operations remain outside
this milestone.

## Configuration

The server will accept:

```text
--database PATH
```

Configuration precedence follows the existing project rule:

```text
--database flag > MEDIAARCHIVE_DATABASE environment variable > built-in default
```

The proposed built-in development default is:

```text
data/mediaarchive.db
```

Production deployments must provide an explicit writable data location and must
not store databases inside the source repository.

## Database invariants

Every opened database must satisfy:

```sql
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
```

File-backed databases additionally use:

```sql
PRAGMA journal_mode = WAL;
```

Tests will query these values rather than assuming successful configuration.

## Migration rules

- Migration filenames start with an increasing, zero-padded three-digit integer.
- Published migrations are immutable.
- New schema changes use new migration files.
- Migration statements and their recorded version commit atomically.
- An unknown or malformed migration prevents server startup.
- Down migrations are intentionally outside the initial scope.

## Acceptance criteria

- [x] `go.mod` contains the selected SQLite driver at an exact resolved version.
- [x] `go.sum` exists and `go mod verify` succeeds.
- [x] Integration tests open a temporary file-backed database.
- [x] Foreign-key enforcement is enabled and tested.
- [x] WAL mode is enabled and tested.
- [x] The busy timeout is configured and tested.
- [x] Pending embedded migrations execute in version order.
- [x] Repeated migration execution makes no schema changes.
- [x] Failed migrations roll back their schema and version record.
- [x] The server accepts a database path through `--database`.
- [x] The database closes during graceful server shutdown.
- [x] Standard milestone checks pass.
- [x] Local CI checks pass.
- [x] GitHub Actions passes on `main`.

## Verification

```bash
go mod verify && ./scripts/check_milestone.sh
```

Run the server with an explicit development database:

```bash
go run ./cmd/server \
  --addr 127.0.0.1:8080 \
  --database ./data/mediaarchive.db
```

## Out of scope

- Media repository CRUD operations
- User and permission tables
- PostgreSQL
- Down migrations
- Online schema migration while the server is running
- Application-level encryption at rest
- Backup and restore automation
