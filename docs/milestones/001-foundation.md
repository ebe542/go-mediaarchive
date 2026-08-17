# Milestone 1: Executable foundation

## Goal

Establish a minimal, tested server foundation and a repeatable quality gate before
adding persistence, authentication, or media management.

## Scope

- Initialize the Go module as `github.com/ebe542/go-mediaarchive`.
- Enforce LF line endings and project-level editor settings.
- Implement `GET /api/v1/health` with a JSON response.
- Test the endpoint through the public HTTP handler contract.
- Provide an executable server with CLI-based address configuration.
- Configure explicit HTTP timeouts and graceful shutdown.
- Provide a script that verifies milestone quality.
- Publish the initial project to GitHub under the MIT License.

## API contract

Request:

```http
GET /api/v1/health HTTP/1.1
Accept: application/json
```

Successful response:

```http
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
```

```json
{"status":"ok"}
```

## Acceptance criteria

- [x] The health handler has an automated test.
- [x] The health endpoint returns HTTP status `200`.
- [x] The response uses `application/json; charset=utf-8`.
- [x] The response body contains `{"status":"ok"}`.
- [x] The server address can be supplied with `--addr`.
- [x] The server configures defensive HTTP timeouts.
- [x] `SIGINT` and `SIGTERM` trigger graceful shutdown.
- [x] The milestone script passes its standard checks.
- [x] A local script validates and optionally emulates the CI workflow.
- [x] The public repository contains the MIT License.
- [x] The initial commit is available on GitHub.

## Verification

```bash
./scripts/check_milestone.sh
```

Optional race-detector verification:

```bash
./scripts/check_milestone.sh --race
```

Manual smoke test:

```bash
go run ./cmd/server --addr 127.0.0.1:8080
```

In another terminal:

```bash
curl --fail-with-body --silent --show-error \
  --header "Accept: application/json" \
  http://127.0.0.1:8080/api/v1/health
```

## Out of scope

- CLI client
- SQLite and migrations
- User authentication and authorization
- Media metadata and file storage
- TLS and mTLS

These capabilities belong to later executable milestones.
