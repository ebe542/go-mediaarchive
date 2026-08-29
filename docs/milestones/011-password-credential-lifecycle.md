# Milestone 11: Password credential lifecycle

## Goal

Allow credential-less users to choose an initial password through a secure,
single-use enrollment and allow authenticated users to change their own
password without exposing plaintext credentials or preserving old sessions.

## Scope

- Let administrators issue password enrollment tokens for existing users.
- Store only SHA-256 hashes of cryptographically random 256-bit tokens.
- Permit at most one current enrollment per user.
- Replace an existing enrollment atomically when a new token is issued.
- Let a user create an initial password with an unexpired enrollment token.
- Consume enrollment and create the password credential atomically.
- Let authenticated users change their own password after verifying the current
  password.
- Revoke all user sessions after a password change.
- Reuse the existing password policy and Argon2id hasher.

## Enrollment contract

An administrator issues or replaces an enrollment:

```http
POST /api/v1/users/3f74e74d-e237-4bd4-a9bb-3407c38dd16f/password-enrollment
Authorization: Bearer admin-session-token
Accept: application/json
```

The response is `201 Created`, contains `Cache-Control: no-store`, and returns
the plaintext token exactly once:

```json
{
  "token": "one-time-secret",
  "expiresAt": "2026-08-28T10:00:00Z"
}
```

Only the token's SHA-256 hash is stored. Issuing a new enrollment immediately
invalidates the previous token, including a token that has not expired. A user
with an existing password credential returns `409 Conflict` because enrollment
is not a password-reset mechanism.

The default enrollment lifetime is 24 hours. Runtime configuration follows the
project precedence rule:

```text
--password-enrollment-lifetime flag > MEDIAARCHIVE_PASSWORD_ENROLLMENT_LIFETIME environment variable > 24h default
```

## Initial password contract

A credential-less user submits the token and a self-selected password:

```http
POST /api/v1/auth/password-enrollments
Content-Type: application/json

{
  "token": "one-time-secret",
  "password": "user-selected-password"
}
```

Success returns `204 No Content`. Credential creation and token consumption
occur in one transaction. Unknown, expired, consumed, and replaced tokens all
return the same generic error. An inactive user may complete enrollment but
cannot authenticate until activated.

## Password change contract

An authenticated user changes only their own password:

```http
PUT /api/v1/users/me/password
Authorization: Bearer current-session-token
Content-Type: application/json

{
  "currentPassword": "old-password",
  "newPassword": "new-password"
}
```

Success returns `204 No Content`. The credential update and revocation of all
sessions, including the current session, occur atomically. The user must log in
again with the new password.

## Security rules

- Enrollment and password endpoints require TLS outside local development.
- Tokens contain 256 bits of cryptographically secure randomness.
- Plaintext tokens are returned once and never logged.
- Passwords and password hashes are never logged or returned.
- Passwords retain their exact byte representation during validation and
  hashing.
- Enrollment errors do not reveal token state.
- Unauthenticated enrollment attempts are limited by source IP without using
  attacker-controlled token values as limiter keys.
- Administrators cannot use enrollment to replace an existing credential.
- Password reset, recovery, email delivery, and MFA remain separate concerns.

## Error contract

| Condition | Status | Error code |
| --- | --- | --- |
| Invalid authentication | `401` | `authentication_required` |
| Invalid current password | `401` | `invalid_credentials` |
| Insufficient role | `403` | `forbidden` |
| Invalid request or password policy | `400` | `invalid_request` |
| Enrollment invalid or expired | `401` | `invalid_enrollment` |
| User not found during administration | `404` | `not_found` |
| Password credential already exists | `409` | `credential_exists` |
| Unexpected application failure | `500` | `internal_error` |

## Implementation sequence

1. Add the enrollment domain model, token generator, migration, and repository.
2. Add the storage-independent enrollment application service and atomic
   credential creation.
3. Add administrator issuance and initial password enrollment over HTTP.
4. Add authenticated password changes and atomic session revocation.

Each step is delivered as a complete Conventional Commit. Acceptance criteria
are checked continuously only after implementation and tests prove them.

## Acceptance criteria

- [x] Enrollment tokens contain 256 bits of secure randomness.
- [x] Only SHA-256 token hashes are persisted.
- [x] Enrollment timestamps and positive lifetimes are validated.
- [x] Reissuing an enrollment invalidates the previous token atomically.
- [x] Expired, unknown, replaced, and consumed tokens share one error contract.
- [x] Only administrators can issue enrollments.
- [x] Users with credentials cannot receive enrollment tokens.
- [x] Credential creation and enrollment consumption are atomic.
- [x] Inactive users may enroll but cannot authenticate.
- [x] Users must provide their current password before changing it.
- [x] Password changes atomically revoke all sessions.
- [x] Passwords, hashes, and plaintext tokens are never logged.
- [x] Enrollment attempts are limited without attacker-controlled limiter keys.
- [x] Standard milestone checks pass.
- [x] Local CI checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-011` tag; it is verified after the final milestone commit.

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

- Administrator password resets
- Password recovery by email or another external channel
- User deletion
- User listing and pagination
- Media ownership and object-level permissions
- Audit logging
- Multi-factor authentication
- Mutual TLS client identity
