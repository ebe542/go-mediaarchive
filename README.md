# Go Media Archive

Go Media Archive is a production-oriented learning project for managing books,
documents, and videos through a versioned REST API. It is designed as a private,
multi-user archive for content that may be licensed and must not be exposed
publicly.

The source code is intended to be public. Managed media, metadata, credentials,
private keys, certificates, and database files are not.

## Current status

Milestone 2 is complete. The project currently provides:

- a Go 1.26 module;
- a versioned `GET /api/v1/health` endpoint;
- an automated HTTP handler test;
- an executable HTTP server with explicit timeouts and graceful shutdown;
- a typed HTTP client with response validation, cancellation, and timeouts;
- an executable CLI with a `health` command and explicit exit codes;
- a repeatable milestone verification script.

## Requirements

- Go 1.26.6 or newer within the Go 1.26 release line
- Git
- Git Bash on Windows for the documented commands
- VS Code with the official Go extension is recommended

## Run the server

```bash
go run ./cmd/server --addr 127.0.0.1:8080
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

## Verify a milestone

Run the standard checks:

```bash
./scripts/check_milestone.sh
```

Run the additional race detector when the local toolchain supports it:

```bash
./scripts/check_milestone.sh --race
```

Validate the GitHub Actions workflow and reproduce its project checks locally:

```bash
./scripts/check_ci.sh
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
[milestone documentation](docs/milestones/001-foundation.md) for details.

## License

This project is intended to use the MIT License.
