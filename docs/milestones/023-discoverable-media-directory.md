# Milestone 23: Discoverable Media Directory

## Goal

Allow authenticated users to find media that they own or may explicitly
discover, without revealing private records through results, counts, cursors,
or errors.

## Scope

- Define bounded title-and-author search with an optional media-type filter.
- Use immutable descending creation-time and ID keyset pagination.
- Select only owned media or media with an explicit `discover` grant.
- Add SQLite indexes for unfiltered and media-type directory traversal.
- Expose `GET /api/v1/media` with filter-bound opaque cursors.
- Add a validated typed-client media-directory operation.
- Add `media list`, `media search`, `media next`, `media first`, and
  `media get <id>` to the interactive client.
- Keep media-management and content-transfer commands outside this milestone.

## Security properties

- Inactive or unauthenticated users cannot use the directory.
- Global roles do not bypass media discovery permission.
- Authorization is part of the repository query rather than post-query
  filtering, so hidden rows cannot affect returned page sizes.
- Owners can always discover their own media.
- Other users require an explicit grant containing `discover`.
- Search input, page limits, cursors, and response items are bounded and
  validated before use.
- Cursors bind search text, media type, and page limit without exposing a
  database offset.
- Directory access does not create permanent audit events; otherwise ordinary
  pagination could grow immutable security history without bound.

## Ordering and filtering

Results use descending `(createdAt, id)` order. Both values are immutable, so
title edits and author changes cannot move an item between pages. Search is a
case-insensitive substring match over title and ordered author names. SQLite's
initial `lower()` behavior provides ASCII case folding; broader language-aware
catalog search remains outside this milestone.

The optional media type is one of `book`, `document`, or `video`. A blank query
or type leaves that criterion unrestricted. The default page size is 50 and the
maximum is 100.

## Implementation sequence

1. Add application filter, cursor, page, and repository contracts.
2. Add authorization-aware SQLite discovery and supporting indexes.
3. Expose the authenticated REST directory with opaque bound cursors.
4. Add typed-client request and response validation.
5. Add interactive client discovery commands and retained pagination state.
6. Complete public documentation and quality gates.

## Commit plan

```text
feat(media): add discoverable directory
feat(api): expose media directory
feat(client): add media directory support
feat(client): add media discovery commands
docs(media): complete discovery milestone
```

## Acceptance criteria

- [x] Directory filters and immutable cursors are validated in the application layer.
- [x] SQLite returns only owned or explicitly discoverable media.
- [x] Title, author, type, literal wildcard, and cursor behavior are tested.
- [x] Directory traversal has dedicated SQLite indexes.
- [x] The authenticated API exposes a filtered, paginated media directory.
- [x] Opaque cursors are bound to the complete filter and page limit.
- [x] The typed client validates filters, cursors, ordering, and media responses.
- [x] The interactive client supports list, search, next, first, and get commands.
- [x] Logout and password replacement clear media pagination state.
- [ ] Public documentation describes media discovery and its authorization boundary.
- [ ] Standard project checks pass.
- [ ] Local quality gate checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-023` tag after all milestone commits are complete.

## Verification

```console
go run ./cmd/projectctl quality-gate
```

## Out of scope

- Media upload, update, deletion, and grant commands
- Explicit persistent download
- Temporary-file viewer integration
- Full-text extraction or content indexing
- Language-aware ranking, stemming, or fuzzy search
- Anonymous discovery and public sharing links
- PostgreSQL persistence
