# Milestone 5: User application service

## Goal

Introduce a storage-independent application service for user administration.
The service coordinates domain validation, identifier generation, timestamps,
and persistence without exposing administrative operations over HTTP before
authentication and authorization exist.

## Scope

- Define a storage-independent user repository contract.
- Implement a user application service.
- Create users from validated application input.
- Generate stable user IDs through an injected dependency.
- Obtain timestamps through an injected clock.
- Retrieve users by ID and normalized username.
- Update usernames, display names, and global roles.
- Activate and deactivate identities without deleting them.
- Preserve user IDs and creation timestamps during updates.
- Propagate recognizable not-found and conflict errors.
- Test service behavior with an in-memory repository test double.

## Architecture boundary

The application service depends on the identity domain and a repository
interface. It does not depend on SQLite, HTTP, JSON, CLI parsing, passwords,
tokens, or certificates.

```text
future HTTP or CLI adapter
          |
          v
 user application service
          |
          v
 identity repository contract
          |
          v
 SQLite repository
```

The repository contract belongs to the identity-facing application boundary.
SQLite remains an implementation detail behind that contract.

## Service operations

The initial service operations are:

```go
CreateUser(ctx context.Context, input CreateUserInput) (identity.User, error)
UserByID(ctx context.Context, id string) (identity.User, error)
UserByUsername(ctx context.Context, username string) (identity.User, error)
UpdateUser(ctx context.Context, id string, input UpdateUserInput) (identity.User, error)
SetUserActive(ctx context.Context, id string, active bool) (identity.User, error)
```

Inputs contain only caller-controlled mutable values. Callers cannot supply or
replace creation timestamps through update operations.

## Testable dependencies

Identifier generation and time access are injected into the service. Tests use
deterministic implementations, while production wiring may use UUID generation
and the system clock.

The service must reject an empty or invalid generated identifier and a zero
timestamp through the existing domain validation rules.

## Security boundary

This milestone does not expose user administration through the REST API. A
global role stored on a user record is authorization data, but it does not prove
that a request was authenticated as that user.

Role enforcement at an external boundary will be introduced only after a
trusted authenticated actor is available. Until then, the service is internal
application functionality and must not be registered as a public handler.

## Acceptance criteria

- [x] A repository interface removes the application's dependency on SQLite.
- [x] The SQLite repository satisfies the interface at compile time.
- [x] Service tests use a deterministic identifier generator and clock.
- [x] Creating a user validates input and persists the resulting identity.
- [x] Users can be retrieved by ID and normalized username.
- [x] Updates preserve `id` and `createdAt`.
- [x] Activation and deactivation preserve the user record.
- [x] Missing users and persistence conflicts remain recognizable.
- [x] No user-administration HTTP routes are registered.
- [x] Standard milestone checks pass.
- [x] Local CI checks pass.
- [ ] GitHub Actions passes on `main`.

## Verification

```bash
./scripts/check_ci.sh
```

## Out of scope

- Passwords and credential storage
- Initial administrator bootstrap
- Login and access tokens
- HTTP user-management endpoints
- CLI user-management commands
- mTLS client certificates
- Media ownership and permissions
- User deletion
- Audit logging
