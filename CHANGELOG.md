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
- Administrator-only user directory with bounded keyset pagination.
- One-time password enrollment and authenticated password changes with
  automatic session revocation.
- Interactive end-user and administrator CLI applications with in-memory
  sessions, user-aware prompts, retryable field input, local-time presentation,
  and complete user-management commands.
- Generated Go package architecture and SQLite database documentation.
- Quality Gate and semantic-version release automation.

[Unreleased]: https://github.com/ebe542/go-mediaarchive/compare/milestone-010...HEAD
