# Version Selection & Built-in Changelog Viewer — Implementation Plan

> **For agents:** Use the `executing-plans` skill to implement this plan task-by-task.

**Goal:**
1. Multi-modal version selection in the installer (`get.manova.space/vX.Y.Z`, `MANOVA_VERSION=...`, `--version ...`).
2. Built-in changelog viewer `manova changelog` / `whatsnew`.
3. Support target version in `manova self-update --version <tag>`.

**Spec:** `docs/superpowers/specs/2026-08-25-version-selection-and-changelog.md`

---

## Task 1: Multi-Modal Installer Version Selection
- Update `clients/manova/manova-infra/static/get.sh`:
  - Support `MANOVA_VERSION` env var, `TARGET_VERSION` default, and `--version` / `-v` flag.
  - Dynamically construct `releases/download/${TAG}/manova-${OS}-${ARCH}` vs `releases/latest/download/...`.
- Update `clients/manova/manova-infra/static/worker.js`:
  - Support `/v:version` and `/:version` routing (e.g. `/v0.2.8`) dynamically substituting `TARGET_VERSION="v0.2.8"`.

---

## Task 2: `pkg/changelog` Engine & Types
- Create `pkg/changelog/types.go` and `pkg/changelog/changelog.go`.
- Create `pkg/changelog/changelog_test.go`:
  - Parsing release entries from feed / GitHub releases.
  - Filtering by version tag.
  - Formatting release cards.

---

## Task 3: CLI Commands & `self-update --version`
- Create `cmd/manova/changelog.go` (aliases: `whatsnew`, `whatisnew`, `news`).
- Create `cmd/manova/changelog_test.go`.
- Update `cmd/manova/selfupdate.go` to support `--version <tag>`.
- Register `newChangelogCmd()` in `cmd/manova/main.go`.

---

## Task 4: Verification & Release
- Run full test suite `go test ./... -v`.
- Build release `v0.3.1` and deploy to Cloudflare Edge.
- Test `manova changelog`, `manova whatsnew`, and version-pinned installer.
