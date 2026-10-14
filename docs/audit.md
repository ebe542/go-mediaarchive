# Security audit guide

Media Archive keeps a persistent security history for authentication, identity,
password, media, authorization, licensed-content, and audit-directory
operations. This guide explains what the history proves, how administrators can
query it, and which operational events deliberately remain outside it.

## Security objective

An audit event answers four bounded questions:

1. When did the operation occur?
2. Which authenticated identity performed it, when one was available?
3. Which stable operation and target were involved?
4. Did the operation succeed, get denied, or fail for an allowlisted reason?

The server creates the event ID, timestamp, actor snapshot, and target snapshot.
Clients cannot submit audit events through the REST API. Snapshots remain useful
after the corresponding user or medium has been deleted.

## Recorded operations

| Area | Event types |
| --- | --- |
| Sessions | `session.created`, `session.create_denied`, `session.revoked` |
| Users | `user.created`, `user.updated`, `user.activated`, `user.deactivated`, `user.deleted` |
| Passwords | `password.enrollment_issued`, `password.enrollment_completed`, `password.changed` |
| Media | `media.uploaded`, `media.deleted` |
| Grants | `media.grant_replaced`, `media.grant_revoked` |
| Content | `media.content_read` |
| Audit directory | `audit.events_listed` |

Outcomes are limited to `success`, `denied`, and `failure`. A denial or failure
may use only a reason from the domain allowlist, such as `invalid_credentials`,
`insufficient_role`, `missing_permission`, `unknown_target`,
`missing_managed_file`, `rate_limited`, or `operation_failure`. Raw Go errors
are never used as reasons.

## Fail-closed behavior

Successful security-sensitive state changes store their audit event in the same
SQLite transaction as the business change. A duplicate event ID, unavailable
database, or other audit-write failure rolls back both changes.

Managed files require additional coordination because file contents live
outside SQLite. Upload and deletion use staging or compensation around the
transaction. A cleanup or finalization failure uses a sanitized failure event
when the database remains available.

Read-only protected operations store their required event before releasing
protected results:

- managed content is not streamed when its required audit write fails;
- the audit directory is not queried when its own access event cannot be
  stored.

Audited business denials also fail closed. When the denial event cannot be
stored, the public denial is replaced by a generic internal error so the server
does not silently report an unaudited security decision.

## Denial and operational-log boundary

Permanent audit storage is intentionally not a record of every HTTP packet or
malformed request. The following events are not written to `audit_events`:

- missing, malformed, or unusable bearer authentication headers;
- HTTP role-middleware rejections before an audited application operation;
- malformed identifiers, query parameters, and byte-range syntax rejected at
  the transport boundary;
- individual content Range requests after the first matching session event;
- client disconnects, transferred byte counts, and response-writer failures.

This boundary prevents unauthenticated traffic and intentionally malformed
requests from amplifying writes into an immutable database. Bounded access
logs, metrics, reverse-proxy controls, intrusion detection, and an external
security information and event management system are the appropriate future
controls for those signals.

Credential verification failures, rate-limited login or enrollment attempts,
and application-level authorization denials remain security audit events.

## Content-access deduplication

The first successful content request for a medium in one authenticated login
session creates one `media.content_read` event. Later complete, `HEAD`, or Range
requests for the same medium and session do not create duplicate success
events. A new medium or a new login session creates a new event.

The first denial or technical failure is likewise recorded once per session,
medium, outcome, and reason. SQLite stores only the SHA-256 session-token hash
in a technical marker. Marker insertion and event insertion share one
transaction. Deleting the session may remove its markers, but never removes the
independent audit history.

## Sensitive-data exclusions

Audit events contain no fields for:

- plaintext passwords or password hashes;
- enrollment tokens, bearer tokens, or session-token hashes;
- authorization or cookie headers;
- request or response bodies;
- media bytes or extracted document text;
- checksums used as secrets;
- storage keys, temporary names, or filesystem paths;
- database connection strings, private keys, stack traces, or raw errors;
- unrestricted JSON metadata.

Usernames, roles, media titles, stable IDs, event types, outcomes, and
allowlisted reasons are bounded snapshots. Dedicated contract tests fix both
the Go event fields and SQLite audit columns so adding unrestricted data
requires an explicit security review.

## Administrator directory

Only an active administrator may request:

```http
GET /api/v1/audit-events
```

Supported optional query parameters are:

| Parameter | Meaning |
| --- | --- |
| `from` | Inclusive canonical UTC timestamp |
| `to` | Exclusive canonical UTC timestamp |
| `actorId` | Exact canonical actor UUID |
| `type` | Exact allowlisted event type |
| `outcome` | `success`, `denied`, or `failure` |
| `limit` | Page size from 1 through 100; default 50 |
| `cursor` | Opaque continuation value returned by the server |

Example request:

```http
GET /api/v1/audit-events?type=media.content_read&outcome=denied&limit=25
Authorization: Bearer <access-token>
```

Results use descending `(occurredAt, id)` keyset ordering. A continuation
request repeats the same filters and limit and adds `cursor`. The cursor binds
those criteria without exposing a database offset. Listing is itself recorded
as `audit.events_listed` before the query runs and can appear in the returned
page when it matches the filters.

Responses include `Cache-Control: no-store`. The typed Go client additionally
validates input filters, returned events, optional byte ranges, descending
ordering, duplicate event IDs, and the bounded URL-safe cursor representation.

## Integrity and operational limits

The application exposes no audit create, update, or delete endpoint. SQLite
triggers reject direct `UPDATE` and `DELETE` statements against `audit_events`.
This is append-only protection inside the application database, not proof
against a privileged operator.

An operator who can replace the database file, executable, backups, or migration
state can still alter history. Cryptographic hash chains, signed checkpoints,
remote collection, write-once storage, retention policy, archival, export, and
SIEM integration remain future controls.

The initial schema retains audit events indefinitely. Administrators should
monitor database growth and protect database files and backups with operating
system access controls and encryption appropriate to the deployment.

## Verification map

| Security property | Primary automated evidence |
| --- | --- |
| Event and reason allowlists | `internal/audit` tests |
| No unrestricted event or database fields | Audit security-contract and SQLite schema tests |
| Append-only rows | SQLite update and delete rejection tests |
| State change and event atomicity | Session, user, password, media, and grant repository tests |
| Denial fail-closed behavior | Session, user, password, upload, media, and grant service tests |
| File-operation compensation | Managed upload and deletion service tests |
| Content withheld on audit failure | Media-content API tests |
| Audit query withheld on audit failure | Audit-directory application tests |
| History survives domain deletion | Audit repository retention test |
| Filter and cursor validation | Audit application, API, repository, and client tests |

Run the complete local verification with:

```console
go run ./cmd/projectctl check &&
go run ./cmd/projectctl quality-gate
```
