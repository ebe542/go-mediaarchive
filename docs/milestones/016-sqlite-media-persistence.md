# Milestone 16: SQLite media persistence

## Goal

Persist minimal media metadata, ordered authors, and compact per-user grants
without storing file content or exposing server filesystem locations.

## Scope

- Add strict SQLite tables for media items, authors, and permission grants.
- Store SHA-256 checksums as fixed-length binary values.
- Preserve author order without embedding JSON in relational columns.
- Store one validated permission bitmask per media and grantee.
- Add SQLite repositories for media identities and grants.
- Prevent deletion of users who still own media.
- Remove grants held by a user when their identity is deleted.
- Preserve the existing last-active-administrator and transactional deletion
  guarantees.

## Database structure

### `media_items`

| Column | SQLite type | Purpose |
| --- | --- | --- |
| `id` | `TEXT` | Canonical media UUID and primary key. |
| `title` | `TEXT` | Minimal searchable title. |
| `original_filename` | `TEXT` | Path-safe descriptive source filename. |
| `media_type` | `TEXT` | `book`, `document`, or `video`. |
| `mime_type` | `TEXT` | Canonical base MIME type. |
| `size` | `INTEGER` | Positive content length in bytes. |
| `checksum` | `BLOB` | Exactly 32 SHA-256 bytes. |
| `owner_id` | `TEXT` | Foreign key to the owning user. |
| `created_at` | `TEXT` | UTC creation time in RFC 3339 Nano format. |
| `updated_at` | `TEXT` | UTC update time in RFC 3339 Nano format. |

The owner foreign key uses restricted deletion. A user identity therefore
cannot disappear while it owns media.

### `media_authors`

| Column | SQLite type | Purpose |
| --- | --- | --- |
| `media_id` | `TEXT` | Foreign key to the media item. |
| `position` | `INTEGER` | Zero-based author order. |
| `name` | `TEXT` | Validated author display value. |

The primary key is `(media_id, position)`. A unique constraint on
`(media_id, name)` rejects duplicate authors. Authors are replaced within the
same transaction as their media metadata.

### `media_grants`

| Column | SQLite type | Purpose |
| --- | --- | --- |
| `media_id` | `TEXT` | Foreign key to the protected media item. |
| `user_id` | `TEXT` | Foreign key to the grantee identity. |
| `permissions` | `INTEGER` | Validated internal permission bitmask. |

The primary key is `(media_id, user_id)`, providing exactly one grant per media
and user. The mask must be between `1` and `63`; zero and unknown bits are
rejected. Public APIs will expose permission names rather than this number.

## User deletion behavior

Self-deletion remains unavailable. Administrator-driven deletion follows these
rules:

- A user who owns at least one media item is not deleted.
- The application reports a specific ownership conflict rather than an
  internal database error.
- Grants held by the user are explicitly deleted within the user-deletion
  transaction because they do not represent ownership.
- Owned media is never removed through `ON DELETE CASCADE`.
- Ownership is never transferred implicitly.
- The user can be deleted only after future explicit transfer or media-deletion
  workflows have removed all ownership references.

These rules prevent an identity-management action from silently destroying
licensed content.

## Repository behavior

The media repository supports:

- creating an item and its ordered authors atomically;
- finding an item by stable ID;
- replacing mutable metadata and authors atomically;
- deleting an item and its authors explicitly.

The grant repository supports:

- inserting or replacing one complete permission mask;
- finding a grant for one media and user pair;
- listing grants for a media item in deterministic order;
- deleting a grant explicitly.

A zero permission mask is not interpreted as revocation. Complete revocation
uses the explicit delete operation.

## Implementation sequence

1. Define the schema, persistence boundaries, and ownership rules.
2. Add strict media, author, and grant migrations with database tests.
3. Add the transactional SQLite media repository and contract tests.
4. Add the SQLite grant repository and contract tests.
5. Integrate media ownership and grant cleanup into safe user deletion.

Each step is delivered as a complete Conventional Commit.

## Acceptance criteria

- [x] The relational schema and ownership lifecycle are documented.
- [x] Permission masks remain an internal persistence detail.
- [x] Automatic deletion of owned media is explicitly prohibited.
- [x] Strict migrations create media, author, and grant tables exactly once.
- [x] Database constraints reject invalid media metadata and permission masks.
- [ ] Media items and ordered authors are created atomically.
- [ ] Media metadata and authors can be found, updated, and deleted.
- [ ] Grants can be saved, found, listed, replaced, and deleted.
- [ ] Grant persistence accepts only known non-empty permission masks.
- [ ] User deletion removes grants held by the target user.
- [ ] User deletion is blocked while the target user owns media.
- [ ] Failed user deletion rolls back grant and authentication-data changes.
- [ ] Standard milestone checks pass.
- [ ] Local quality gate checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-016` tag after all milestone commits are complete.

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

- Self-service account deletion
- Automatic or cascading deletion of owned media
- Ownership transfer
- Search and pagination
- Application services and REST endpoints for media
- File upload, streaming, and download
- Server filesystem locations
- Content encryption at rest
- Audit-log persistence
