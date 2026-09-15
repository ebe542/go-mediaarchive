# Milestone 18: Media REST API

## Goal

Expose authenticated media metadata and per-user grant operations through the
versioned JSON REST API without duplicating application authorization rules in
HTTP handlers.

## Scope

- Add authenticated CRUD endpoints for media metadata.
- Add authenticated replacement, inspection, listing, and revocation endpoints
  for per-user media grants.
- Represent permission sets as stable permission-name arrays.
- Represent SHA-256 checksums as lowercase hexadecimal strings.
- Wire the media repositories and application services into the server.
- Add typed client operations for every new endpoint.
- Preserve indistinguishable not-found responses for unknown and unauthorized
  media.
- Keep interactive commands, search, file storage, upload, streaming, and
  download outside this milestone.

## Resource model

Media responses use the current minimal domain metadata:

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "title": "Security Engineering",
  "authors": ["Example Author"],
  "originalFilename": "security-engineering.pdf",
  "type": "book",
  "mimeType": "application/pdf",
  "size": 4096,
  "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "ownerId": "223e4567-e89b-12d3-a456-426614174000",
  "createdAt": "2026-09-13T10:00:00Z",
  "updatedAt": "2026-09-13T10:00:00Z"
}
```

Creation and replacement requests contain only caller-controlled fields:

```json
{
  "title": "Security Engineering",
  "authors": ["Example Author"],
  "originalFilename": "security-engineering.pdf",
  "type": "book",
  "mimeType": "application/pdf",
  "size": 4096,
  "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}
```

IDs, ownership, and lifecycle timestamps are supplied by the application
service. API timestamps use RFC 3339 in UTC. Presentation in the local time zone
remains a client responsibility.

Grant responses use external permission names rather than the compact database
bitmask:

```json
{
  "mediaId": "123e4567-e89b-12d3-a456-426614174000",
  "userId": "323e4567-e89b-12d3-a456-426614174000",
  "permissions": ["discover", "read", "download"]
}
```

The complete permission vocabulary is `discover`, `read`, `download`, `update`,
`delete`, and `share`. Requests reject empty arrays, duplicate names, combined
values, and unknown names.

## Endpoints

| Method | Path | Success | Application operation |
| --- | --- | --- | --- |
| `POST` | `/api/v1/media` | `201 Created` | Create media |
| `GET` | `/api/v1/media/{id}` | `200 OK` | Read metadata |
| `PUT` | `/api/v1/media/{id}` | `200 OK` | Replace metadata |
| `DELETE` | `/api/v1/media/{id}` | `204 No Content` | Delete media |
| `PUT` | `/api/v1/media/{id}/grants/{userId}` | `200 OK` | Replace grant |
| `GET` | `/api/v1/media/{id}/grants/{userId}` | `200 OK` | Inspect grant |
| `GET` | `/api/v1/media/{id}/grants` | `200 OK` | List grants |
| `DELETE` | `/api/v1/media/{id}/grants/{userId}` | `204 No Content` | Revoke grant |

Creation returns the created representation and a resource `Location` header.
Grant listing returns an object containing a `grants` array. Empty lists are
encoded as `[]`, not `null`.

## Authentication and authorization

Every endpoint requires a valid bearer session. The authentication middleware
resolves the complete active user and stores it in the request context. Handlers
pass that identity to the media application services.

HTTP handlers do not implement media authorization and do not add an
administrator role bypass. The application services remain responsible for:

- restricting creation to active editors and administrators;
- applying owner permissions;
- loading at most the requesting actor's explicit grant;
- enforcing `discover`, `update`, `delete`, or `share` as appropriate;
- denying administrators implicit access to another user's media.

An unknown media ID and a media item unavailable to the actor both produce the
same `404 Not Found` response. This preserves the anti-enumeration boundary at
the public API.

## Error mapping

| Condition | Status |
| --- | --- |
| Missing, malformed, expired, or revoked session | `401 Unauthorized` |
| Unknown or unauthorized media | `404 Not Found` |
| Unknown grant or grant recipient | `404 Not Found` |
| Malformed JSON, invalid metadata, checksum, or permission set | `400 Bad Request` |
| Forbidden global creation role | `403 Forbidden` |
| Owner grant or inactive grant recipient | `409 Conflict` |
| Repository or other unexpected operational failure | `500 Internal Server Error` |

Error responses use the existing JSON error format and do not expose grant
contents, credentials, database details, or filesystem paths.

## Server composition

The server creates one SQLite media repository, grant repository, and user
repository. Storage-independent metadata and grant services receive these
dependencies and are registered with the existing authenticated API handler.

Server composition tests verify that the real application handler exposes the
routes. Handler tests continue to use narrow recording services so HTTP behavior
and application authorization remain independently testable.

## Typed client

The internal HTTP client adds request and response types plus methods for all
eight endpoints. It continues to provide cancellation, timeouts, TLS trust,
bearer authentication, JSON content validation, and bounded response handling.

The typed client exposes server errors without reimplementing authorization.
Interactive `client` and `admin` console commands are not added here. Media
commands will accompany managed content storage so users cannot create a CLI
media entry that falsely implies a file was uploaded.

## Implementation sequence

1. Define routes, external representations, security boundaries, and errors.
2. Add authenticated metadata handlers and HTTP contract tests.
3. Add authenticated grant handlers and HTTP contract tests.
4. Wire SQLite media services into the executable server.
5. Add typed media and grant client operations.
6. Complete command and milestone documentation.

Each step is delivered as a complete Conventional Commit. Tests remain in the
same commit as the behavior they specify.

## Acceptance criteria

- [x] Media and grant routes and JSON representations are documented.
- [x] HTTP status and security error mappings are documented.
- [x] Interactive commands and file-content operations are explicitly excluded.
- [x] All metadata routes require authentication.
- [x] Metadata endpoints preserve application authorization and not-found masking.
- [x] Grant endpoints require application-level `share` authorization.
- [x] Permission names and hexadecimal checksums are strictly validated.
- [x] Empty grant lists are encoded as JSON arrays.
- [x] The executable server wires real SQLite-backed media services.
- [x] The typed client supports every media and grant endpoint.
- [ ] Standard project checks pass.
- [ ] Local quality gate checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-018` tag after all milestone commits are complete.

## Verification

```console
go run ./cmd/projectctl quality-gate
```

## Out of scope

- Interactive media commands
- Search, filtering, and pagination
- File upload and managed filesystem storage
- Content streaming and download
- HTTP range requests
- Ownership transfer
- Audit-log persistence
- mTLS client authentication
- PostgreSQL persistence
