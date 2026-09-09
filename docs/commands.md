# Command reference

This page documents the commands currently provided by Go Media Archive. Run
development commands from the repository root in Git Bash unless a PowerShell
alternative is shown explicitly.

## Programs

Source commands and release binaries provide the same behavior:

| Purpose | Development command | Release binary |
| --- | --- | --- |
| REST API server | `go run ./cmd/server` | `go-mediaarchive-server` |
| End-user console | `go run ./cmd/client` | `go-mediaarchive-client` |
| Administrator console | `go run ./cmd/admin` | `go-mediaarchive-admin` |

Examples below use development commands. Replace the `go run ./cmd/...` prefix
with the corresponding installed binary when using a release.

## Configuration precedence

Runtime configuration uses this precedence:

```text
command-line flag > environment variable > built-in default
```

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| Server listener | `--addr` | `MEDIAARCHIVE_ADDR` | `127.0.0.1:8080` |
| SQLite database | `--database` | `MEDIAARCHIVE_DATABASE` | `data/mediaarchive.db` |
| TLS certificate | `--tls-certificate` | `MEDIAARCHIVE_TLS_CERTIFICATE` | disabled |
| TLS private key | `--tls-private-key` | `MEDIAARCHIVE_TLS_PRIVATE_KEY` | disabled |
| Enrollment lifetime | `--password-enrollment-lifetime` | `MEDIAARCHIVE_PASSWORD_ENROLLMENT_LIFETIME` | `24h` |
| Client server URL | `--server` | `MEDIAARCHIVE_SERVER` | `http://127.0.0.1:8080` |
| Additional trusted CA | `--ca-certificate` | none | system trust only |

The administrator bootstrap uses only its command-line options because it is a
local, offline operation.

## Server

### Local loopback server

Plain HTTP is permitted only for loopback development addresses:

```bash
go run ./cmd/server \
  --addr 127.0.0.1:8080 \
  --database ./data/mediaarchive.db
```

### HTTPS server

Both certificate files must be configured together. The server requires TLS
1.3 or newer. `CERTIFICATE_DIRECTORY` is a shell helper used only by these
examples; it is not an application configuration variable.

```bash
export CERTIFICATE_DIRECTORY=/path/to/go-mediaarchive-certificates && \
go run ./cmd/server \
  --addr 0.0.0.0:8443 \
  --database ./data/mediaarchive.db \
  --tls-certificate "$CERTIFICATE_DIRECTORY/server.crt" \
  --tls-private-key "$CERTIFICATE_DIRECTORY/server.key"
```

### Enrollment lifetime

Use a Go duration to change the validity of newly issued enrollment tokens:

```bash
go run ./cmd/server --password-enrollment-lifetime 12h
```

### Environment example

```bash
export CERTIFICATE_DIRECTORY=/path/to/go-mediaarchive-certificates && \
export MEDIAARCHIVE_ADDR=127.0.0.1:8443 && \
export MEDIAARCHIVE_DATABASE=./data/mediaarchive.db && \
export MEDIAARCHIVE_TLS_CERTIFICATE="$CERTIFICATE_DIRECTORY/server.crt" && \
export MEDIAARCHIVE_TLS_PRIVATE_KEY="$CERTIFICATE_DIRECTORY/server.key" && \
export MEDIAARCHIVE_PASSWORD_ENROLLMENT_LIFETIME=12h && \
go run ./cmd/server
```

## Local test certificates

Keep test certificates and all private keys outside the repository. The
following example creates a private test CA and a server certificate valid for
`localhost` and `127.0.0.1`.

```bash
export CERTIFICATE_DIRECTORY=/path/to/go-mediaarchive-certificates && \
mkdir -p "$CERTIFICATE_DIRECTORY" && \
cd "$CERTIFICATE_DIRECTORY" && \
openssl req -x509 \
  -newkey rsa:3072 \
  -sha256 \
  -days 3650 \
  -nodes \
  -keyout test-ca.key \
  -out test-ca.crt \
  -subj "/CN=Media Archive Test CA" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" && \
openssl req -new \
  -newkey rsa:3072 \
  -sha256 \
  -nodes \
  -keyout server.key \
  -out server.csr \
  -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
  -addext "keyUsage=critical,digitalSignature,keyEncipherment" \
  -addext "extendedKeyUsage=serverAuth" && \
openssl x509 -req \
  -in server.csr \
  -CA test-ca.crt \
  -CAkey test-ca.key \
  -CAcreateserial \
  -copy_extensions copy \
  -days 365 \
  -sha256 \
  -out server.crt && \
rm server.csr && \
openssl verify -CAfile test-ca.crt server.crt
```

`test-ca.key` and `server.key` are secrets. Only `test-ca.crt` is passed to a
client. The current server authenticates users with passwords and bearer
sessions; mutual TLS client certificates are not implemented yet.

## First administrator

Bootstrap is an offline operation against the SQLite database. It succeeds only
when the database contains no users.

```bash
go run ./cmd/admin bootstrap \
  --database ./data/mediaarchive.db \
  --username archive_admin \
  --display-name "Archive Administrator"
```

