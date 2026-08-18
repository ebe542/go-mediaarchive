# Milestone 4: User identity foundation

## Goal

Introduce persistent user identities and global roles before media ownership and
authentication are implemented. The resulting model provides valid foreign-key
targets for future media records without prematurely coupling identity to a
password or token mechanism.

## Scope

- Define the user identity domain model.
- Define the roles `viewer`, `editor`, and `admin`.
- Validate usernames, display names, and roles in Go.
- Add migration `002_create_users.sql`.
- Enforce identity invariants with SQLite constraints.
- Implement a SQLite-backed user repository.
- Create users with caller-supplied stable IDs.
- Retrieve users by ID and normalized username.
- Distinguish missing users from storage failures.
- Reject duplicate IDs and usernames.
- Preserve creation timestamps during updates.
- Support activation and deactivation without deleting identities.
- Test repository behavior against temporary SQLite databases.

## Domain model

A user identity contains:

| Field | Purpose |
| --- | --- |
| `id` | Stable UUID represented as lowercase text |
| `username` | Unique normalized login name |
| `displayName` | Human-readable name |
| `role` | Global role: `viewer`, `editor`, or `admin` |
| `active` | Whether the identity may be used |
| `createdAt` | Immutable UTC creation timestamp |
| `updatedAt` | UTC timestamp of the latest change |

Usernames use lowercase ASCII letters, digits, hyphens, and underscores. They
start with a letter or digit and contain between 3 and 32 characters.

## Role semantics

| Role | Intended capability |
| --- | --- |
| `viewer` | View permitted metadata; download requires separate permission |
| `editor` | Create media and manage permitted or owned records |
| `admin` | Manage identities, policies, and audit data |

Roles do not grant content download permission by themselves. Object-level
authorization remains a later milestone.

## Persistence model

Migration `002_create_users.sql` creates a `users` table with:

- a primary key on `id`;
- a case-normalized unique username;
- a role check constraint;
- an active-state check constraint;
- UTC timestamps;
- no password, token, or certificate fields.

Authentication credentials will be modeled separately so that mTLS devices,
passwords, and future authentication methods do not become properties of the
identity record itself.

## Repository contract

The initial repository operations are:

```go
Create(ctx context.Context, user identity.User) error
FindByID(ctx context.Context, id string) (identity.User, error)
FindByUsername(ctx context.Context, username string) (identity.User, error)
Update(ctx context.Context, user identity.User) error
```

The repository returns a stable domain-level not-found error rather than leaking
`sql.ErrNoRows` to callers.

## Acceptance criteria

- [x] Domain tests cover every valid role.
- [x] Domain tests reject invalid usernames, display names, and roles.
- [x] Migration `002_create_users.sql` applies after the baseline migration.
- [x] SQLite constraints reject invalid persisted roles and active states.
- [x] Repository tests create and retrieve a user by ID.
- [x] Repository tests retrieve usernames case-insensitively after normalization.
- [x] Duplicate IDs and usernames return recognizable conflict errors.
- [x] Missing users return a recognizable not-found error.
- [x] Updates preserve `id` and `createdAt`.
- [x] Deactivated identities remain retrievable.
- [x] Standard milestone checks pass.
- [x] Local CI checks pass.
- [ ] GitHub Actions passes on `main`.

## Verification

```bash
./scripts/check_ci.sh
```

## Out of scope

- Password hashing and password storage
- Login and access tokens
- mTLS client certificates
- Per-media permissions
- Media tables and repositories
- User deletion
- Audit logging
