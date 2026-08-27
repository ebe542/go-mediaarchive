# Milestone 10: Administrator user management

## Goal

Allow authenticated administrators to create and maintain user identities over
the versioned JSON API without coupling identity management to passwords or
licensed media permissions.

## Scope

- Add administrator-only endpoints for creating and updating users.
- Add an administrator-only endpoint for activation changes.
- Reuse strict, bounded JSON request decoding.
- Preserve the distinction between identity records and credentials.
- Prevent administrators from removing their own access.
- Prevent removal of the last active administrator atomically.
- Keep user deletion unavailable.
- Keep password management unavailable.

## API contract

Create a user identity:

```http
POST /api/v1/users
Authorization: Bearer admin-session-token
Content-Type: application/json
Accept: application/json

{
  "username": "archive_editor",
  "displayName": "Archive Editor",
  "role": "editor"
}
```

A successful request returns `201 Created`, a `Location` header containing the
new user resource, `Cache-Control: no-store`, and the public user JSON
representation.

Replace all mutable identity fields:

```http
PUT /api/v1/users/3f74e74d-e237-4bd4-a9bb-3407c38dd16f
Authorization: Bearer admin-session-token
Content-Type: application/json

{
  "username": "updated_editor",
  "displayName": "Updated Editor",
  "role": "editor"
}
```

Change activation state:

```http
PUT /api/v1/users/3f74e74d-e237-4bd4-a9bb-3407c38dd16f/active
Authorization: Bearer admin-session-token
Content-Type: application/json

{
  "active": false
}
```

Update and activation operations return `200 OK` with the updated public user
representation. Setting the existing activation state is idempotent.

## Credential separation

Creating a user creates only an identity. It does not accept or generate a
password credential. A credential-less user cannot authenticate and receives
the same generic authentication failure as an unknown user.

Password provisioning and password changes remain a separate security
milestone. Passwords and password hashes are never included in user responses.

## Authorization

Only the global `admin` role permits identity mutation. Authenticated viewers
and editors receive `403 Forbidden` before the user application service is
called. Missing or unusable sessions receive the existing generic `401
Unauthorized` response.

Global roles remain independent from media visibility and download permission.
Administrative identity management does not grant access to licensed content.

## Administrator safety

An administrator cannot deactivate their own identity or replace their own
`admin` role. These operations return `409 Conflict` with the error code
`self_lockout`.

An update that would leave no active administrator returns `409 Conflict` with
the error code `last_administrator`. The count and update occur in one database
transaction so concurrent HTTP requests cannot independently remove the final
administrator.

## Request validation

Mutation endpoints accept only bounded `application/json` request bodies.
Media-type parameters such as `charset=utf-8` are allowed. Requests reject:

- missing or unsupported content types;
- malformed JSON;
- unknown JSON fields;
- multiple JSON values;
- bodies larger than 64 KiB;
- missing required fields;
- invalid usernames, display names, roles, or user IDs.

## Error contract

| Condition | Status | Error code |
| --- | --- | --- |
| Invalid authentication | `401` | `authentication_required` |
| Insufficient role | `403` | `forbidden` |
| Invalid request or domain value | `400` | `invalid_request` |
| User not found | `404` | `not_found` |
| Duplicate username or ID | `409` | `conflict` |
| Administrator removes own access | `409` | `self_lockout` |
| Last active administrator would be removed | `409` | `last_administrator` |
| Unexpected application failure | `500` | `internal_error` |

Error bodies do not expose SQL, repository, credential, or token details.

## Acceptance criteria

- [x] Strict JSON request decoding is shared by login and user mutation APIs.
- [x] Only administrators can invoke user mutation operations.
- [x] Administrators can create credential-less user identities.
- [x] Create responses include `201`, `Location`, and `Cache-Control: no-store`.
- [x] Administrators can replace all mutable identity fields.
- [x] Administrators can idempotently change activation state.
- [x] Invalid domain values return the generic `400` contract.
- [x] Missing users return the generic `404` contract.
- [x] Identity conflicts return the generic `409` contract.
- [x] Administrators cannot demote or deactivate themselves.
- [x] The last active administrator is protected atomically.
- [x] Passwords and credentials are absent from requests and responses.
- [x] User deletion remains unavailable.
- [x] Media permissions remain independent from global roles.
- [x] Standard milestone checks pass.
- [x] Local CI checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-010` tag; it is verified after the final milestone commit.

## Verification

Git Bash:

```bash
./scripts/check_ci.sh
```

Windows PowerShell:

```powershell
.\scripts\check_ci.ps1
```

## Implementation sequence

1. Share strict JSON request decoding.
2. Add administrator-only user creation.
3. Add administrator-only user updates.
4. Add safe and idempotent activation management.

The HTTP, application, and transactional safety changes are delivered as one
cohesive implementation commit because they form one authorization boundary.
The completed documentation is included in that commit.

## Out of scope

- Password provisioning, changes, and resets
- User deletion
- User listing and pagination
- Media ownership and object-level permissions
- Metadata visibility and content downloads
- Audit logging
- Multi-factor authentication
- Mutual TLS client identity
