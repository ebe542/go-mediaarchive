# Milestone 22: Administrator Audit Console

## Goal

Allow administrators to inspect the existing security audit directory from the
interactive administrator console without constructing HTTP requests or
handling opaque cursors directly.

## Scope

- Add authenticated `audit list`, `audit next`, and `audit first` commands.
- Prompt for optional time, actor, event-type, and outcome filters.
- Repeat only the affected prompt after invalid filter input.
- Keep audit pagination separate from user-directory pagination.
- Render event timestamps in the operating system's local time zone.
- Display anonymous actors, targets, safe reason codes, and optional ranges.
- Clear retained audit filters and cursors when authentication state is cleared.
- Document the commands and an administrator audit-inspection scenario.

The REST endpoint, typed client operation, immutable SQLite event storage, and
audit security policy remain the responsibility of Milestone 21.

## Command contract

| Command | Behavior |
| --- | --- |
| `audit list [limit]` | Prompt for filters and load the first matching page. |
| `audit next` | Load the continuation page with the retained filters and limit. |
| `audit first` | Reload the first page with the retained filters and limit. |

The optional page limit is from 1 through 100 and defaults to 50. Time filters
use RFC3339 with an explicit UTC offset. Empty prompts leave a filter
unrestricted. Cursors remain opaque and are never entered or displayed by the
administrator.

## Security properties

- Every audit command requires a verified administrator session.
- The console calls the existing validated typed client and does not duplicate
  HTTP, JSON, cursor, or response validation.
- The access token and opaque continuation cursor are retained only in process
  memory and are not printed.
- Output contains only the bounded audit snapshot fields returned by the API.
- Listing events remains self-auditing through `audit.events_listed`.
- Logout and password replacement clear the active filter and cursor.

## Implementation sequence

1. Extend the administrator console client boundary and command routing.
2. Add validated filter prompts and independent pagination state.
3. Render local timestamps and bounded event details.
4. Cover filtering, pagination, retry behavior, output, and state clearing.
5. Complete command documentation and quality gates.

## Acceptance criteria

- [x] An authenticated administrator can start a filtered audit listing.
- [x] Invalid prompted filter values repeat only their own prompt.
- [x] Audit pagination retains filters without exposing cursor contents.
- [x] User and audit pagination state are independent.
- [x] Event output uses local timestamps and identifies anonymous actors.
- [x] Optional reason and range fields are rendered only when present.
- [x] Logout and password changes clear retained audit pagination state.
- [x] Command help and examples document the audit workflow.
- [x] Standard project checks pass.
- [x] Local quality gate checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-022` tag after the implementation commit is complete.

## Verification

```console
go run ./cmd/projectctl quality-gate
```

## Out of scope

- Audit export formats
- Retention deletion and archival policy
- Cryptographic hash chains or signed checkpoints
- External SIEM, syslog, or remote append-only storage
- Source-IP persistence and trusted-proxy handling
- Interactive command history
- Graphical administrator interfaces
