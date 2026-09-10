# Milestone 15: Secure media authorization model

## Goal

Define minimal searchable media metadata and a storage-independent policy that
makes every content operation depend on an explicit authorization decision.

## Scope

- Add a common media identity for books, documents, and videos.
- Keep metadata deliberately small and independent of catalog-specific fields.
- Record zero or more authors for identification and later search.
- Model ownership separately from explicitly granted permissions.
- Centralize content-authorization decisions in one domain policy.
- Define how global user roles interact with media-specific permissions.
- Keep storage paths, file transfer, persistence, and HTTP endpoints out of this
  milestone.

## Minimal media identity

Each media item contains:

- a stable media ID;
- a title;
- an optional ordered list of authors;
- the original filename;
- a media type: `book`, `document`, or `video`;
- a MIME type;
- a byte size;
- a SHA-256 checksum;
- an owner user ID;
- creation and update timestamps.

Authors are optional because not every document or video has a meaningful
author. When present, every author must contain a non-empty normalized display
value. Author order is preserved and duplicate entries are rejected.

The original filename is descriptive metadata, not a filesystem path. It must
not contain path separators or represent a relative or absolute path. A future
storage component will generate and retain its own internal location without
exposing it through the public media identity.

The checksum verifies content integrity; it does not provide encryption or
prove that content is trustworthy.

## Validation boundaries

- Media and owner IDs use the existing canonical UUID representation.
- Titles contain 1 through 200 Unicode characters after trimming. Control
  characters are rejected.
- An item has at most 20 authors. Each author contains 1 through 100 Unicode
  characters after trimming, control characters are rejected, and an exact
  normalized duplicate is rejected.
- Original filenames contain 1 through 255 Unicode characters after trimming.
  Path separators, drive designators, control characters, `.` and `..` are
  rejected.
- MIME types contain only a syntactically valid base media type without
  parameters and use at most 127 characters.
- Byte size is positive and fits in a signed 64-bit integer. A future storage
  service may enforce a smaller configurable upload limit.
- SHA-256 checksums contain exactly 32 bytes in the domain model.
- Creation and update timestamps are non-zero, stored in UTC, and the update
  time cannot precede the creation time.
- Grants contain exactly one valid media permission. The media ID and grantee
  user ID must both be valid.

## Permissions

The authorization model defines these media-specific permissions:

| Permission | Decision controlled |
| --- | --- |
| `discover` | Include metadata in searches and directory results. |
| `read` | Stream content for viewing. |
| `download` | Provide an explicit persistent download. |
| `update` | Replace mutable metadata or content. |
| `delete` | Permanently remove the media item. |
| `share` | Grant or revoke permissions for other users. |

The owner receives every permission implicitly. Other users receive only
explicit grants. A grant is identified by media ID, user ID, and permission.
Inactive users are never authorized by a stored grant.

Global roles do not silently grant content access. In particular, the `admin`
role permits administrative workflows but does not automatically authorize
`read` or `download`. This separates operation of the service from access to
licensed content.

The distinction between `read` and `download` supports policy, user experience,
and future audit records. It cannot guarantee that content displayed on an
authorized client device will never be copied.

## Security rules

- Every media operation asks the same policy for an authorization decision.
- API handlers, clients, and repositories do not duplicate policy logic.
- Authorization requires an active user identity.
- Unknown media types and permissions are rejected.
- Owner and grantee identifiers use validated user IDs.
- Filenames never select or construct a server storage path.
- Metadata and permission collections have explicit size limits.
- Future HTTP responses must not reveal whether undiscoverable media exists.
- Future audit records must retain an actor snapshot independently of the user
  identity lifecycle.

## Implementation sequence

1. Define this milestone, its boundaries, and authorization rules.
2. Add the minimal media identity, validation, and domain tests.
3. Add explicit grants, the centralized authorization policy, and tests.

Each step is delivered as a complete Conventional Commit.

## Acceptance criteria

- [x] Minimal metadata and authorization semantics are documented.
- [x] Authors are represented without introducing catalog-specific metadata.
- [x] Storage paths and file transfer are explicitly outside the domain model.
- [x] Media IDs, types, titles, authors, filenames, MIME types, sizes,
  checksums, owner IDs, and timestamps are validated.
- [x] Original filenames cannot be interpreted as storage paths.
- [ ] Owners receive all media permissions implicitly.
- [ ] Active grantees receive only their explicit media permissions.
- [ ] Inactive users receive no media permission.
- [ ] Global administrator status does not imply content read or download.
- [ ] Invalid grants and permissions are rejected.
- [ ] Standard milestone checks pass.
- [ ] Local quality gate checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-015` tag after all milestone commits are complete.

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

- SQLite media and permission tables
- Search queries and pagination
- Upload, streaming, and download endpoints
- Server filesystem layout
- Content encryption at rest
- HTTP Range requests
- Ownership transfer
- Audit-log persistence
- Group- or role-based media grants
