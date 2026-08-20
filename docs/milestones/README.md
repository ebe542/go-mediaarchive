# Milestones

Milestones divide development into small, executable, and independently
verifiable increments.

| Tag | Milestone | Status |
| --- | --- | --- |
| `milestone-001` | [Executable foundation](001-foundation.md) | Complete |
| `milestone-002` | [CLI health client](002-cli-health-client.md) | Complete |
| `milestone-003` | [SQLite foundation](003-sqlite-foundation.md) | Complete |
| `milestone-004` | [User identity foundation](004-user-identity-foundation.md) | Complete |
| `milestone-005` | [User application service](005-user-application-service.md) | Complete |
| `milestone-006` | [Administrator credential bootstrap](006-admin-bootstrap.md) | Complete |
| `milestone-007` | [TLS transport foundation](007-tls-transport.md) | Complete |
| `milestone-008` | [Authentication sessions](008-authentication-sessions.md) | Complete |
| `milestone-009` | [Role-based authorization foundation](009-role-authorization.md) | In progress |

## Versioning rule

A milestone may contain multiple complete Conventional Commits. The completion
point is identified by an immutable annotated Git tag named `milestone-NNN`.
Commit counts are not used as milestone numbers.

A milestone tag is created only when:

1. all documented acceptance criteria are complete;
2. the standard local milestone checks pass;
3. GitHub Actions passes on `main`;
4. the milestone documentation reflects the published implementation.

Published milestone tags are never moved. Subsequent corrections are recorded
in new commits so that the project history remains auditable.

The final implementation commit includes the completed milestone documentation.
A separate documentation-only completion commit is not required. After GitHub
Actions passes for that commit, the milestone tag is created directly.
