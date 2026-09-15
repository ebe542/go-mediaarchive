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
- Safe administrator-only user deletion with transactional authentication-data
  removal, last-administrator protection, and explicit console confirmation.
- Minimal media identities, ordered authors, and compact per-user permission
  grants backed by strict transactional SQLite repositories.
- Storage-independent media application services with centralized object-level
  authorization and indistinguishable unknown and unauthorized media.
- Authenticated media metadata and per-user grant REST endpoints with strict
  JSON representations and a typed Go client for every operation.
- Media ownership protection for user deletion with transactional cleanup of
  grants held on other users' media.
- Interactive end-user and administrator CLI applications with in-memory
  sessions, user-aware prompts, retryable field input, local-time presentation,
  and complete user-management commands.
- Generated Go package architecture and SQLite database documentation.
- Cross-platform Go project tooling for Quality Gate checks, reproducible
  release archives, checksums, and semantic-version release validation.

[Unreleased]: https://github.com/ebe542/go-mediaarchive/compare/milestone-010...HEAD
