# Milestone 8: Authentication sessions

## Goal

Authenticate active users over the versioned JSON API and issue opaque,
server-side sessions that can expire and be revoked. Administrative user
operations remain unavailable until authenticated authorization is established.

## Scope

- Load password credentials by normalized username.
- Verify Argon2id password hashes for active users.
- Use indistinguishable failures for unknown users, wrong passwords, and
  deactivated users.
- Add migration `004_create_sessions.sql`.
- Generate opaque session tokens from 32 cryptographically random bytes.
- Return the raw token only once and persist only its SHA-256 hash.
- Enforce a 30-minute idle timeout and an eight-hour absolute timeout.
- Resolve active sessions from bearer tokens.
- Revoke the current session on logout.
- Add versioned JSON login and logout endpoints.
- Limit failed login attempts independently by normalized username and source IP.
- Keep all user-administration endpoints unavailable.

## API contract

Create a session:

```http
POST /api/v1/auth/sessions
Content-Type: application/json
Accept: application/json

{
  "username": "archive-admin",
  "password": "a user-supplied passphrase"
}
```

Successful response:

```http
HTTP/1.1 201 Created
Content-Type: application/json; charset=utf-8
Cache-Control: no-store

{
  "accessToken": "opaque-token-value",
  "tokenType": "Bearer",
  "expiresAt": "2026-08-19T18:00:00Z"
}
```

Revoke the current session:

```http
DELETE /api/v1/auth/sessions/current
Authorization: Bearer opaque-token-value
Accept: application/json
```

Successful logout returns `204 No Content`. Session tokens are never accepted in
URLs or query parameters.

## Authentication failure behavior

Unknown usernames, incorrect passwords, deactivated identities, and missing
credentials return the same status and JSON error contract. The implementation
performs an Argon2id verification even when no usable credential exists to
reduce timing differences.

Malformed requests remain distinguishable from authentication failure because
they do not reveal whether an account exists. Request bodies have a strict size
limit, reject unknown JSON fields, and contain exactly one JSON value.

## Login throttling

Failed login attempts use two independent in-memory limits:

- five failures per normalized username within 15 minutes;
- twenty failures per source IP within 15 minutes.

Both limits must permit an attempt. A limit response uses a generic `429 Too Many
Requests` body without identifying the triggering bucket or exposing an exact
retry schedule. Successful authentication clears the username failure state.

The initial limiter is intentionally process-local. Distributed rate limiting
is deferred until multiple server instances become a project requirement.
Reverse-proxy client IP headers are not trusted in this milestone.

## Session tokens

Tokens contain 256 bits of cryptographically secure randomness and use unpadded
URL-safe Base64 for transport. Tokens carry no user ID, role, timestamps, or
other application data.

The database stores `SHA-256(token)` as a fixed-length binary value. Possession
of a raw token grants the associated session authority, so raw tokens are never
logged, stored in plaintext, or returned again after session creation.

## Session lifetime

Every session has:

- a 30-minute server-enforced idle timeout;
- an eight-hour server-enforced absolute expiration;
- an optional revocation timestamp.

A session is valid only when the user remains active and both timeouts remain
unexpired. Logout records revocation server-side. Expired and revoked sessions
produce the same unauthenticated response.

## Security headers and transport

Authentication responses use `Cache-Control: no-store`. Network deployments are
already required to use TLS 1.3 by Milestone 7. Plain HTTP authentication remains
possible only on an IP loopback listener for local development.

## Acceptance criteria

- [ ] Credential lookup normalizes usernames and preserves not-found semantics.
- [ ] Unknown, incorrect, inactive, and credential-less users share one failure.
- [ ] Missing-user authentication performs a dummy Argon2id verification.
- [ ] Session tokens contain 256 bits of cryptographic randomness.
- [ ] Only SHA-256 token hashes are persisted.
- [ ] Migration `004_create_sessions.sql` enforces user and token constraints.
- [ ] Idle and absolute expiration are enforced server-side.
- [ ] Logout revokes the current session and is idempotent.
- [ ] Username and source-IP login limits operate independently.
- [ ] Login accepts only bounded, strict JSON requests.
- [ ] Login returns versioned JSON with `Cache-Control: no-store`.
- [ ] Authentication failures use a generic status and response body.
- [ ] Session tokens are absent from logs and error messages.
- [ ] No user-administration endpoints are registered.
- [ ] Standard milestone checks pass.
- [ ] Local CI checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-008` tag; it is verified after the final milestone commit.

## Verification

Git Bash:

```bash
./scripts/check_ci.sh
```

Windows PowerShell:

```powershell
.\scripts\check_ci.ps1
```

## Out of scope

- REST user administration
- Refresh tokens
- Browser cookies and CSRF protection
- Multi-factor authentication
- Password changes and resets
- Distributed rate limiting
- Trusted reverse-proxy headers
- Mutual TLS client certificates
- Media permissions
- Audit logging
