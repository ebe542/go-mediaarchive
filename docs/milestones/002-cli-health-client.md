# Milestone 2: CLI health client

## Goal

Provide the first command-line client and establish a reusable, typed boundary
for communication with the versioned REST API.

## Scope

- Add an internal HTTP client package.
- Call `GET /api/v1/health` with an explicit JSON accept header.
- Decode and validate the JSON response.
- Apply a request timeout and support context cancellation.
- Return useful errors for transport failures, unexpected status codes, and
  invalid response bodies.
- Add an executable `mediaarchive` CLI with a `health` command.
- Accept the server base URL primarily through a CLI flag.
- Print a concise human-readable result.
- Use meaningful process exit codes.
- Test client behavior without starting the real server.

## CLI contract

```bash
go run ./cmd/client --server http://127.0.0.1:8080 health
```

Successful output:

```text
Server status: ok
```

The built-in server URL is `http://127.0.0.1:8080`. The
`MEDIAARCHIVE_SERVER` environment variable may provide a different default, and
the explicit `--server` flag takes precedence.

Configuration precedence:

```text
CLI flag > environment variable > built-in default
```

## Client boundary

The CLI must not construct HTTP requests directly. The `internal/client` package
owns endpoint paths, request headers, response decoding, and HTTP-related error
context. This boundary can later add authentication, TLS, and mTLS without
coupling those details to individual commands.

## Acceptance criteria

- [x] A client test verifies the HTTP method, path, and `Accept` header.
- [x] The client decodes a successful health response.
- [x] The client reports non-success HTTP status codes.
- [x] The client reports malformed JSON responses.
- [x] The client honors context cancellation and HTTP timeouts.
- [x] The CLI supports `--server` and the `health` command.
- [x] The CLI prints `Server status: ok` for a healthy server.
- [x] Invalid arguments produce usage information and exit code `2`.
- [x] Standard milestone checks pass locally.
- [ ] GitHub Actions passes on `main`.

## Verification

Run all automated milestone checks:

```bash
./scripts/check_milestone.sh
```

Run the server:

```bash
go run ./cmd/server --addr 127.0.0.1:8080
```

Run the CLI in another terminal:

```bash
go run ./cmd/client --server http://127.0.0.1:8080 health
```

## Out of scope

- User authentication and authorization
- TLS and mTLS client configuration
- Persistent CLI configuration files
- Media management commands
- Shell completion
