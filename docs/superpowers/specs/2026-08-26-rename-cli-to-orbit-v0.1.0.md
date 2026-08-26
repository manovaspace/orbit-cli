# Rename CLI to Orbit (`orbit`), Fresh Baseline v0.1.0 & Dual-Mode `orbit.manova.space` — Spec

**Goal:** Refactor the primary developer and workspace CLI from `manova` to `orbit` (with 1-letter shortcut `o`), reset version and migration history to a clean `v0.1.0` baseline, update installer script and Caddy routing for `orbit.manova.space` with content negotiation (curl vs. browser), add `/orbit` landing page on the website, and update all workspace documentation.

---

## 1. Scope & Architecture

### A. CLI Binary & Package Renaming (`orbit/orbit-cli`)
- Rename entrypoint directory from `cmd/manova/` to `cmd/orbit/`.
- Root Cobra command: `Use: "orbit"` (Short: "Orbit platform and workspace orchestrator. (Shortcut: 'o')").
- Version: `v0.1.0` (commit/date injected at build time).
- Reset legacy migrations to a fresh baseline (v0.1.0).
- Primary shell alias: `o` (`alias o="orbit"`).
- Default configuration directory: `~/.orbit/` (`users.json`, `session.json`, `feed.json`, `messages.json`, `state.json`).
- WireGuard VPN profile: `~/.config/orbit/wg0.conf`.
- Workspace state file: `.orbit/state.json`.
- Environment variables:
  - `ORBIT_EDGE_URL` (fallback `MANOVA_EDGE_URL`)
  - `ORBIT_INVITE_SECRET` (fallback `MANOVA_INVITE_SECRET`)
  - `ORBIT_JWT_SECRET` (fallback `MANOVA_JWT_SECRET`)
  - `ORBIT_SKIP_UPDATE_CHECK` (fallback `MANOVA_SKIP_UPDATE_CHECK`)
  - `ORBIT_ROOT` (fallback `MANOVA_ROOT`)

### B. Shell Ergonomics & Completions (`pkg/alias`)
- Shortcut alias: `o` (`alias o="orbit"`).
- Autocompletion:
  - Zsh: `compdef o=orbit`
  - Bash: `complete -o default -F __start_orbit o`
- Man pages: Generate `dist/man/man1/orbit.1` and `dist/man/man1/o.1`.

### C. Installer & Content Negotiation (`orbit.manova.space`)
- Create `clients/manova/manova-infra/static/orbit.sh` with updated banners, `orbit` download URLs, and `orbit onboard` / `orbit init --bootstrap`.
- In `clients/manova/manova-infra/caddy/website.caddy`:
  - `orbit.manova.space`:
    - `@cli` matcher on `User-Agent (?i)(curl|wget|httpie)` or `Accept: text/plain` -> serve `/var/www/static/orbit.sh` (`text/plain; charset=utf-8`).
    - Web browsers -> reverse proxy to `manova-frontend` (serving `/orbit` landing page).
  - `get.manova.space` -> redirect or alias to `orbit.manova.space`.

### D. Website Landing Page (`clients/manova/manova-frontend`)
- Add bilingual `/orbit` route (`app/[locale]/orbit/page.tsx`) showcasing:
  - Copyable 1-line installation: `curl -fsSL orbit.manova.space | bash`
  - 1-letter shortcut: `o onboard`, `o dev up`, `o status`
  - Feature highlights (multi-repo sync, zero-leak identity, wireguard VPN, dev compose)
  - Links to handbook docs.
- Update `products.json` Orbit entry to link to `https://orbit.manova.space` (or `/orbit`).

### E. Handbook & Workspace Docs
- Update [`developer-and-member-management.md`](file:///home/opmc/Dev/Manova/handbook/docs/orbit/guides/developer-and-member-management.md) with `orbit user`, `orbit invite`, `orbit onboard`, and `o`.
- Update [`dev-zone-identity.md`](file:///home/opmc/Dev/Manova/handbook/docs/orbit/guides/dev-zone-identity.md) and [`manova-workspace.md`](file:///home/opmc/Dev/Manova/handbook/docs/orbit/guides/manova-workspace.md).
- Update [`handbook/cursor/agent-routing.yaml`](file:///home/opmc/Dev/Manova/handbook/cursor/agent-routing.yaml) and root [`AGENTS.md`](file:///home/opmc/Dev/Manova/AGENTS.md).
