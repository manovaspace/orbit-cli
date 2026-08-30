# orbit-cli — Agent Guide

Orbit developer platform, workspace orchestrator, and infrastructure daemon (`orbit` and `orbit-server`).

Status: `beta`.

## Architecture & Scope

Orbit CLI follows a dual-binary architecture:
- **Workstation CLI (`orbit`)**: Client tool for developers and administrators. Handles multi-repo workspace management, environment validation, diagnostics, local dev orchestration, invitation management, and HMAC `orbit staff`. Workstation commands do not hold lldap/Stalwart *admin* credentials. `orbit invite create` may SMTP-send from the workstation unless `--no-send`.
- **Infrastructure Daemon (`orbit-server`)**: Standalone edge HTTP daemon running on server infrastructure. Manages Stalwart SMTP gateways, out-of-band OTP challenges, administrator ownership verification, and developer claim provisioning.

### Core Capabilities

- **Workspace Management**: `orbit init`, `orbit sync`, `orbit status`, `orbit update` — parses `workspace.yaml` manifest and orchestrates git repositories and dependencies.
- **Gitignored assets (R2)**: `orbit assets pull|push|add|status` — private Cloudflare R2 for PDFs/PNGs listed in `orbit-assets.yaml` ([ADR-022](../../handbook/docs/orbit/decisions/022-gitignored-assets-on-r2.md)). Rebuildable `bin/` and `dist/` stay gitignored and are not stored on R2.
- **Diagnostics & Auto-Healing**: `orbit doctor` (`--fix`) — comprehensive checks for Docker, Go toolchain, Node/Bun, disk space, dev ports, git remotes, and workspace integrity with automated fixes.
- **Environment Validation**: `orbit env check` — validates local `.env` files against `.env.example` across all workspace modules.
- **50-Port Allocation**: `orbit port list`, `orbit port allocate` — 50-port blocks per ADR-006 (`1nxxx` range).
- **Workspace Migrations**: `orbit migrate` (applies pending) and `orbit migrate status`.
- **Local Dev Orchestration**: `orbit dev up|down|tier2|caddy|portal|logs`.
- **Client & Dev Onboarding**: `orbit invite`, `orbit onboard` — workstation invite tokens (does **not** create lldap users). `--resume` continues a checkpoint; `--ignore-and-remove-checkpoint` (alias `--reset`) discards it. Staff directory: `orbit staff` HMAC client → orbit-staff ([staff lifecycle](https://handbook.dev.manova.space/docs/guides/staff-lifecycle)). CLI is implemented; the staff HTTP service is not live yet.
- **System Administration**: `orbit admin init` — root system ownership initialization, out-of-band email OTP challenges, and sealed vault management (`~/.config/orbit/owner.json`).

## Commands

### Build & Test

```bash
go build -o bin/orbit ./cmd/orbit
go build -o bin/orbit-server ./cmd/orbit-server
go test ./...
```

### CLI Invocations

```bash
orbit doctor              # run system and workspace diagnostics
orbit doctor --fix        # auto-heal detected issues
orbit init                # initialize workspace from workspace.yaml
orbit sync                # synchronize repositories and dependencies
orbit status              # multi-repo git status and dirty tree check
orbit env check           # validate environment variables against .env.example
orbit env setup           # generate missing .env files from schemas
orbit port list           # display dev port allocations (ADR-006)
orbit port allocate <project> <service>
orbit migrate             # apply pending workspace migrations
orbit migrate status      # inspect applied migrations
orbit dev up              # start local dev stack services
orbit invite create <email> # issue HMAC-signed onboarding invite
orbit onboard             # interactive onboarding wizard
orbit staff create --uid … --name … --forward …
orbit staff recreate --uid … --name … --forward … [--totp]
orbit staff reset-password <uid> [--ldap|--mailbox|--totp]
orbit assets pull         # download gitignored media from R2
orbit assets push         # upload gitignored media to R2
orbit assets add <path>   # upload a file, update orbit-assets.yaml + .gitignore
orbit assets status       # compare local assets vs R2
orbit admin init          # initiate platform ownership verification
orbit doc -f markdown -o docs/cli
orbit doc -f man -o docs/cli/man
orbit config show         # local CLI config
orbit self-update
orbit uninstall
```

### Edge Daemon

```bash
go run ./cmd/orbit-server --addr :8080 --smtp-host mail.manova.space --smtp-port 587
```

## Structure & Packages

| Package / Path | Role |
| --- | --- |
| `cmd/orbit/` | Main CLI entrypoint and Cobra subcommand tree (`init`, `doctor`, `dev`, `invite`, etc.) |
| `cmd/orbit-server/` | Standalone infrastructure daemon HTTP listener and mail gateway |
| `pkg/doctor/` | Workspace diagnostics, runtime health checks, and auto-healing engine |
| `pkg/manifest/` | `workspace.yaml` parsing and multi-repo dependency resolution |
| `pkg/migrate/` | Workspace schema, config, and structure migration engine |
| `pkg/ports/` | Dev host port block allocation (ADR-006) and conflict detection |
| `pkg/onboard/` | Developer and client onboarding flow and claim verification |
| `pkg/session/` | Onboarding session state (`~/.config/orbit/session.json`; reads legacy `~/.config/manova/session.json`) |
| `pkg/invite/` | Cryptographic HMAC invite tokens, token store, and SMTP mailer |
| `pkg/owner/` | Root cryptographic trust vault (`owner.json`) and challenge verification |
| `pkg/updater/` | Binary self-update mechanisms and background version checks |
| `pkg/client/` | HTTP clients: Orbit server plus HMAC `StaffClient` for orbit-staff |
| `pkg/staffhmac/` | HMAC-SHA256 signing for `orbit staff` (`X-Orbit-Timestamp` / `X-Orbit-Signature`) |
| `pkg/config/` | Workstation YAML config (`orbit config`) |
| `pkg/env/` | `.env` schema validation (`orbit env check|setup`) |
| `pkg/provisioner/` | Onboarding claim / SSH provisioning helpers |
| `pkg/orchestrator/` | Local process and container runner for `orbit dev` |
| `pkg/assets/` | `orbit-assets.yaml` index, gitignore helper, R2 S3 client, pull/push/add/status |
| `pkg/tui/` | Lipgloss styles, icons, and terminal UI formatting |

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

## Do / don't

- Invite and owner-challenge mail HTML/text come from `github.com/manovaspace/orbit-notifications/pkg/mailtemplates` (local replace `../orbit-notifications`). Do not add HTML strings in `pkg/invite`.
- Invite curl host is `https://orbit.manova.space` (not `get.manova.space`).
- Never put the HMAC token or OTP in the email subject.
- Use `pkg/tui` Lipgloss styling for output formatting — do not write unformatted console prints.
- Keep workstation CLI commands as pure API clients — do not embed server SMTP credentials in workstation commands.
- Adhere strictly to ADR-006 50-port block allocations (`1nxxx` range).
- Never log OTP challenge codes or master cryptographic signing secrets in stdout/stderr.
- No commit unless user asks.