The command requests and confirms the password without terminal echo. Passwords
and tokens must never be supplied as command arguments, environment variables,
or committed files.

## End-user console

### Start the console

For local HTTP:

```bash
go run ./cmd/client --server http://127.0.0.1:8080
```

For HTTPS with the test CA:

```bash
export CERTIFICATE_DIRECTORY=/path/to/go-mediaarchive-certificates && \
go run ./cmd/client \
  --server https://localhost:8443 \
  --ca-certificate "$CERTIFICATE_DIRECTORY/test-ca.crt"
```

### One-shot health check

```bash
go run ./cmd/client --server http://127.0.0.1:8080 health
```

### Interactive commands

| Command | Authentication | Purpose | Example |
| --- | --- | --- | --- |
| `help` | No | Show available commands. | `help` |
| `health` | No | Check server availability. | `health` |
| `login <username>` | No | Create an in-memory authenticated session. | `login archive_user` |
| `logout` | Yes | Revoke the current session. | `logout` |
| `me` | Yes | Show the current user identity. | `me` |
| `password enroll` | No | Set the initial password with an enrollment token. | `password enroll` |
| `password change` | Yes | Change the current user's password. | `password change` |
| `exit` | No | Attempt logout and close the console. | `exit` |
| `quit` | No | Alias for `exit`. | `quit` |
| `bye` | No | Alias for `exit`. | `bye` |

The access token exists only in process memory. EOF and a normal interrupt also
attempt to revoke an active session. The prompt identifies
the authenticated user, for example `archive_user@mediaarchive>`. Before login
and after logout it uses `anonymous@mediaarchive>`.

Invalid interactive field values repeat only the affected prompt. Values already
accepted during the current operation do not need to be entered again. API
errors still end the current operation and return to the command prompt.

## Administrator console

### Start the console

For local HTTP:

```bash
go run ./cmd/admin --server http://127.0.0.1:8080
```

For HTTPS with the test CA:

```bash
export CERTIFICATE_DIRECTORY=/path/to/go-mediaarchive-certificates && \
go run ./cmd/admin \
  --server https://localhost:8443 \
  --ca-certificate "$CERTIFICATE_DIRECTORY/test-ca.crt"
```

Only a user with the global `admin` role may remain logged in to this console.
A session created for another role is immediately revoked.

The prompt changes from `anonymous@mediaarchive-admin>` to a form such as
`archive_admin@mediaarchive-admin>` after login. Displayed timestamps use the
operating system's current local time zone and retain their numeric UTC offset.

### Interactive commands

| Command | Purpose | Example |
| --- | --- | --- |
| `help` | Show available commands. | `help` |
| `health` | Check server availability. | `health` |
| `login <username>` | Create and verify an administrator session. | `login archive_admin` |
| `logout` | Revoke the current session. | `logout` |
| `me` | Show the current administrator identity. | `me` |
| `user list [limit]` | Start at the first user page. | `user list 20` |
| `user next` | Load the next page using the in-memory cursor. | `user next` |
| `user first` | Return to the first page with the current limit. | `user first` |
| `user get <id>` | Show one user. | `user get 123e4567-e89b-12d3-a456-426614174000` |
| `user create` | Prompt for and create a user without a password. | `user create` |
| `user update <id>` | Prompt for mutable user properties. | `user update 123e4567-e89b-12d3-a456-426614174000` |
| `user activate <id>` | Allow a deactivated user to authenticate again. | `user activate 123e4567-e89b-12d3-a456-426614174000` |
| `user deactivate <id>` | Block authentication and revoke the user's sessions. | `user deactivate 123e4567-e89b-12d3-a456-426614174000` |
| `user delete <id>` | Permanently delete a user after exact username confirmation. | `user delete 123e4567-e89b-12d3-a456-426614174000` |
| `password enrollment <user-id>` | Issue a one-time initial-password token. | `password enrollment 123e4567-e89b-12d3-a456-426614174000` |
| `password change` | Change the administrator's own password. | `password change` |
| `exit`, `quit`, `bye` | Attempt logout and close the console. | `exit` |

`user list` accepts limits from 1 through 100 and uses 50 by default. The
continuation cursor is retained internally and is cleared on logout.

## Complete user enrollment scenario

Start with a running server and a bootstrapped administrator.

### 1. Create a user

```text
$ go run ./cmd/admin --server http://127.0.0.1:8080
anonymous@mediaarchive-admin> login archive_admin
Password:
Logged in as archive_admin.
archive_admin@mediaarchive-admin> user create
Username: archive_user
Display name: Archive User
Role (viewer|editor|admin): viewer
Created user archive_user (123e4567-e89b-12d3-a456-426614174000).
```

The actual generated user ID will differ from the example.

### 2. Issue an enrollment token

Use the generated user ID:

```text
archive_admin@mediaarchive-admin> password enrollment 123e4567-e89b-12d3-a456-426614174000
Enrollment token (shown once):
<one-time-enrollment-token>
Expires: <local timestamp with UTC offset>
archive_admin@mediaarchive-admin> logout
Logged out.
anonymous@mediaarchive-admin> exit
```

