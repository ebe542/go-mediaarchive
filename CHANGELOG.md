# Changelog

All notable changes to this project are documented in this file.

The format is based on Keep a Changelog, and this project adheres to Semantic
Versioning for product releases. Milestone tags remain independent development
checkpoints.

## [Unreleased]

### Added

- Versioned JSON health, authentication, and user-management APIs.
- SQLite migrations and repositories for users, password credentials, and
  server-side sessions.
- Administrator bootstrap with Argon2id password hashing.
- TLS 1.3 server transport and explicit client certificate trust.
- Opaque authentication sessions with login throttling and revocation.
- Role-based authorization and protected user operations.
- One-time password enrollment and authenticated password changes with
  automatic session revocation.
- CLI applications for server administration and health checks.
- Generated Go package architecture and SQLite database documentation.
- Quality Gate and semantic-version release automation.

[Unreleased]: https://github.com/ebe542/go-mediaarchive/compare/milestone-010...HEAD
