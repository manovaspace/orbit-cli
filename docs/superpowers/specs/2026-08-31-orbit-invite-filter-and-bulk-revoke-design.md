# Design Specification: Orbit Invite List Filtering and Bulk Revocation

## 1. Overview
This specification details changes to `orbit-cli` (`o invite`) to:
1. **Filter by default in `orbit invite list`**: Hide revoked and expired invitations by default, showing only `active` invitations unless `--all` (`-a`) is explicitly provided.
2. **Add bulk revocation in `orbit invite revoke --all`**: Enable revoking all active invitations simultaneously with `--all` (`-a`).

## 2. CLI Command Changes

### 2.1 `orbit invite list`
- **Flag `--all` / `-a`**: Default value changes from `true` to `false`.
- **Default View (`all=false`)**:
  - Filters stored invitations to only records with `Status() == "active"`.
  - If no active invitations exist, displays:
    `No active invitations found. Use --all (-a) to view expired or revoked invitations, or 'orbit invite create <email>' to generate one.`
- **All View (`all=true`)**:
  - Displays all records regardless of status (`active`, `revoked`, `expired`).
- **JSON Output (`--format json`)**:
  - When `all=false`, outputs JSON array containing only active records.
  - When `all=true`, outputs JSON array containing all records.

### 2.2 `orbit invite revoke`
- **Flag `--all` / `-a`**: Added boolean flag (`Revoke all active developer onboarding invitations`).
- **Arguments Validation**:
  - If `--all` is set: no positional argument required.
  - If `--all` is NOT set: exactly 1 positional argument (`<token_or_id>`) required.
  - If neither is provided, returns:
    `invite token or ID required (or use --all to revoke all active invitations)`
- **Behavior with `--all`**:
  - Revokes all unrevoked invitations in the store atomically.
  - Outputs summary of revoked invitations (count, IDs/emails).
  - If no active invitations exist, cleanly reports `No active invitations to revoke.`

## 3. Storage Layer Updates

### 3.1 `pkg/invite/store.go`
Add `RevokeAllInvites() ([]*InviteRecord, error)`:
- Scans all records loaded from disk.
- Sets `Revoked = true` and `RevokedAt = time.Now().UTC()` on all non-revoked records.
- Saves changes atomically.
- Returns the list of mutated `InviteRecord`s.

### 3.2 `pkg/serverstore/sqlite/invites.go` & `pkg/serverstore/store.go`
Add `RevokeAll(ctx context.Context) ([]*invite.InviteRecord, error)`:
- Executes `UPDATE invites SET revoked = 1, revoked_at = ? WHERE revoked = 0`.
- Returns mutated records.

## 4. Verification Plan
- Unit tests in `pkg/invite/store_test.go` and `pkg/serverstore/sqlite/invites_test.go`.
- CLI integration tests in `cmd/orbit/invite_test.go` covering:
  - Default list showing only active items.
  - List with `--all` showing revoked and expired items.
  - JSON format with and without `--all`.
  - Revoke single token vs `revoke --all`.
  - Revoke `--all` when 0 active invites exist.
- Man page and CLI doc updates.