Transfer the token to the intended user through an appropriately protected
channel. Do not store it in shell history or project files.

### 3. Enroll the initial password

```text
$ go run ./cmd/client --server http://127.0.0.1:8080
anonymous@mediaarchive> password enroll
Enrollment token:
New password:
Confirm new password:
Password enrolled. You can now log in.
```

### 4. Log in and inspect the identity

```text
anonymous@mediaarchive> login archive_user
Password:
Logged in as archive_user.
archive_user@mediaarchive> me
ID: 123e4567-e89b-12d3-a456-426614174000
Username: archive_user
Display name: Archive User
Role: viewer
Active: true
```

### 5. Change the password

```text
archive_user@mediaarchive> password change
Current password:
New password:
Confirm new password:
Password changed. Log in again.
anonymous@mediaarchive> login archive_user
Password:
Logged in as archive_user.
archive_user@mediaarchive> exit
```

A successful password change revokes every server-side session owned by the
user, so a new login is required.

## Permanent user deletion

Use deactivation when access may need to be restored. Deletion is irreversible
and permanently removes the identity together with its password enrollment,
password credential, and server-side sessions.

The administrator console loads and displays the selected identity before
asking for its exact username. An incorrect value repeats only the confirmation
prompt. Submit an empty value to cancel without changing server data.

```text
archive_admin@mediaarchive-admin> user delete 123e4567-e89b-12d3-a456-426614174000
ID: 123e4567-e89b-12d3-a456-426614174000
Username: archive_user
Display name: Archive User
Role: viewer
Active: true
Type username "archive_user" to permanently delete this user (blank cancels): archive_user
Permanently deleted user archive_user (123e4567-e89b-12d3-a456-426614174000).
```

An administrator cannot delete their own identity or the last active
administrator. Before media ownership is introduced, its design must either
require transferring owned records or block deletion. Future audit records
must retain an independent actor snapshot and survive identity deletion.

## REST API mapping

Graphical applications do not invoke either terminal program. They communicate
directly with the same HTTPS JSON API and implement equivalent session handling.

| CLI operation | Method and endpoint | Access |
| --- | --- | --- |
| Health | `GET /api/v1/health` | Public |
| Login | `POST /api/v1/auth/sessions` | Public, rate limited |
| Logout | `DELETE /api/v1/auth/sessions/current` | Authenticated |
| Current identity | `GET /api/v1/users/me` | Authenticated |
| User lookup | `GET /api/v1/users/{id}` | Administrator |
| User directory | `GET /api/v1/users?limit={limit}&cursor={cursor}` | Administrator |
| Create user | `POST /api/v1/users` | Administrator |
| Update user | `PUT /api/v1/users/{id}` | Administrator |
| Activate or deactivate | `PUT /api/v1/users/{id}/active` | Administrator |
| Permanently delete user | `DELETE /api/v1/users/{id}` | Administrator |
| Issue enrollment | `POST /api/v1/users/{id}/password-enrollment` | Administrator |
| Complete enrollment | `POST /api/v1/auth/password-enrollments` | Public, token protected and rate limited |
| Change own password | `PUT /api/v1/users/me/password` | Authenticated |

Successful API responses use JSON except operations returning HTTP `204 No
Content`. Authenticated requests send the opaque token as:

```http
Authorization: Bearer <access-token>
```

Tokens, passwords, and enrollment secrets must not be logged or placed in URLs.

## Development checks

### Milestone checks

```bash
./scripts/check_milestone.sh
```

Include the race detector:

```bash
./scripts/check_milestone.sh --race
```

PowerShell equivalents:

```powershell
.\scripts\check_milestone.ps1
.\scripts\check_milestone.ps1 -Race
```

### Local Quality Gate

```bash
./scripts/check_ci.sh
```

Skip the race detector when it is unavailable:

```bash
./scripts/check_ci.sh --skip-race
```

Run the GitHub Actions `verify` job locally with `act` and Docker:

```bash
./scripts/check_ci.sh --act
```

PowerShell equivalents:

```powershell
.\scripts\check_ci.ps1
.\scripts\check_ci.ps1 -SkipRace
.\scripts\check_ci.ps1 -Act
```

### Individual Go checks

```bash
go fmt ./... && \
go mod tidy -diff && \
go mod verify && \
go vet ./... && \
go test -count=1 -cover ./... && \
go build ./... && \
git diff --check && \
git diff --cached --check
```

### Architecture documentation

Regenerate the package overview after changing package dependencies or package
comments:

```bash
go run ./tools/archdoc -output docs/architecture.md
```

### Release archives

Git Bash:

```bash
./scripts/build_release.sh \
  --version v0.1.0 \
  --output-directory dist && \
(cd dist && sha256sum -c SHA256SUMS)
```

PowerShell:

```powershell
.\scripts\build_release.ps1 `
  -Version v0.1.0 `
  -OutputDirectory dist
```

Local release builds do not publish files. Publishing is triggered only by an
annotated `vMAJOR.MINOR.PATCH` tag as described in [Releases](releases.md).
