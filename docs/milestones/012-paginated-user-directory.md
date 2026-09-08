# Milestone 12: Paginated user directory

## Goal

Allow administrators to traverse the user directory through a bounded,
deterministic REST API without exposing authentication material or relying on
offset pagination.

## Scope

- Add a storage-independent user-page application contract.
- Retrieve users in immutable `created_at`, `id` order.
- Use keyset pagination with an opaque continuation cursor.
- Restrict the HTTP endpoint to administrators.
- Apply a default page size of 50 and a maximum of 100.
- Return the public user representation without credentials or sessions.
- Reject malformed, duplicated, or unknown query parameters.

## API contract

An administrator requests the first page:

```http
GET /api/v1/users?limit=50
Authorization: Bearer admin-session-token
Accept: application/json
```

The response is `200 OK`, uses `Cache-Control: no-store`, and contains a cursor
only when another page exists:

```json
{
  "users": [
    {
      "id": "3f74e74d-e237-4bd4-a9bb-3407c38dd16f",
      "username": "archive_reader",
      "displayName": "Archive Reader",
      "role": "viewer",
      "active": true,
      "createdAt": "2026-08-29T10:00:00Z",
      "updatedAt": "2026-08-29T10:00:00Z"
    }
  ],
  "nextCursor": "opaque-value"
}
```

The next request supplies that cursor unchanged:

```http
GET /api/v1/users?limit=50&cursor=opaque-value
Authorization: Bearer admin-session-token
Accept: application/json
```

## Pagination contract

- The default limit is 50, the minimum is 1, and the maximum is 100.
- Ordering is always `created_at ASC, id ASC`.
- The repository loads `limit + 1` records to detect another page.
- The next cursor identifies the last user returned to the client.
- The cursor internally contains a canonical UTC RFC 3339 Nano creation time
  and canonical user UUID encoded as unpadded URL-safe Base64 JSON.
- The cursor is opaque but not secret and grants no authority.
- A malformed cursor returns `400 invalid_request`.
- `nextCursor` is omitted on the final page.
- No total count is calculated.

Pages requested at different times do not share a database snapshot. New users
whose ordering key follows the cursor may appear on later pages. Existing user
updates do not move entries because creation time and ID are immutable.

## Error contract

| Condition | Status | Error code |
| --- | --- | --- |
| Invalid authentication | `401` | `authentication_required` |
| Insufficient role | `403` | `forbidden` |
| Invalid query or cursor | `400` | `invalid_request` |
| Unexpected application failure | `500` | `internal_error` |

## Implementation sequence

1. Add the application page contract and SQLite keyset query.
2. Add cursor encoding, the administrator endpoint, and production wiring.

Each step is delivered as a complete Conventional Commit. Acceptance criteria
are checked continuously only after implementation and tests prove them.

## Acceptance criteria

- [x] Page limits are validated with a default of 50 and maximum of 100.
- [x] Users are ordered by immutable creation time and ID.
- [x] Repository pagination uses a keyset and loads one lookahead record.
- [x] The application returns a next cursor only when another page exists.
- [x] Only administrators can list users.
- [x] Cursor input is strictly decoded and validated.
- [x] Unknown and duplicated query parameters are rejected.
- [x] User responses contain no credential or session data.
- [x] Sensitive responses use `Cache-Control: no-store`.
- [x] Standard milestone checks pass.
- [x] Local CI checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-012` tag; it is verified after the final milestone commit.

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

- User deletion or anonymization
- Filtering and search
- Total user counts
- Snapshot pagination across requests
- Media ownership and object-level permissions
- Audit logging
