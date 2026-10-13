# Milestone 21: Security Audit Logging

## Goal

Create a persistent, append-only security audit trail for authentication,
identity, media, authorization, and licensed-content operations. Administrators
can inspect filtered, paginated events without gaining additional access to
media content or sensitive credentials.

## Scope

- Define a bounded, storage-independent audit event model.
- Persist immutable audit events in SQLite.
- Reject application-level updates and deletions of audit rows.
- Record security-relevant identity, password, media, grant, and content events.
- Fail closed when a required audit event cannot be persisted.
- Preserve actor and target snapshots after identities or media are deleted.
- Add an administrator-only, filtered, paginated audit directory.
- Add typed client operations for the audit directory.
- Keep raw secrets, content, paths, and unrestricted error text out of events.
- Document operational limits and the distinction between append-only storage
  and externally tamper-evident auditing.

Audit export, retention deletion, cryptographic log chaining, external security
information and event management integration, and administrator-console audit
commands remain outside this milestone.

## Security properties

### Required and fail closed

A security-sensitive operation must not report success or release protected
content unless its required audit event has been stored. An audit persistence
failure therefore becomes an internal operational failure for:

- session creation and revocation;
- user creation, update, activation, deactivation, and deletion;
- password enrollment and password replacement;
- media upload and deletion;
- media grant replacement and revocation;
- complete and partial managed-content access;
- administrator audit-directory access.

Where a business change and audit event share SQLite, both belong to one
transaction. Filesystem operations continue to use the existing staged or
compensating workflow around that transaction. No successful database mutation
may commit without its corresponding audit row.

Denied operations also require an audit event before their safe public response
is returned. If that event cannot be stored, the API returns a generic internal
error rather than silently producing an unaudited denial.

### Append-only, not tamper-proof

The application exposes only event creation and querying. It provides no update
or delete repository operation. SQLite triggers reject `UPDATE` and `DELETE`
against the audit table, including accidental changes made through application
database access.

These controls do not protect against an operator who can replace or directly
modify the SQLite database file, executable, or migration state. Cryptographic
chaining, signed checkpoints, write-once storage, replication, and external
audit collection are separate future controls.

### Anti-enumeration boundary

Public API responses keep their existing masking. An administrator reading the
audit directory may see a stable internal reason code that distinguishes, for
example, an unknown medium, missing permission, missing content location, or
missing managed file. These reason codes never appear in the protected
operation's public response.

Audit records contain no raw Go error strings. Only an allowlisted reason code
may describe a denial or failure.

## Event model

Each event contains:

| Field | Meaning |
| --- | --- |
| `id` | Server-generated canonical UUID |
| `occurredAt` | Server-controlled UTC timestamp |
| `type` | Stable allowlisted event type |
| `outcome` | `success`, `denied`, or `failure` |
| `actorId` | Identity at the time of the action, empty for an anonymous attempt |
| `actorUsername` | Bounded username snapshot |
| `actorRole` | Bounded role snapshot |
| `targetType` | Stable target category such as `user`, `media`, or `session` |
| `targetId` | Bounded target identity when one is known |
| `targetName` | Optional bounded display snapshot needed after deletion |
| `reason` | Optional allowlisted internal reason code |
| `rangeStart` | Optional first selected content byte |
| `rangeEnd` | Optional last selected content byte |
| `rangeTotal` | Optional complete representation size |

Actor and target fields are snapshots rather than foreign keys. Deleting a user
or medium must not delete or invalidate its previous audit history. Anonymous
authentication failures may retain a validated, bounded username as the target
name, but malformed or oversized input is never copied into an event.

The first schema deliberately avoids an unrestricted JSON details field. New
security-relevant attributes require an explicit domain and migration change so
arbitrary secrets cannot be attached by convenience.

## Event types

Initial stable event types are:

| Area | Event types |
| --- | --- |
| Sessions | `session.created`, `session.create_denied`, `session.revoked` |
| Users | `user.created`, `user.updated`, `user.activated`, `user.deactivated`, `user.deleted` |
| Passwords | `password.enrollment_issued`, `password.enrollment_completed`, `password.changed` |
| Media | `media.uploaded`, `media.deleted` |
| Grants | `media.grant_replaced`, `media.grant_revoked` |
| Content | `media.content_read` |
| Audit | `audit.events_listed` |

An event type is independent from its outcome. For example,
`session.create_denied` records the generic authentication denial, while
`media.content_read` may have `success`, `denied`, or `failure` outcomes.
Additional event types require tests and documentation before use.

## Content-access semantics

The first successful `GET` or `HEAD` for one medium in an authenticated login
session creates one `media.content_read` event. The event is stored after
authorization, content-location lookup, file opening, and range validation but
before response headers or bytes are committed. Later complete or ranged
requests for the same medium and login session remain fully authorized but do
not create duplicate success events.

SQLite stores a technical marker keyed by the hashed session token, media ID,
outcome, and reason. The marker and its first audit event share one transaction.
Markers may be removed with their session; immutable audit history remains.
Raw bearer tokens are never persisted.

