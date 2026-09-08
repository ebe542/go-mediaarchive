# Milestone 13: Interactive command-line tools

## Goal

Provide complete terminal applications for end users and administrators while
keeping the HTTPS JSON API independent for future desktop and web clients.

## Scope

- Expand the typed Go client to cover every currently published REST endpoint.
- Turn `mediaarchive` into an interactive end-user console.
- Turn `mediaarchive-admin` into an interactive administrator console while
  preserving its offline bootstrap mode.
- Keep bearer tokens only in process memory.
- Read passwords and enrollment tokens interactively without terminal echo.
- Revoke an active session on logout and normal console exit.
- Document every executable, console command, script, and API workflow.

## Architecture

The terminal programs and future graphical clients are independent adapters of
the same REST API:

```text
End-user CLI ----\
Admin CLI --------+-- HTTPS + JSON --> REST API --> application services
Desktop GUI ------+
Web backend ------/
```

Neither CLI is a proxy or prerequisite for another client. Authorization and
all business rules remain server-side.

## End-user console

Starting `mediaarchive` without a command opens an interactive console. The
existing one-shot `health` command remains compatible.

```text
help
health
login <username>
logout
me
password enroll
password change
exit
quit
bye
```

## Administrator console

Starting `mediaarchive-admin` without `bootstrap` opens an interactive console.
The existing offline bootstrap command remains compatible.

```text
help
health
login <username>
logout
me
user list [limit]
user next
user first
user get <id>
user create
user update <id>
user activate <id>
user deactivate <id>
password enrollment <user-id>
password change
exit
quit
bye
```

Complex mutations collect their values through explicit prompts. The user page
cursor is retained only in memory so administrators do not need to copy it.

## Session and secret rules

- Access tokens are never accepted as command arguments or environment values.
- Access tokens are not persisted to disk.
- Passwords and enrollment tokens are entered without terminal echo.
- A second login requires the current session to be logged out first.
- `logout` revokes the current server-side session.
- `exit`, `quit`, `bye`, EOF, and normal interrupt handling attempt logout.
- A successful password change clears local authentication immediately because
  the server revokes every session owned by that user.
- Process termination without cleanup leaves the server-side idle and absolute
  session limits as the final safeguard.

## Implementation sequence

1. Expand the typed REST client and its transport tests.
2. Add the interactive end-user console and command tests.
3. Add remote interactive administrator commands and tests.
4. Add the central command reference and complete the milestone documentation.

Each step is delivered as a complete Conventional Commit. Acceptance criteria
are checked continuously only after implementation and tests prove them.

## Acceptance criteria

- [x] The typed client covers every currently published REST endpoint.
- [x] API errors retain HTTP status, stable code, and safe message.
- [x] JSON response content types and required fields are validated.
- [x] The end-user console supports its documented commands.
- [x] The administrator console supports its documented commands.
- [x] Offline administrator bootstrap remains compatible.
- [x] Passwords and enrollment tokens use hidden interactive input.
- [x] Bearer tokens remain in memory and are never command arguments.
- [x] Logout and normal console exit revoke active sessions.
- [x] Password changes clear local authentication.
- [x] Pagination state remains in memory and supports first/next navigation.
- [ ] A central command reference documents complete example workflows.
- [ ] Standard milestone checks pass.
- [ ] Local CI checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-013` tag; it is verified after the final milestone commit.

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

- Persistent session files
- Non-interactive authenticated automation
- JSON CLI output mode
- A separate scenario executable
- Desktop or web user interfaces
- User deletion
- Media-management commands
