# Milestone 9: Role-based authorization foundation

## Goal

Turn a valid server-side session into an authenticated request identity and
introduce reusable role checks. Prove the authorization boundary with a
self-service identity endpoint and a read-only administrator endpoint.

## Scope

- Parse bearer credentials from the `Authorization` request header.
- Resolve active sessions through the application session service.
- Store the authenticated user in the request context with a private typed key.
- Distinguish missing authentication from insufficient authorization.
- Add a reusable role-authorization HTTP wrapper.
- Add `GET /api/v1/users/me` for every authenticated role.
- Add `GET /api/v1/users/{id}` for administrators only.
- Load the current user state and role for every authenticated request.
- Keep health checks and session creation public.
- Keep user creation, modification, and activation changes unavailable over HTTP.

## Authentication boundary

Protected endpoints accept exactly one bearer credential:

```http
Authorization: Bearer opaque-session-token
```

Missing, malformed, unknown, expired, idle, revoked, and deactivated-user
sessions produce the same `401 Unauthorized` response. The response includes a
`WWW-Authenticate: Bearer` challenge and does not reveal why authentication
failed.

An unexpected session storage or application failure produces `500 Internal
Server Error`. Tokens are never included in response bodies or logs.

## Authorization boundary

Authentication establishes who is making a request. Authorization separately
decides whether that authenticated user may perform an operation.

The first role rule is:

| Operation | `viewer` | `editor` | `admin` |
| --- | --- | --- | --- |
| Read own identity | Allow | Allow | Allow |
| Read another user by ID | Deny | Deny | Allow |

An authenticated user without a permitted role receives `403 Forbidden`.
Authentication failures remain `401 Unauthorized`.

Global roles do not grant access to licensed media content. Metadata visibility,
content downloads, ownership, and object-level permissions remain separate
authorization decisions for later milestones.

## API contract

Read the current identity:

```http
GET /api/v1/users/me
Authorization: Bearer opaque-session-token
Accept: application/json
```

Successful response:

```http
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
Cache-Control: no-store

{
  "id": "3f74e74d-e237-4bd4-a9bb-3407c38dd16f",
  "username": "archive_admin",
  "displayName": "Archive Administrator",
  "role": "admin",
  "active": true
}
```

Read a user as an administrator:

```http
GET /api/v1/users/3f74e74d-e237-4bd4-a9bb-3407c38dd16f
Authorization: Bearer opaque-session-token
Accept: application/json
```

The successful response uses the same public identity representation as the
self-service endpoint. An unknown user ID returns `404 Not Found` without
exposing storage details.

## Request context

Authentication middleware resolves the bearer token before the protected
handler runs. It attaches the complete current `identity.User` value to a
derived request context. Context access uses a package-private key type so
unrelated packages cannot overwrite the authenticated identity accidentally.

Handlers retrieve the authenticated identity through an explicit API helper.
They do not parse tokens, query sessions, or trust caller-supplied identity or
role fields.

## Current role state

Session resolution loads the user for every authenticated request. Role and
activation changes therefore take effect without issuing a new token. A
deactivated user cannot continue using an otherwise valid session.

## Error contract

| Condition | Status | Error code |
| --- | --- | --- |
| Authentication required or invalid | `401` | `authentication_required` |
| Authenticated role is insufficient | `403` | `forbidden` |
| Requested user does not exist | `404` | `not_found` |
| Unexpected application failure | `500` | `internal_error` |

All error responses use the existing versioned JSON error shape and avoid
sensitive details.

## Acceptance criteria

- [x] Bearer credentials are parsed once for protected requests.
- [x] Malformed and unusable credentials share one `401` response.
- [x] Authentication challenges include `WWW-Authenticate: Bearer`.
- [x] Authenticated users are available through a typed request context.
- [x] Current database role and activation state apply to every request.
- [x] Every active role can read its own identity.
- [x] Only administrators can read another user by ID.
- [x] Insufficient roles receive `403` rather than `401`.
- [x] Unknown users receive the documented `404` response.
- [x] Successful identity responses use `Cache-Control: no-store`.
- [x] Tokens are absent from response bodies, errors, and logs.
- [x] Public health and login behavior remains unchanged.
- [x] No user mutation endpoint is registered.
- [x] Global roles do not imply media download permission.
- [x] Standard milestone checks pass.
- [x] Local CI checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-009` tag; it is verified after the final milestone commit.

## Verification

Git Bash:

```bash
./scripts/check_ci.sh
```

Windows PowerShell:

```powershell
.\scripts\check_ci.ps1
```

## Planned commits

1. `feat(api): add authenticated request context`
2. `feat(api): add role authorization guards`
3. `feat(api): add protected user read endpoints`

## Out of scope

- User creation, modification, and activation endpoints
- Password changes and resets
- Media ownership and object-level permissions
- Metadata visibility rules
- Content download authorization
- Audit logging
- Distributed session storage or rate limiting
- Trusted reverse-proxy headers
- Mutual TLS client identity
