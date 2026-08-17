# Project goals

## Vision

Go Media Archive is a production-oriented learning project that demonstrates how
to design, implement, test, secure, and operate a multi-user media archive in Go.
It manages books, documents, and videos without making licensed content public.

## Primary goals

- Provide a versioned JSON REST API under `/api/v1`.
- Provide a command-line client as the first user interface.
- Keep the server independent from any graphical or web user interface.
- Store metadata and storage locations in SQLite initially.
- Store media files in managed local file storage rather than SQLite BLOBs.
- Support multiple users, roles, and object-level permissions.
- Separate metadata visibility from content download permission.
- Develop features test-first in independently executable milestones.
- Document architecture decisions, security considerations, and operations.
- Publish source code and safe fixtures without publishing managed content.

## Media scope

The initial media types are:

- `book`
- `document`
- `video`

Shared metadata will include an ID, media type, title, description, MIME type,
file size, SHA-256 checksum, storage location, owner, and timestamps. Specialized
metadata such as ISBN, document language, or video duration will be introduced
only with concrete use cases.

## Access control

The initial roles are:

| Role | Purpose |
| --- | --- |
| `viewer` | View permitted metadata; download only when explicitly allowed |
| `editor` | Create media and manage permitted or owned records |
| `admin` | Manage users, policies, and audit data |

New media is private by default. Anonymous access and public sharing links are
outside the initial scope. Administrative capability must not automatically be
treated as a content license.

Permissions will distinguish operations such as:

- viewing metadata;
- downloading content;
- creating, updating, and deleting media;
- managing users and permissions;
- reading audit records.

## Security goals

- Require TLS for network communication outside local development.
- Explore mTLS as an additional client-device trust layer.
- Keep user authentication and authorization independent from mTLS identity.
- Hash passwords with Argon2id and never store plaintext credentials.
- Audit successful and rejected security-sensitive operations.
- Prevent path traversal and direct access to internal storage paths.
- Apply upload limits, request timeouts, and safe error responses.
- Keep databases, media files, secrets, certificates, and private keys out of Git.
- Use operating-system or volume encryption for data at rest initially.

## Data storage

SQLite is the first metadata database. It will use migrations, foreign keys,
transactions, WAL mode, and a configured busy timeout. The application will run
as a single server instance initially.

PostgreSQL is a later learning milestone. Its implementation will be validated
with the same behavioral repository tests as SQLite. The design will abstract
domain operations rather than attempting to hide arbitrary SQL behind a generic
database wrapper.

## Non-goals for the first version

- Web or mobile user interfaces
- Anonymous access
- Public download links
- Video transcoding or adaptive streaming
- Automated content analysis
- Full-text extraction from managed files
- Cloud or S3-compatible storage
- Multiple concurrently deployed server instances
- Organization-level multi-tenancy
- OAuth, LDAP, or single sign-on
- Custom cryptographic algorithms

## Engineering principles

- Use Go 1.26 and prefer the standard library where practical.
- Introduce third-party dependencies only with a documented reason.
- Follow red-green-refactor development.
- Keep packages small and responsibilities explicit.
- Use English for code, names, comments, documentation, and API messages.
- Use LF line endings for all text files.
- Keep every milestone executable and independently verifiable.
- Use the MIT License for the public source project.
