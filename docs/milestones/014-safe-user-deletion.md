# Milestone 14: Safe user deletion

## Goal

Complete the initial user lifecycle with explicit, irreversible administrator
deletion while preserving administrative access and future data ownership.

## Scope

- Add transactional user deletion to the user repository.
- Delete password enrollments, sessions, and password credentials explicitly.
- Prevent an administrator from deleting their own identity.
- Prevent deletion of the last active administrator.
- Expose administrator-only deletion through the versioned REST API.
- Add confirmed deletion to the interactive administrator console.
- Document irreversible deletion and future ownership constraints.

## Safety rules

- Deactivation remains the reversible way to block authentication.
- Deletion permanently removes the current user identity and credentials.
- Self-deletion is rejected by the application service.
- The repository checks the last-active-administrator invariant within the same
  transaction that deletes the user.
- Related authentication records are deleted explicitly before the user row.
- Unknown user IDs produce the existing user-not-found error.
- Future media records must require ownership transfer or block deletion.
- Future audit records must survive user deletion and retain an independent
  actor snapshot rather than cascading from the user row.

## API

```http
DELETE /api/v1/users/{id}
Authorization: Bearer <administrator-token>
```

A successful deletion returns HTTP `204 No Content`.

## Administrator command

```text
user delete <id>
```

The console loads the target identity and requires its exact username as
confirmation. An incorrect confirmation repeats only the confirmation prompt.

## Implementation sequence

1. Add application and transactional SQLite deletion.
2. Expose deletion through the REST API and typed client.
3. Add confirmed administrator-console deletion and documentation.

Each step is delivered as a complete Conventional Commit.

## Acceptance criteria

- [x] The application rejects administrator self-deletion.
- [x] Unknown and invalid user IDs are rejected safely.
- [x] Authentication records and the user are deleted atomically.
- [x] The last active administrator cannot be deleted.
- [x] Concurrent administrator deletions preserve one active administrator.
- [ ] Only administrators can use the deletion endpoint.
- [ ] Successful API deletion returns HTTP 204.
- [ ] The typed client supports user deletion.
- [ ] The administrator console requires exact username confirmation.
- [ ] Invalid confirmation repeats only the affected prompt.
- [ ] Command and lifecycle documentation describes permanent deletion.
- [ ] Standard milestone checks pass.
- [ ] Local CI checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-014` tag after all milestone commits are complete.

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

- User restoration
- Username reuse after retained media or audit data exists
- Media ownership transfer
- Audit-log persistence
- Bulk deletion
