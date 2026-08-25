# Developer User Management CLI (`manova user`) — Spec

**Goal:** Provide full developer lifecycle management (list, inspect, lock, unlock, deprovision, and rotate keys) directly from the Manova CLI across LLDAP, Forgejo, and WireGuard subsystems.

---

## 1. Domain Model & Architecture

### Data Models (`pkg/user/types.go`)
```go
type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusLocked   UserStatus = "locked"
	UserStatusArchived UserStatus = "archived"
)

type DeveloperUser struct {
	UID          string     `json:"uid"`
	Email        string     `json:"email"`
	DisplayName  string     `json:"display_name"`
	Role         string     `json:"role"` // admin, developer, readonly
	Status       UserStatus `json:"status"`
	WireGuardIP  string     `json:"wireguard_ip,omitempty"`
	WireGuardKey string     `json:"wireguard_pubkey,omitempty"`
	ForgejoUser  string     `json:"forgejo_username,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
}
```

### UserManager Engine (`pkg/user/manager.go`)
Interface and implementation to manage users against local mock / edge provisioner / state:
```go
type UserManager interface {
	ListUsers(ctx context.Context, statusFilter string) ([]DeveloperUser, error)
	GetUser(ctx context.Context, identifier string) (*DeveloperUser, error)
	LockUser(ctx context.Context, identifier string, reason string) error
	UnlockUser(ctx context.Context, identifier string) error
	DeprovisionUser(ctx context.Context, identifier string) (*DeprovisionSummary, error)
	RotateKey(ctx context.Context, identifier string) (*invite.InviteResponse, error)
}
```

---

## 2. CLI Commands (`cmd/manova/user.go`)

```text
System & Tooling:
  user        Manage developer accounts, roles, and provisioned credentials
```

### Subcommands:
- `manova user list [--status active|locked|all] [--format table|json]`
  Renders colorized table with `UID`, `NAME`, `EMAIL`, `ROLE`, `STATUS`, `VPN IP`, `GIT`.
- `manova user inspect <email_or_uid> [--json]`
  Detailed user credentials, groups, VPN configuration, and SSH key diagnostics.
- `manova user lock <email_or_uid> [--reason ...]`
  Freezes LLDAP, disables WireGuard peer, and deactivates Git access.
- `manova user unlock <email_or_uid>`
  Restores user access across all subsystems.
- `manova user deprovision <email_or_uid> [--yes]`
  Atomic zero-leak offboarding: frees VPN IP, removes Git keys, deletes LLDAP entry, revokes tokens.
- `manova user rotate-key <email_or_uid>`
  Revokes old credentials and issues a fresh onboarding token for re-claiming.

---

## 3. Files Touched

| File | Change |
|---|---|
| `orbit/orbit-cli/pkg/user/types.go` | Data models and constants for users |
| `orbit/orbit-cli/pkg/user/manager.go` | Core user management engine with atomic rollback |
| `orbit/orbit-cli/pkg/user/manager_test.go` | Unit tests for list, inspect, lock, unlock, deprovision |
| `orbit/orbit-cli/cmd/manova/user.go` | CLI subcommands (`list`, `inspect`, `lock`, `unlock`, `deprovision`, `rotate-key`) |
| `orbit/orbit-cli/cmd/manova/user_test.go` | CLI integration tests |
| `orbit/orbit-cli/cmd/manova/main.go` | Register `user` command in `System & Tooling` group |