The first masked denial and the first technical content failure for the same
session, medium, outcome, and reason are recorded once. Malformed media IDs and
invalid or unsatisfiable ranges remain transport validation results rather than
permanent audit events. This prevents Range traffic or deliberate invalid
requests from growing the immutable audit history without bound.

Range fields remain reserved in the initial audit schema but are not populated
by session-level content events. Individual byte selections, transfer counts,
client disconnects, and response-writer failures are operational telemetry.

## Transaction coordination

Audit recording belongs as close as possible to the protected state change:

1. Validate the request and authorize the actor.
2. Begin the business transaction when persistence is required.
3. Apply the business mutation.
4. Insert the audit event through the same transaction.
5. Commit once.
6. Complete any already-staged filesystem operation.

For read-only content access, the service prepares the authorized open handle
and validated range first. The audit event is inserted before the handler writes
successful response headers. An audit failure closes the handle and produces no
content bytes.

Compensation and staged-deletion failures receive allowlisted failure events
when the database remains available. The original operational error is never
stored verbatim.

## Audit directory

The API exposes one read-only endpoint:

| Method | Path | Access |
| --- | --- | --- |
| `GET` | `/api/v1/audit-events` | Active administrator |

Supported optional filters are:

- `from` inclusive and `to` exclusive UTC timestamps;
- `actorId`;
- exact event `type`;
- exact `outcome`;
- `limit` and opaque continuation `cursor`.

Results use a stable descending `(occurredAt, id)` order so newly appended
events do not duplicate or skip entries inside an existing cursor sequence. The
cursor binds the active filters and limit and reveals no raw database offset.
Continuation requests repeat the same filters and limit together with the
cursor. The default limit is 50 and the maximum is 100.
Malformed filters or cursors return `400 Bad Request`. Missing authentication
returns `401 Unauthorized`; a non-administrator receives `403 Forbidden`.

Listing audit events is itself audited as `audit.events_listed`. The access
event is stored before the query executes and may therefore appear in the first
page when it matches the selected filters.

There is no REST operation for creating, replacing, or deleting audit events.

## Sensitive-data exclusions

Audit events must never contain:

- passwords, password hashes, or enrollment tokens;
- bearer tokens or session-token hashes;
- Authorization, Cookie, or other credential headers;
- request or response bodies;
- media bytes, checksums used as secrets, or extracted document text;
- storage keys, absolute paths, database connection strings, or private keys;
- unrestricted error messages or stack traces.

This milestone does not persist client IP addresses or forwarded-address
headers. Network-source auditing requires a separate privacy, trusted-proxy,
retention, and disclosure policy.

## Typed client

The typed client lists audit events with validated filter and pagination input.
It returns typed event snapshots, UTC timestamps, optional range metadata, and
the next opaque cursor. API error bodies retain the existing bounded JSON
handling. The client never interprets internal reason codes as authorization
decisions for another operation.

Caller-provided time values are encoded as UTC. Event types, outcomes, actor
IDs, limits, time bounds, complete range triples, event ordering, and returned
event snapshots are validated before use. Cursor contents remain a server
implementation detail; the client validates only their bounded URL-safe
transport representation.

The administrator console remains unchanged in this milestone. A future console
command can build on the typed directory operation without duplicating HTTP or
cursor validation.

## Implementation sequence

1. Define audit contracts, event types, outcomes, and validation.
2. Add append-only SQLite persistence and protective triggers.
3. Record authentication, user, and password events transactionally.
4. Record media, grant, upload, and deletion events transactionally.
5. Record complete, partial, denied, and failed content requests.
6. Add the filtered administrator audit directory and opaque pagination.
7. Wire the real service into the server and add typed client support.
8. Complete public documentation and quality gates.

Large integration areas remain separate runnable Conventional Commits. Tests
stay in the same commit as the behavior they specify.

## Acceptance criteria

- [x] Audit event fields, types, outcomes, and exclusions are documented.
- [x] Audit events validate bounded allowlisted data and UTC timestamps.
- [x] SQLite audit rows reject application updates and deletions.
- [x] Audit history survives user and media deletion.
- [ ] Required audit failures prevent security-sensitive success.
- [x] Authentication, user, and password operations create safe events.
- [x] Media, grant, upload, and deletion operations create safe events.
- [x] Each login session records content access and outcomes once per medium.
- [x] Public authorization and availability masking remains unchanged.
- [x] Administrators can filter and page through immutable audit events.
- [x] Audit-directory access is itself audited.
- [x] The typed client validates audit filters, events, and cursors.
- [ ] No audit event contains prohibited secrets, content, paths, or raw errors.
- [ ] Standard project checks pass.
- [ ] Local quality gate checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-021` tag after all milestone commits are complete.

## Verification

```console
go run ./cmd/projectctl quality-gate
```

## Out of scope

- Audit-event update or deletion APIs
- Retention deletion and archival policy
- Cryptographic hash chains or signed checkpoints
- External SIEM, syslog, or remote append-only storage
- Source-IP persistence and trusted-proxy handling
- Free-form event metadata
- Logical browser viewing-session aggregation
- Transfer-completion telemetry after response commitment
- Audit export formats
- Administrator-console audit commands
- Explicit persistent media download
- Media search and discovery
- MFA
- mTLS client authentication
- PostgreSQL persistence
