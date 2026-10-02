# Milestone 20: Authorized Content Streaming

## Goal

Allow an authenticated user with the media-specific `read` permission to view
or play server-managed content without granting the separate right to retain an
explicit download.

## Scope

- Add safe managed-content reading to the filesystem adapter.
- Coordinate concurrent reads and staged deletion within one server process.
- Add an application service that applies the existing `read` authorization.
- Add authenticated `GET` and `HEAD` content endpoints.
- Support complete responses and one bounded HTTP byte range.
- Return safe inline-content and cache-control headers.
- Preserve JSON error responses before any binary response is committed.
- Wire content reading into the executable server.
- Add a typed client operation that streams into an `io.Writer`.
- Keep explicit download, interactive media commands, content replacement, and
  browser session cookies outside this milestone.

## Endpoints

| Method | Path | Success | Required media permission |
| --- | --- | --- | --- |
| `GET` | `/api/v1/media/{id}/content` | `200 OK` or `206 Partial Content` | `read` |
| `HEAD` | `/api/v1/media/{id}/content` | `200 OK` or `206 Partial Content` | `read` |

The owner has the existing implicit permission set and may therefore read their
content. Another active user requires an explicit grant containing `read`.
Possessing only `discover` or `download` is insufficient. A global
administrator role does not bypass object authorization.

## Not-found masking

The following conditions all produce the same `404 Not Found` JSON response:

- the media identity does not exist;
- the actor may not read the media;
- the media identity has no managed-content location;
- the managed file is absent from the configured content root.

The application checks media authorization before retrieving a content location
or opening a file. This preserves the existing anti-enumeration boundary and
prevents unauthorized callers from reaching storage adapters.

## Successful response

A complete response includes:

```http
HTTP/1.1 200 OK
Content-Type: application/pdf
Content-Length: 123456
Content-Disposition: inline; filename="example.pdf"
Cache-Control: private, no-store
X-Content-Type-Options: nosniff
ETag: "0123456789abcdef..."
Last-Modified: Sat, 20 Sep 2026 10:00:00 GMT
Accept-Ranges: bytes
```

`Content-Type`, filename, byte size, checksum, and modification time come from
validated domain or storage data. Absolute paths and storage keys never appear
in headers or bodies. `Content-Disposition` uses safe standard-library encoding
for non-ASCII filenames.

`GET` streams bytes after all authorization and range validation has succeeded.
`HEAD` returns the same representation headers without opening a response body.
Successful content responses are binary rather than JSON. Errors remain in the
existing bounded JSON error envelope.

## Byte ranges

The endpoint supports either no `Range` header or one byte range:

```http
Range: bytes=0-65535
Range: bytes=65536-
Range: bytes=-65536
```

A satisfiable range produces `206 Partial Content`, `Content-Range`, and the
selected `Content-Length`. The server seeks to the validated start offset and
copies at most the selected number of bytes.

Unsupported range units, multiple ranges, malformed values, empty files, and
unsatisfiable ranges produce `416 Range Not Satisfiable` with:

```http
Content-Range: bytes */123456
Content-Type: application/json; charset=utf-8
```

Range parsing and validation occur before response headers are committed. The
server does not use a helper that could replace the project's JSON error format
with an implementation-defined plain-text error.

Conditional requests such as `If-Range`, `If-None-Match`, and
`If-Modified-Since` are outside this milestone. The strong ETag is exposed for
future conditional-request support.

## Filesystem reading

The filesystem adapter accepts only the existing normalized relative storage
key format. It rejects symbolic links and non-regular files, opens the final
managed path read-only, and returns a seekable handle with size and modification
time. Callers must close the handle.

An open handle retains a process-local shared lock until it is closed. Staged
deletion takes the corresponding exclusive lock. Concurrent reads remain
allowed, while deletion waits for active streams to finish. This provides
consistent behavior across Unix and Windows without exposing a partially
deleted resource. Coordination across multiple server processes or shared
network filesystems remains outside the local-storage deployment model.

Request cancellation and server write deadlines stop copying and close the
handle. Opening or seeking a file never loads the complete content into memory.

## Application service

The content-reading service performs these steps:

1. Resolve the media identity and apply `PermissionRead` through the existing
   centralized media authorization policy.
