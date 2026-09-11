# Milestone 17: Media application services

## Goal

Coordinate media metadata and grants while enforcing global creation roles and
media-specific authorization in one storage-independent application boundary.

## Scope

- Add an application service for creating, reading, updating, and deleting
  media metadata.
- Add an application service for replacing, inspecting, listing, and revoking
  per-user grants.
- Assign media ownership from the authenticated actor rather than caller input.
- Restrict media creation to active editors and administrators.
- Resolve only the requesting actor's grant for authorization decisions.
- Mask undiscoverable and unauthorized media as not found.
- Keep HTTP, CLI, search, file storage, and content transfer outside this
  milestone.

## Metadata operations

### Create

An active `editor` or `admin` may create a media identity. The service generates
the media ID and timestamps and assigns the actor as owner. Caller-controlled
input cannot select another owner or provide lifecycle timestamps.

The global role authorizes creation only. It does not grant access to media
owned by another user.

### Read

Reading metadata requires the `discover` permission. The service loads the item
and, for a non-owner, only the grant identified by media ID and actor ID.

An unknown item and an item the actor cannot discover produce the same
application error. This prevents callers from enumerating hidden media IDs.

### Update

Updating metadata requires `update`. The complete mutable metadata and ordered
author list are replaced. The existing owner, media ID, and creation timestamp
are retained; the service supplies a new update timestamp.

### Delete

Deleting metadata requires `delete`. The repository explicitly removes grants,
authors, and the media identity. No file content exists at this application
stage.

## Grant operations

### Replace permissions

Granting permissions requires `share`. The target user must exist and be active.
The supplied non-empty permission set replaces the target user's complete
existing set. A grant for the owner is rejected because owner permissions are
implicit.

### Inspect and list

Reading one grant or listing all grants for an item requires `share`. Results
remain internal application data until a later administrator API milestone.

### Revoke

Revoking permissions requires `share` and explicitly deletes the target grant.
An empty permission set is never interpreted as revocation.

## Authorization boundary

Application services are the only orchestration layer that combines:

- the authenticated user identity;
- the media identity and owner;
- the actor's optional explicit grant;
- the requested media permission;
- the media and grant repositories.

For authorization, the service asks the grant repository only for
`(media_id, actor_id)`. `ErrGrantNotFound` means the actor has no explicit grant;
other repository errors remain operational failures. The existing pure domain
policy performs the final decision.

Handlers, clients, repositories, and future filesystem components must not
duplicate these decisions.

## Error behavior

- `ErrCreationForbidden` rejects inactive actors and roles without creation
  authority.
- `ErrMediaNotFound` represents both unknown and unauthorized media.
- `ErrOwnerGrant` rejects redundant grants for the media owner.
- `ErrInactiveGrantee` rejects new or replacement grants for inactive users.
- Invalid media input and permission sets preserve their domain errors through
  wrapping so callers can classify them with `errors.Is`.
- Infrastructure errors are wrapped with operation context without exposing
  credentials, grants, or internal paths.

## Implementation sequence

1. Define application operations, authorization boundaries, and error behavior.
2. Add the authorized media metadata service and tests.
3. Add the authorized grant service and tests.

Each step is delivered as a complete Conventional Commit. Tests remain with the
behavior they specify rather than forming a separate test-only commit.

## Acceptance criteria

- [x] Metadata and grant use cases are documented.
- [x] Global creation roles are separated from media-specific permissions.
- [x] Unauthorized media is indistinguishable from unknown media.
- [x] Only active editors and administrators can create media.
- [x] The authenticated creator becomes owner without caller override.
- [x] Metadata reads require `discover`.
- [x] Metadata updates preserve owner and creation time and require `update`.
- [x] Metadata deletion requires `delete`.
- [x] Authorization loads at most one actor-specific grant.
- [ ] Grant replacement and inspection require `share`.
- [ ] Grant recipients must exist and be active.
- [ ] Owner grants and empty permission sets are rejected.
- [ ] Grant revocation explicitly deletes the stored grant.
- [ ] Standard milestone checks pass.
- [ ] Local quality gate checks pass.

GitHub Actions passing on `main` is the external gate for creating the immutable
`milestone-017` tag after all milestone commits are complete.

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

- REST API and typed-client operations
- CLI media commands
- Search and pagination
- File upload, streaming, and download
- Server filesystem locations
- Ownership transfer
- Self-service account deletion
- Group- or role-based media grants
- Audit-log persistence
