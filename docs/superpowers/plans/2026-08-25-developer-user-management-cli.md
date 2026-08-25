# Developer User Management CLI (`manova user`) — Implementation Plan

> **For agents:** Use the `executing-plans` skill to implement this plan task-by-task.

**Goal:** Implement full lifecycle developer account management (`manova user list`, `inspect`, `lock`, `unlock`, `deprovision`, `rotate-key`) with atomic subsystem coordination and table/JSON formatting.

**Architecture:**
- `pkg/user` contains the data models and `UserManager` engine interacting with `pkg/provisioner` and local / edge state.
- `cmd/manova/user.go` implements the Cobra CLI commands registered in the `System & Tooling` group.
- Automatically generates updated man pages (`manova-user.1`, `manova-user-list.1`, etc.).

**Spec:** `docs/superpowers/specs/2026-08-25-developer-user-management-cli.md`

---

## Task 1: `pkg/user` Data Models & Manager Engine

**Files:**
- Create: `pkg/user/types.go`
- Create: `pkg/user/manager.go`
- Create: `pkg/user/manager_test.go`

### Tests to write:
- `TestListUsers_Filtering`: Test filtering by `active`, `locked`, `all`.
- `TestGetUser_NotFoundAndFound`: Test looking up by email and UID.
- `TestLockAndUnlockUser`: Verify status transition from active to locked to active.
- `TestDeprovisionUser_AtomicCleanup`: Verify zero-leak removal of LDAP, Git, and WireGuard credentials.
- `TestRotateKey_GeneratesNewInvite`: Verify old credentials invalidated and new invite returned.

---

## Task 2: CLI Subcommands (`cmd/manova/user.go`) & Registration

**Files:**
- Create: `cmd/manova/user.go`
- Create: `cmd/manova/user_test.go`
- Modify: `cmd/manova/main.go`

### Subcommands:
1. `manova user list`: Table and JSON output with status filtering.
2. `manova user inspect <user>`: Comprehensive user diagnostics card.
3. `manova user lock <user>`: Lock account with reason.
4. `manova user unlock <user>`: Restore account access.
5. `manova user deprovision <user>`: Prompt confirmation (or `--yes`) and purge account.
6. `manova user rotate-key <user>`: Rotate credentials and output claim instructions.

---

## Task 3: Test Suite & End-to-End Verification

- Run `go test ./pkg/user/... -v` and `go test ./cmd/manova -run TestUser -v`.
- Run full test suite `go test ./... -v`.
- Verify man page generation includes `manova-user*.1`.
