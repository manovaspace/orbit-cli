---
see_also:
  - handbook/docs/orbit/architecture/contracts.md
  - handbook/docs/orbit/architecture/module-catalog.md
  - handbook/docs/orbit/architecture/orbit-dev-ports.md
  - handbook/docs/orbit/decisions/022-gitignored-assets-on-r2.md
  - handbook/docs/orbit/guides/development-workflow.md
  - handbook/docs/orbit/guides/gitignored-assets-r2.md
  - handbook/docs/orbit/guides/manova-workspace.md
  - handbook/docs/orbit/guides/orbit-admin-init.md
  - handbook/docs/orbit/guides/staff-lifecycle.md
---

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
- **50-Port Allocation**: `orbit port list`, `orbit port allocate` — 50-port blocks per ADR-058.
- **Workspace Migrations**: `orbit migrate` (applies pending) and `orbit migrate status`.
- **Local Dev Orchestration**: `orbit dev up|down|tier2|caddy|portal|logs`.
- **Client & Dev Onboarding**: `orbit invite`, `orbit onboard` — workstation invite tokens (does **not** create lldap users). `--resume` continues a checkpoint; `--ignore-and-remove-checkpoint` (alias `--reset`) discards it. Staff directory: `orbit staff` HMAC client → orbit-staff ([staff lifecycle](https://handbook.internal.manova.space/docs/guides/staff-lifecycle)). CLI is implemented; the staff HTTP service is not live yet.
- **Pull Request & Delivery Lifecycle**: `orbit pr create`, `orbit pr merge`, `orbit pr status`, `orbit pr deliver` — automated branch pushing, Forgejo PR creation, PR status listing (one repository or every manifest repository matching a name pattern with `*`), merging, and full end-to-end branch delivery with fast-forward trunk re-sync per ADR-026. Non-release pull requests merge with no checks, skipped checks or only the secret scan; failed or pending checks refuse. Release and Renovate pull requests (`release-please--`, `changeset-release/`, `renovate/` heads) need a passing CI check. `--override "reason"` records an override comment, `--allow-no-checks` lets a release or Renovate pull request with no reported checks merge, and deliver (plus `create --merge`) waits for pending checks up to `--wait-timeout`.
- **Release & Demo Promotion**: `orbit release status|check|demo|prod` — reads the bot release PR and its release-tier check, promotes `main` to `demo` through a PR after the `main` commit's checks pass, and merges the release PR. Library repos (`.changeset/config.json`) dispatch `publish.yml`; the owner still approves the `npm-publish` environment. `release check --local` runs the release tier against the existing dev Postgres, Redis and NATS with a throwaway database and starts no containers ([ADR-044](../../handbook/docs/orbit/decisions/065-delivery.md)).
- **System Administration**: `orbit admin init` — root system ownership initialization, out-of-band email OTP challenges, and sealed vault management (`~/.config/orbit/owner.json`).

## Non-negotiables

1. **`o` commands, never `cd` loops** — every agent using orbit-cli must use `orbit` / `o` commands for workspace orchestration.
2. **`log/slog` only** — no `log.Printf`, `fmt.Println`, or other loggers anywhere.
3. **`orbit doctor --local` for research and inspection** — diagnostics never implicitly heal. Only select `--fix` when repairs are explicitly authorized; remote probes require `--remote`, and host-key acceptance additionally requires `--accept-host-keys --fix`.
4. **Port allocation via ADR-058** — never hardcode ports. Use `o port list` to inspect allocations.
5. **Sequential migrations** — `orbit migrate` only; never run raw SQL against production.
6. **`orbit staff` is HMAC-auth only** — never embed admin credentials in workstation commands.

## Engineering gotchas

- **Dual-binary architecture:** `orbit` (workstation CLI) and `orbit-server` (edge daemon) are separate binaries with separate concerns. Workstation commands must never hold lldap/Stalwart admin credentials.
- **`orbit repair` is non-destructive:** It copies `.git` onto gitless trees; it never runs `checkout -f`. Do not confuse with `orbit init`.
- **`orbit status` distinguishes states:** "not cloned" (absent dir) vs "gitless" (dir exists, no `.git`) vs "dirty" (uncommitted changes). These require different remediation.
- **`--resume` vs `--reset` in `orbit onboard`:** `--resume` continues from checkpoint; `--ignore-and-remove-checkpoint` (alias `--reset`) discards it entirely.
- **Mail templates are owned by `orbit-notifications`:** `pkg/mailtemplates` is the canonical source. Never duplicate or inline mail templates in orbit-cli.
- **Exit codes and output:** commands exit `0` ok, `1` failed, `2` could not check, `3` usage, `4` auth, `5` network, `6` external tool. New commands return `pkg/clierr` errors and write machine-readable results through `pkg/output`; external commands go through `pkg/runner` (tests use `pkg/runner/runnertest.Fake`). `cmd/orbit/main.go` maps every error to its exit code, and plain errors still exit `1`. Runtime errors print one line plus hint on stderr and never the usage block; `--json` failures are exactly one envelope on stdout (ADR-047 D5/D7).
- **Results go through `pkg/render`:** new commands print their text result through `render.Emit`, never `fmt.Fprint*`. ADR-047 phase 3 batch 1 has migrated `status`, `sync`, `migrate status` (and `migrate`), `port list`/`port allocate`, `assets status`, `env check` and `doctor` onto the shared title/table/section/summary shape; the remaining commands still use the `cmd/orbit/ui.go` style aliases until their batch lands.
- **Help goldens:** `cmd/orbit/testdata/help/` captures every command's `--help`. Run `go test ./cmd/orbit -run TestHelpGolden -update` after intentionally changing flags or help text. The test pins `HOME`, `COLUMNS` and `NO_COLOR` so goldens never depend on the machine.
- **Render goldens:** `pkg/render` and the batch 1 text/JSON goldens use `rendertest.Golden`. Run `go test ./pkg/render ./cmd/orbit -update-render` to rewrite them; the separate flag name keeps them from clashing with the help golden `-update`.

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
go run ./cmd/orbit-server --dev --addr :8080 --smtp-host mail.manova.space --smtp-port 587
```

## Structure & Packages

`cmd/orbit/` (Cobra tree), `cmd/orbit-server/` (edge daemon, mail gateway), `pkg/<name>/` one package per command area (`doctor`, `manifest`, `migrate`, `ports`, `pr`, `onboard`, `invite`, `owner`, `updater`, `client`, `staffhmac`, `config`, `env`, `provisioner`, `orchestrator`, `assets`, `render`, `tui`). `ls pkg/` and each package doc comment give the role.

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
| Tier-1 invariants and their tests | `docs/testing/tier1-invariants.md` |
| Agent `--json` / `--format json` contract | `docs/agent-json-contract.md` |

## Session pre-flight

1. `go build -o bin/orbit ./cmd/orbit` — confirm clean build before any changes
2. `bash scripts/check-agent-cli.sh` — check the installed diagnostic interface using version/help only; if it fails, validate the source build before installing an update
3. `o doctor --local` — inspect local toolchain without remote probes or repairs, after the compatibility check passes
4. Optional: `o status orbit` — branch state for this scope

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
- Adhere strictly to ADR-058 50-port block allocations.
- Never log OTP challenge codes or master cryptographic signing secrets in stdout/stderr.
- No commit unless user asks.

<!-- manova:conventions v1 — generated from manovaspace/ci conventions/core.md; edit it there -->
## Shared conventions

These rules apply to every Manova repository. They are copied from `manovaspace/ci` (`conventions/core.md`); change them there, not here.

### Delivery

1. Never push to `main`. Every change goes on a topic branch (`feat/<name>`, `fix/<name>`, `refactor/<name>`, `docs/<name>`, `chore/<name>`) and reaches `main` through a pull request.
2. One pull request does one thing. An unrelated problem you find on the way becomes an issue, not part of this change.
3. Pull request titles and commit messages follow Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `chore:`, `test:`; `!` for a breaking change). Releases are computed from them.
4. Merge with squash once the checks are green. Do not use `--no-verify`; if you ever must, say so in the pull request.

### Checks

5. CI runs `manovaspace/ci@v1`. Run the same gate locally before you push; a red check is fixed, not skipped.
6. Tool versions (Go, Node, Bun, linters) come from `ci/versions.env` in `manovaspace/ci`. Do not pin a different version in this repository.
7. A behaviour change comes with a test that fails without it. Do not delete or weaken a test to make a change pass.

### Secrets

8. Never commit a secret, token, password, private key or `.env` file. Commit `.env.example` with placeholders instead.
9. Never print a secret in logs, test output, error messages or pull requests. Read secrets from the environment or a file at run time.

### Runtime

10. Containers talk to each other by name on a Docker network. Do not publish a port on the host unless a person or a host process needs it, and then bind `127.0.0.1`, never `0.0.0.0`.
11. Configuration comes from the environment. A service with a missing required setting refuses to start; it never falls back to a built-in secret or a development default in production.
12. Port numbers come from the workspace port registry. Do not invent a port.

### Documentation

13. This repository explains itself: `README.md` says how to set up, run, test and deploy it; `AGENTS.md` holds its binding rules; repository decisions live in `docs/decisions/`.
14. Write plain, short sentences. Keep exact names, paths, commands and versions. Do not document what the code already says.
<!-- /manova:conventions -->
