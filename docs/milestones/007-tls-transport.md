# Milestone 7: TLS transport foundation

## Goal

Protect network communication with TLS before passwords, sessions, or licensed
content are exposed through the REST API. Plain HTTP remains available only for
explicit loopback development.

## Scope

- Add server certificate and private-key configuration.
- Serve HTTPS with TLS 1.3 or newer when certificate paths are configured.
- Require the certificate and private key to be configured together.
- Reject plain HTTP binding on non-loopback addresses.
- Keep the default plain HTTP address at `127.0.0.1:8080` for local development.
- Add CLI support for a custom trusted CA certificate.
- Preserve system certificate authorities when a custom CA is added.
- Validate TLS configuration before starting the server.
- Test HTTPS communication with generated, short-lived test certificates.
- Keep certificates and private keys outside version control.

## Server configuration

The server accepts:

```text
--tls-certificate <path>
--tls-private-key <path>
```

Environment fallbacks use:

```text
MEDIAARCHIVE_TLS_CERTIFICATE
MEDIAARCHIVE_TLS_PRIVATE_KEY
```

Configuration precedence remains:

```text
explicit flag > environment variable > safe built-in default
```

Both TLS paths are required together. Supplying only one path is a configuration
error. The private key is never logged.

## Plain HTTP restriction

Without TLS, the configured listener host must be an IP loopback address such as
`127.0.0.1` or `::1`. Wildcard, unspecified, hostname, and non-loopback bindings
are rejected.

This restriction prevents an accidental plaintext network deployment while
retaining a simple local development workflow. Reverse-proxy TLS termination is
outside this milestone and will require an explicit trusted-proxy design later.

## TLS policy

- Minimum protocol version: TLS 1.3.
- Certificates and private keys are supplied by the operator.
- The application does not generate persistent production certificates.
- Certificate renewal and hot reload are outside this milestone.
- Client certificates are not requested yet.

TLS authenticates the server and encrypts transport. It does not authenticate a
Media Archive user and does not grant access to metadata or content.

## CLI trust configuration

The CLI accepts an optional CA certificate path:

```text
--ca-certificate <path>
```

When set, the PEM certificate is added to the system trust pool. The option is
valid only with an `https` server URL. The CLI does not provide an option to skip
certificate verification.

## Test certificates

Tests generate short-lived certificates at runtime or place them only in
temporary directories. No reusable private key, certificate, or certificate
password is committed to the repository.

## Acceptance criteria

- [x] Server configuration requires certificate and key paths together.
- [x] Plain HTTP accepts IPv4 and IPv6 loopback listeners.
- [x] Plain HTTP rejects wildcard and non-loopback listeners.
- [x] HTTPS listeners require TLS 1.3 or newer.
- [x] The server starts and serves the health endpoint over test HTTPS.
- [x] The CLI trusts a supplied test CA while retaining system roots.
- [x] The CLI rejects custom CA configuration for plain HTTP URLs.
- [x] Certificate errors do not silently disable verification.
- [x] Logs and errors never contain private-key contents.
- [x] No certificate or private-key fixture is tracked by Git.
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

- User login and sessions
- REST user administration
- Mutual TLS client certificates
- Automatic certificate issuance or renewal
- Certificate hot reload
- TLS termination by a reverse proxy
- Public HTTP redirects
- Media upload or download
