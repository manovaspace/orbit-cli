# Rename CLI to Orbit (`orbit`), Fresh Baseline v0.1.0 & Dual-Mode `orbit.manova.space` — Implementation Plan

**Goal:** Refactor the CLI to `orbit` (alias `o`), reset version/migrations to `v0.1.0`, implement `orbit.manova.space` dual content negotiation in Caddy/installer, add `/orbit` landing page to the frontend website, and update all workspace documentation.

---

## Task 1: Refactor `orbit/orbit-cli` Core Packages
- Update `pkg/alias`: Change default alias from `m` to `o`, update autocompletion hooks for `orbit` and `o`.
- Update `pkg/user`: Set `DefaultUserStoreFile = "~/.orbit/users.json"` (with fallback reading).
- Update `pkg/session`: Set `DefaultSessionFile = "~/.orbit/session.json"`.
- Update `pkg/worker`: Set `DefaultStateFile = "~/.orbit/edge-version.json"`.
- Update `pkg/notifier`: Set `DefaultFeedFile = "~/.orbit/feed.json"`, `DefaultStoreFile = "~/.orbit/messages.json"`, `DefaultFeedURL = "https://orbit.manova.space/api/feed"`.
- Update `pkg/updater`: Update binary naming to `orbit-${OS}-${ARCH}` and reset version logic.
- Update `pkg/migrate`: Reset migrations to clean `v0.1.0` baseline, tracking in `.orbit/state.json`.
- Update `pkg/manpage`: Generate `orbit.1` and `o.1`.

## Task 2: Refactor `cmd/` Entrypoints to `cmd/orbit/`
- Rename `cmd/manova/` directory to `cmd/orbit/`.
- Update `cmd/orbit/main.go`:
  - Set root command `Use: "orbit"`.
  - Set default version to `v0.1.0`.
  - Update post-run notifier and onboarding prompts to reference `orbit onboard --resume` and `orbit self-update`.
- Update all subcommands in `cmd/orbit/`:
  - `dev.go`, `doctor.go`, `env.go`, `init.go`, `invite.go`, `migrate.go`, `onboard.go`, `port.go`, `selfupdate.go`, `status.go`, `sync.go`, `uninstall.go`, `update.go`, `user.go`, `worker.go`, `changelog.go`, `doc.go`.
  - Update references from `manova` to `orbit`, and `m` to `o`.
  - Update environment variable resolution: `ORBIT_*` first, falling back to `MANOVA_*`.

## Task 3: Test Verification in `orbit-cli`
- Update all test files in `pkg/` and `cmd/orbit/`.
- Run `go test ./...` in `orbit/orbit-cli` and ensure all tests pass.
- Build binary `bin/orbit` and verify `./bin/orbit --help`, `./bin/orbit version`, `./bin/orbit user list`.

## Task 4: Installer & Caddy Routing for `orbit.manova.space`
- Create `clients/manova/manova-infra/static/orbit.sh` (and symlink / keep `get.sh` pointing to it).
- Update `clients/manova/manova-infra/caddy/website.caddy`:
  - Add `orbit.manova.space` block with `@cli` header matcher (`User-Agent (?i)(curl|wget|httpie)`) serving `orbit.sh`.
  - Route browser requests to `manova-frontend`.
  - Add `get.manova.space` redirect/alias to `orbit.manova.space`.

## Task 5: Website Landing Page (`clients/manova/manova-frontend`)
- Create `src/app/[locale]/orbit/page.tsx` with bilingual (EN/FA) content:
  - Copyable 1-line command: `curl -fsSL orbit.manova.space | bash`
  - Quickstart guide: `o dev up`, `o status`, `o onboard`
  - Architecture highlights.
- Update `src/content/data/products.json` Orbit product link.
- Run `bun run build` or `bun run lint` in `manova-frontend` to verify.

## Task 6: Handbook & Workspace Documentation
- Update `handbook/docs/orbit/guides/developer-and-member-management.md` with `orbit` and `o`.
- Update `handbook/docs/orbit/guides/dev-zone-identity.md` and `handbook/docs/orbit/guides/manova-workspace.md`.
- Update `handbook/cursor/agent-routing.yaml` and root `AGENTS.md`.
- Run `bun run validate:agent-routing` and `bun run build` in `handbook`.

## Task 7: Final End-to-End Verification
- Run tests and builds across all modified repositories.
