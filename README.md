# Go Media Archive

Go Media Archive is a production-oriented learning project for managing books,
documents, and videos through a versioned REST API. It is designed as a private,
multi-user archive for content that may be licensed and must not be exposed
publicly.

The source code is intended to be public. Managed media, metadata, credentials,
private keys, certificates, and database files are not.

## Current status

Milestone 9 is in progress. The project currently provides:

- a Go 1.26 module;
- a versioned `GET /api/v1/health` endpoint;
- an automated HTTP handler test;
- an executable HTTP server with explicit timeouts and graceful shutdown;
- a typed HTTP client with response validation, cancellation, and timeouts;
- an executable CLI with a `health` command and explicit exit codes;
- a CGO-free SQLite foundation with WAL, foreign keys, and embedded migrations;
- persistent user identities with global roles and a SQLite repository;
- a storage-independent application service for user administration;
- Argon2id password credentials and an atomic local administrator bootstrap;
- TLS 1.3 network transport with explicit CLI certificate trust;
- opaque server-side authentication sessions with login throttling;
- a repeatable milestone verification script.

## Requirements

- Go 1.26.6 or newer within the Go 1.26 release line
- Git
- Git Bash on Windows for the documented commands
- VS Code with the official Go extension is recommended

## Run the server

For local loopback development, run plain HTTP with:

```bash
go run ./cmd/server \
  --addr 127.0.0.1:8080 \
  --database ./data/mediaarchive.db
```

Check its health from another terminal:

```bash
curl --fail-with-body --silent --show-error \
  --header "Accept: application/json" \
  http://127.0.0.1:8080/api/v1/health
```

Expected response:

```json
{"status":"ok"}
```

Plain HTTP is rejected for wildcard, hostname, and non-loopback listener
addresses. For network access, provide a TLS certificate and matching private
key:

```bash
go run ./cmd/server \
  --addr 0.0.0.0:8443 \
  --database ./data/mediaarchive.db \
  --tls-certificate /path/to/server.crt \
  --tls-private-key /path/to/server.key
```

The server requires TLS 1.3 or newer. Both TLS files must be configured
together. Certificate and private-key files must remain outside version control.

## Run the CLI

With the server running, check its status from another terminal:

```bash
go run ./cmd/client --server http://127.0.0.1:8080 health
```

Expected output:

```text
Server status: ok
```

The server URL uses the following configuration precedence:

```text
--server flag > MEDIAARCHIVE_SERVER environment variable > built-in default
```

When an HTTPS server uses a private certificate authority, add that CA to the
CLI trust configuration:

```bash
go run ./cmd/client \
  --server https://archive.example.test:8443 \
  --ca-certificate /path/to/archive-ca.crt \
  health
```

The additional CA extends the system trust store. Certificate verification
cannot be disabled.

## Bootstrap the first administrator

Create the first administrator directly in a local SQLite database before the
first API login:

```bash
go run ./cmd/admin bootstrap \
  --database ./data/mediaarchive.db \
  --username archive-admin \
  --display-name "Archive Administrator"
```

The command requests and confirms the password interactively without terminal
echo. Passwords contain between 15 and 1,024 Unicode characters. They are hashed
with Argon2id and are never accepted as command-line arguments or environment
variables.

Bootstrap succeeds only while the database contains no users. It atomically
creates the initial administrator and password credential. Subsequent user
administration will require authenticated and authorized API operations.

## Verify a milestone

Run the standard checks:

```bash
./scripts/check_milestone.sh
```

Run the additional race detector when the local toolchain supports it:

```bash
./scripts/check_milestone.sh --race
```

Run the equivalent checks from Windows PowerShell:

```powershell
.\scripts\check_milestone.ps1 -Race
```

Validate the GitHub Actions workflow and reproduce its project checks locally:

```bash
./scripts/check_ci.sh
```

From Windows PowerShell, run:

```powershell
.\scripts\check_ci.ps1
```

Run the complete `verify` job locally with `act` and Docker:

```bash
./scripts/check_ci.sh --act
```

## Project direction

The first production scope will use SQLite for metadata and local managed file
storage. A later learning milestone will add PostgreSQL and run shared repository
contract tests against both database implementations.

The first client will be a command-line application. The server will not provide
a web interface. Network deployments will require TLS, with mutual TLS (mTLS)
planned as an advanced security milestone in addition to user authentication and
authorization.

See [Project goals](docs/project-goals.md) and the
[milestone overview](docs/milestones/README.md) for details.

## License

This project uses the MIT License.
