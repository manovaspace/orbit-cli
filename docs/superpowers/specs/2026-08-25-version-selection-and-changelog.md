# Version Selection & Built-in Changelog Viewer — Spec

**Goal:**
1. Support multi-modal version selection during installation (`get.manova.space/vX.Y.Z`, `MANOVA_VERSION=...`, and `--version` flag).
2. Add a first-class terminal changelog viewer (`manova changelog` / `whatsnew`).
3. Support target version installs in `manova self-update --version <tag>`.

---

## 1. Multi-Modal Version Selection Architecture

### A. Edge Worker (`clients/manova/manova-infra/static/worker.js`)
- Route match: `GET /v:version` or `GET /:version` (e.g. `/v0.2.8`, `/0.2.8`)
- Injects `TARGET_VERSION="v0.2.8"` into the served installer shell script.
- Serves latest stable when root path `/` is requested.

### B. Installer Script (`static/get.sh`)
Resolution hierarchy for target version:
1. `MANOVA_VERSION` environment variable (if set).
2. Injected `TARGET_VERSION` from edge script route.
3. `--version` CLI flag passed to `bash -s -- --version <tag>`.
4. Defaults to `latest`.

Target download URL:
- If version is `latest`: `https://github.com/manovaspace/orbit-cli/releases/latest/download/manova-${OS}-${ARCH}`
- If version is specific tag: `https://github.com/manovaspace/orbit-cli/releases/download/${TAG}/manova-${OS}-${ARCH}`

---

## 2. Terminal Changelog Engine (`pkg/changelog` & `cmd/manova/changelog.go`)

### `pkg/changelog/types.go`
```go
type ReleaseEntry struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	Highlights  []string  `json:"highlights"`
	Body        string    `json:"body,omitempty"`
}
```

### Subcommands & Aliases:
- `manova changelog` [aliases: `whatsnew`, `whatisnew`, `news`]
  - Flags:
    - `--version <tag>`: Show specific release notes
    - `--limit <n>`: Number of releases to display (default: 5)
    - `--json`: Output structured JSON

### Self-Update Integration:
- `manova self-update --version <tag>`: Downgrade or upgrade to a specific target version.
- On successful update, prints the "What's New in <version>" summary card.

---

## 3. Files Touched
- `clients/manova/manova-infra/static/worker.js`
- `clients/manova/manova-infra/static/get.sh`
- `orbit/orbit-cli/pkg/changelog/types.go`
- `orbit/orbit-cli/pkg/changelog/changelog.go`
- `orbit/orbit-cli/pkg/changelog/changelog_test.go`
- `orbit/orbit-cli/cmd/manova/changelog.go`
- `orbit/orbit-cli/cmd/manova/changelog_test.go`
- `orbit/orbit-cli/cmd/manova/selfupdate.go`
- `orbit/orbit-cli/cmd/manova/main.go`