2. Retrieve the content location only after authorization succeeds.
3. Open the managed file only after the location is available.
4. Return the authorized media metadata and closeable content handle.

Unknown and unauthorized media continue to use the same application error.
Missing locations and files are also mapped to that masked result. Unexpected
repository and filesystem failures remain internal operational errors.

## HTTP streaming safety

- No content bytes are written before status and headers are final.
- The response uses `Cache-Control: private, no-store` because content may be
  licensed and user-specific.
- `X-Content-Type-Options: nosniff` prevents browser MIME sniffing.
- `Content-Disposition: inline` expresses viewing rather than an explicit
  persistent download.
- The server never logs bearer tokens, content bytes, storage keys, absolute
  paths, or request bodies.
- A disconnected client cannot cause the server to buffer the remaining file.
- Read permission does not imply `download`, `update`, `delete`, or `share`.

## Typed client

The typed client accepts an `io.Writer` and streams the response into it. It
returns validated response metadata including content type, filename, full or
selected length, ETag, last-modified time, and any returned byte range.

The caller may request one optional byte range. The client validates response
status and headers before copying. API errors remain bounded JSON responses. A
network or destination-writer failure may leave partial output; the client does
not retry automatically because an arbitrary `io.Writer` cannot be rewound
safely.

This operation is an API primitive for viewers and graphical clients. The
interactive CLI does not write content to a persistent file in this milestone,
because that behavior belongs to the separate `download` permission.

## Browser use

A graphical web client can issue an HTTPS `fetch` request with the bearer token,
create a browser Blob URL from the response, and give that URL to an appropriate
PDF viewer or media element. A plain `<iframe src>` request cannot attach the
current bearer header. Cookie-based browser sessions and a dedicated web UI are
separate future security decisions.

## Error mapping

| Condition | Status |
| --- | --- |
| Missing, malformed, expired, or revoked session | `401 Unauthorized` |
| Unknown, unauthorized, metadata-only, or missing managed content | `404 Not Found` |
| Malformed, multiple, or unsatisfiable byte range | `416 Range Not Satisfiable` |
| Repository, filesystem, seek, or streaming setup failure | `500 Internal Server Error` |

An error detected after a successful binary response has started cannot be
replaced with JSON. The server stops streaming, closes the file, and relies on
operational diagnostics without exposing details to the client.

## Implementation sequence

1. Define streaming, authorization, range, and security contracts.
2. Add coordinated managed-content reading to the filesystem adapter.
3. Add the authorized content-reading application service.
4. Add the authenticated `GET` and `HEAD` API with range support.
5. Wire the content-reading service into the executable server.
6. Add typed streaming client support and complete documentation.

Each implementation step is delivered as a complete Conventional Commit. Tests
remain in the same commit as the behavior they specify.

## Acceptance criteria

- [x] Streaming, authorization, range, and security contracts are documented.
- [x] Filesystem reads validate keys, reject links, and remain seekable.
- [x] Active reads and staged deletion are coordinated within one process.
- [x] The application authorizes `read` before location or filesystem access.
- [x] Unknown, unauthorized, and unavailable content share a masked result.
- [ ] `GET` streams complete and single-range content without full buffering.
- [ ] `HEAD` returns matching headers without response content.
- [ ] Content responses use safe inline, cache, MIME, and integrity headers.
- [ ] Range failures retain the JSON error format and disclose no internals.
- [ ] The executable server wires the real content-reading service.
- [ ] The typed client streams into an `io.Writer` and validates headers.
- [ ] Standard project checks pass.
- [ ] Local quality gate checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-020` tag after all milestone commits are complete.

## Verification

```console
go run ./cmd/projectctl quality-gate
```

## Out of scope

- Explicit persistent download and `Content-Disposition: attachment`
- Multiple HTTP ranges and multipart byte-range responses
- Conditional requests and cache revalidation
- Content replacement
- Media search, filtering, and pagination
- Interactive client and administrator media commands
- Browser cookie sessions and a server-provided web interface
- Deep format validation or malware scanning
- Orphaned-file reconciliation
- Filesystem encryption at rest
- Object storage and remote storage providers
- Audit-log persistence
- mTLS client authentication
- PostgreSQL persistence
