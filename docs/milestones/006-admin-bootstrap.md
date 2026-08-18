# Milestone 6: Administrator credential bootstrap

## Goal

Provide a secure local process for creating the first administrator identity and
password credential. This establishes an initial trusted account without
exposing user administration or credentials through the REST API.

## Scope

- Define password credential domain and persistence models separately from users.
- Hash passwords with Argon2id using explicit, versioned parameters.
- Encode password hashes in a self-describing PHC-style string.
- Verify passwords without storing or logging plaintext values.
- Add migration `003_create_password_credentials.sql`.
- Store one password credential per user initially.
- Create the first administrator and credential atomically.
- Reject bootstrap when any user already exists.
- Add a local administrative CLI bootstrap command.
- Read passwords interactively without terminal echo.
- Confirm the password before performing database writes.
- Keep all REST routes unchanged.

## Security model

The bootstrap command operates directly on a local SQLite database. It is an
administrative maintenance tool, not a remote API operation.

Passwords must not be accepted through command-line arguments or environment
variables because both may be exposed through process inspection, shell history,
diagnostics, or inherited environments. The command reads the password from a
terminal with echo disabled and requests a second matching entry.

Plaintext passwords exist only for the shortest practical duration in process
memory. They are never persisted, logged, returned in errors, or included in
test fixtures that resemble real credentials.

Bootstrap passwords contain between 15 and 1,024 Unicode code points. The lower
bound follows NIST SP 800-63B for single-factor passwords; the generous upper
bound permits passphrases while preventing unbounded input. No character-class
composition rules, case conversion, trimming, normalization, or truncation are
applied.

## Password hashing

The initial password hasher uses Argon2id with 19 MiB of memory, two iterations,
one degree of parallelism, a 16-byte salt, and a 32-byte derived key. These
parameters meet the current OWASP minimum recommendation and remain explicit so
deployment-specific benchmarking can guide later increases. Every stored
encoding contains the algorithm, version, parameters, salt, and derived key so
future verification and parameter upgrades remain possible.

Cryptographic randomness is obtained from `crypto/rand`. Tests inject
deterministic bytes only to verify encoding behavior; production code must never
use deterministic salts.

Password comparison uses constant-time comparison after deriving the candidate
hash. Malformed or unsupported encodings are rejected safely.

## Persistence model

Migration `003_create_password_credentials.sql` creates a separate credential
table linked to `users` by a foreign key. It contains:

- `user_id` as the primary key and user reference;
- the encoded Argon2id password hash;
- creation and update timestamps.

The table contains no plaintext password, password hint, recoverable encryption
key, token, or certificate identity.

## Atomic bootstrap

The SQLite bootstrap operation runs in one transaction:

1. verify that no user exists;
2. insert the administrator identity;
3. insert its password credential;
4. commit both records together.

Any failure rolls back both inserts. A recognizable error reports that bootstrap
has already been completed without revealing account details.

## Command behavior

The local administrative executable provides a command shaped as:

```text
mediaarchive-admin bootstrap \
  --database ./data/mediaarchive.db \
  --username archive-admin \
  --display-name "Archive Administrator"
```

The password is requested interactively. Successful output confirms creation
without printing the password hash or other credential material.

## Acceptance criteria

- [x] Password tests cover hashing, verification, malformed encodings, and wrong passwords.
- [x] Production salts use cryptographically secure randomness.
- [x] Password hashes use a self-describing Argon2id encoding.
- [x] Migration `003_create_password_credentials.sql` enforces the user relationship.
- [x] Credential persistence never stores plaintext passwords.
- [x] Bootstrap creates the first admin and credential in one transaction.
- [x] Bootstrap rollback leaves neither record after a failure.
- [x] A second bootstrap attempt returns a recognizable error.
- [x] The administrative CLI reads and confirms a password without terminal echo.
- [x] Passwords and hashes are absent from command output and errors.
- [x] No user-administration or authentication HTTP routes are registered.
- [x] Standard milestone checks pass.
- [x] Local CI checks pass.
- [ ] GitHub Actions passes on `main`.

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

- REST login endpoints
- Access or refresh tokens
- Password changes and resets
- Multiple credential types per user
- mTLS client certificates
- Remote user administration
- Media ownership and permissions
- Audit logging
