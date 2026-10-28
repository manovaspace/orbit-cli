# orbit-cli — Agent Guide

Orbit developer platform, workspace orchestrator, and infrastructure daemon (`orbit` and `orbit-server`).

Status: `beta`.

## Architecture & Scope

Orbit CLI follows a dual-binary architecture:
- **Workstation CLI (`orbit`)**: Client tool for developers and administrators. Handles multi-repo workspace management, environment validation, diagnostics, local dev orchestration, invitation management, and HMAC `orbit staff`. Workstation commands do not hold lldap/Stalwart *admin* credentials. `orbit invite create` may SMTP-send from the workstation unless `--no-send`.
- **Infrastructure Daemon (`orbit-server`)**: Standalone edge HTTP daemon running on server infrastructure. Manages Stalwart SMTP gateways, out-of-band OTP challenges, administrator ownership verification, and developer claim provisioning.

### Core Capabilities

- **Workspace Management**: `orbit init`, `orbit sync`, `orbit status`, `orbit repair`, `orbit update` — parses `workspace.yaml` manifest and orchestrates git repositories and dependencies. `orbit status` distinguishes **not cloned** vs **gitless** (files, no `.git`). `orbit repair` copies `.git` onto gitless trees; it never `checkout -f`.
- **Gitignored assets (R2)**: `orbit assets pull|push|add|status` — private Cloudflare R2 for PDFs/PNGs listed in `orbit-assets.yaml` ([ADR-022](../../handbook/docs/orbit/decisions/022-gitignored-assets-on-r2.md)). Rebuildable `bin/` and `dist/` stay gitignored and are not stored on R2.
- **Diagnostics & Auto-Healing**: `orbit doctor` (`--fix`) — comprehensive checks for Docker, Go toolchain, Node/Bun, disk space, dev ports, git remotes, and workspace integrity with automated fixes.
- **Environment Validation**: `orbit env check` — validates local `.env` files against `.env.example` across all workspace modules.
- **50-Port Allocation**: `orbit port list`, `orbit port allocate` — 50-port blocks per ADR-006 (`1nxxx` range).
- **Workspace Migrations**: `orbit migrate` (applies pending) and `orbit migrate status`.
- **Local Dev Orchestration**: `orbit dev up|down|tier2|caddy|portal|logs`.
- **Client & Dev Onboarding**: `orbit invite`, `orbit onboard` — workstation invite tokens (does **not** create lldap users). `--resume` continues a checkpoint; `--ignore-and-remove-checkpoint` (alias `--reset`) discards it. Staff directory: `orbit staff` HMAC client → orbit-staff ([staff lifecycle](https://handbook.dev.manova.space/docs/guides/staff-lifecycle)). CLI is implemented; the staff HTTP service is not live yet.
- **Pull Request & Delivery Lifecycle**: `orbit pr create`, `orbit pr merge`, `orbit pr status`, `orbit pr deliver` — automated branch pushing, Forgejo PR creation, PR status listing, merging, and full end-to-end branch delivery with fast-forward trunk re-sync per ADR-026.
- **System Administration**: `orbit admin init` — root system ownership initialization, out-of-band email OTP challenges, and sealed vault management (`~/.config/orbit/owner.json`).

## Non-negotiables

1. **`o` commands, never `cd` loops** — every agent using orbit-cli must use `orbit` / `o` commands for workspace orchestration.
2. **`log/slog` only** — no `log.Printf`, `fmt.Println`, or other loggers anywhere.
3. **`orbit doctor --local` for research and inspection** — diagnostics never implicitly heal. Only select `--fix` when repairs are explicitly authorized; remote probes require `--remote`, and host-key acceptance additionally requires `--accept-host-keys --fix`.
4. **Port allocation via ADR-006** — never hardcode ports. Use `o port list` to inspect allocations.
5. **Sequential migrations** — `orbit migrate` only; never run raw SQL against production.
6. **`orbit staff` is HMAC-auth only** — never embed admin credentials in workstation commands.

## Engineering gotchas

- **Dual-binary architecture:** `orbit` (workstation CLI) and `orbit-server` (edge daemon) are separate binaries with separate concerns. Workstation commands must never hold lldap/Stalwart admin credentials.
- **`orbit repair` is non-destructive:** It copies `.git` onto gitless trees; it never runs `checkout -f`. Do not confuse with `orbit init`.
- **`orbit status` distinguishes states:** "not cloned" (absent dir) vs "gitless" (dir exists, no `.git`) vs "dirty" (uncommitted changes). These require different remediation.
- **`--resume` vs `--reset` in `orbit onboard`:** `--resume` continues from checkpoint; `--ignore-and-remove-checkpoint` (alias `--reset`) discards it entirely.
- **Mail templates are owned by `orbit-notifications`:** `pkg/mailtemplates` is the canonical source. Never duplicate or inline mail templates in orbit-cli.

## Commands

### Build & Test

```bash
go build -o bin/orbit ./cmd/orbit
go build -o bin/orbit-server ./cmd/orbit-server
go test ./...
```

### CLI Invocations

Run `orbit --help` / `orbit <cmd> --help`; generated reference lives in `docs/cli/` (`orbit doc -f markdown -o docs/cli`). Non-obvious:

- `orbit doctor --fix` and `orbit repair` modify the workspace; run read-only `orbit doctor --local` first.
- `orbit staff recreate` is blocked; use the Keycloak cleanup procedure.
- `orbit pr deliver` pushes, opens the PR, merges and fast-forwards in one step.
- `orbit assets add <path>` updates `orbit-assets.yaml` and `.gitignore`.

### Edge Daemon

```bash
go run ./cmd/orbit-server --addr :8080 --smtp-host mail.manova.space --smtp-port 587
```

## Structure & Packages

`cmd/orbit/` (Cobra tree), `cmd/orbit-server/` (edge daemon, mail gateway), `pkg/<name>/` one package per command area (`doctor`, `manifest`, `migrate`, `ports`, `pr`, `onboard`, `invite`, `owner`, `updater`, `client`, `staffhmac`, `config`, `env`, `provisioner`, `orchestrator`, `assets`, `tui`). `ls pkg/` and each package doc comment give the role.

## Documentation

| Topic | Path |
| --- | --- |
| Workspace model | `handbook/docs/orbit/guides/manova-workspace.md` |
| Development workflow | `handbook/docs/orbit/guides/development-workflow.md` |
| Dev port allocation | `handbook/docs/orbit/architecture/orbit-dev-ports.md` |
| Module catalog | `handbook/docs/orbit/architecture/module-catalog.md` |
| Platform owner init runbook | `handbook/docs/orbit/guides/orbit-admin-init.md` |
| Staff lifecycle (`orbit staff`) | `handbook/docs/orbit/guides/staff-lifecycle.md` |
| Generated CLI markdown | `orbit/orbit-cli/docs/cli/` (`orbit doc -f markdown`) |
| Generated man pages | `orbit/orbit-cli/docs/cli/man/` (`orbit doc -f man`) |
| Ownership & email delivery (repo) | `orbit/orbit-cli/docs/guides/platform-ownership-and-email-delivery.md` |

## Session pre-flight

1. `go build -o bin/orbit ./cmd/orbit` — confirm clean build before any changes
2. `rtk proxy bash scripts/check-agent-cli.sh` — check the installed diagnostic interface using version/help only; if it fails, validate the source build before installing an update
3. `rtk o doctor --local` — inspect local toolchain without remote probes or repairs, after the compatibility check passes
4. Optional: `rtk o status orbit` — branch state for this scope

## Definition of done

- [ ] `go build -o bin/orbit ./cmd/orbit` exits 0
- [ ] `go test ./...` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `orbit doctor` shows no errors on clean workspace
- [ ] Docs updated in `handbook/docs/orbit/guides/` if CLI commands changed

## Do / don't


- Invite and owner-challenge mail HTML/text come from the pinned `github.com/manovaspace/orbit-notifications/pkg/mailtemplates` module. Standalone builds must resolve it without a sibling replacement. Do not add HTML strings in `pkg/invite`.
- Invite curl host is `https://orbit.manova.space` (not `get.manova.space`).
- Never put the HMAC token or OTP in the email subject.
- Use `pkg/tui` Lipgloss styling for output formatting — do not write unformatted console prints.
- Keep workstation CLI commands as pure API clients — do not embed server SMTP credentials in workstation commands.
- Adhere strictly to ADR-006 50-port block allocations (`1nxxx` range).
- Never log OTP challenge codes or master cryptographic signing secrets in stdout/stderr.
- No commit unless user asks.
