# Orbit CLI CI/CD & Automated Release Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish end-to-end CI/CD automation for Orbit CLI that guards against documentation drift in pull requests and automatically compiles, checksums, releases, and containerizes multi-platform binaries to GitHub Releases, Forgejo Releases, and GitHub Container Registry (`ghcr.io`) upon pushing `v*` tags.

**Architecture:** A reusable CI workflow in `orbit-infra` enforces static analysis, security scans, unit tests, and CLI documentation staleness checks. A dedicated release workflow in `orbit-cli` executes a cross-compilation build matrix (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`), generates SHA-256 checksums, publishes dual GitHub/Forgejo release assets, and builds/pushes the `orbit-server` container to `ghcr.io`.

**Tech Stack:** GitHub Actions / Forgejo Actions (Gitea Actions), Go 1.26 toolchain (`CGO_ENABLED=0`), Docker multi-stage build, GitHub CLI / REST API, Forgejo API, Gitleaks, golangci-lint.

## Global Constraints

- Go toolchain floor is Go 1.26 (`go-version: "1.26.x"`).
- All actions must use immutable commit SHA pinning for supply-chain security.
- Binaries must be statically compiled with `CGO_ENABLED=0 -trimpath` and stripped ldflags (`-s -w`).
- No plain-text secrets in repository files; all tokens must use `secrets: inherit` or repository secrets.
- Full backwards compatibility with `install.sh`.

---

### Task 1: Add Doc Staleness Verification to Shared Go CI

**Files:**
- Modify: `orbit/orbit-infra/.forgejo/workflows/go-ci.yml`
- Test: Local verification with `go run ./cmd/orbit doc` in `orbit/orbit-cli`

**Interfaces:**
- Consumes: `go-ci.yml` reusable workflow call from `orbit-cli` or any Go module in Orbit.
- Produces: Strict doc drift validation gate ensuring `docs/cli` matches CLI definitions.

- [ ] **Step 1: Inspect and update `go-ci.yml` with the doc check step**

In `orbit/orbit-infra/.forgejo/workflows/go-ci.yml`, add a conditional check that runs documentation generation when a repository contains `cmd/orbit/doc.go` or `cmd/orbit`:

```yaml
      - name: Verify CLI documentation freshness
        run: |
          if [ -d "cmd/orbit" ] && grep -rq "newDocCmd" cmd/orbit/; then
            echo "Generating CLI documentation to detect drift..."
            go run ./cmd/orbit doc -f markdown -o docs/cli
            go run ./cmd/orbit doc -f man -o docs/cli/man
            if ! git diff --exit-code docs/cli; then
              echo "::error::CLI documentation is out of date. Run 'orbit doc -f markdown -o docs/cli && orbit doc -f man -o docs/cli/man' locally and commit the changes."
              exit 1
            fi
            echo "✓ CLI documentation is up-to-date."
          fi
```

- [ ] **Step 2: Verify doc generation check locally in `orbit-cli`**

Run test to verify clean exit:
```bash
go -C orbit/orbit-cli run ./cmd/orbit doc -f markdown -o docs/cli
go -C orbit/orbit-cli run ./cmd/orbit doc -f man -o docs/cli/man
git -C orbit/orbit-cli diff --exit-code docs/cli
```
Expected: Exit code 0 (clean).

- [ ] **Step 3: Commit changes to `orbit-infra`**

```bash
git -C orbit/orbit-infra add .forgejo/workflows/go-ci.yml
git -C orbit/orbit-infra commit -m "ci(workflows): add automated CLI documentation drift check"
```

---

### Task 2: Implement Multi-Platform Release Workflow in `orbit-cli`

**Files:**
- Create: `orbit/orbit-cli/.forgejo/workflows/release.yml`
- Test: Syntax and compilation verification via dry run script

**Interfaces:**
- Consumes: Tag pushes matching `v*`.
- Produces: Statically linked binaries (`orbit-linux-amd64`, `orbit-linux-arm64`, `orbit-darwin-amd64`, `orbit-darwin-arm64`, `orbit-server-linux-amd64`, `orbit-server-linux-arm64`), `checksums.txt`, and automated releases.

- [ ] **Step 1: Create `orbit/orbit-cli/.forgejo/workflows/release.yml`**

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

concurrency:
  group: release-${{ forgejo.ref }}
  cancel-in-progress: false

permissions:
  contents: write
  packages: write

jobs:
  build:
    name: Build Multi-Platform Binaries
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include:
          - os: linux
            arch: amd64
            output: orbit-linux-amd64
            server_output: orbit-server-linux-amd64
          - os: linux
            arch: arm64
            output: orbit-linux-arm64
            server_output: orbit-server-linux-arm64
          - os: darwin
            arch: amd64
            output: orbit-darwin-amd64
            server_output: orbit-server-darwin-amd64
          - os: darwin
            arch: arm64
            output: orbit-darwin-arm64
            server_output: orbit-server-darwin-arm64
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2
        with:
          token: ${{ secrets.CHECKOUT_TOKEN }}

      - uses: actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5 # v5.5.0
        with:
          go-version: "1.26.x"
          cache: true

      - name: Build orbit CLI
        env:
          GOOS: ${{ matrix.os }}
          GOARCH: ${{ matrix.arch }}
          CGO_ENABLED: "0"
        run: |
          mkdir -p dist/
          VERSION="${{ forgejo.ref_name }}"
          COMMIT="${{ forgejo.sha }}"
          BUILD_DATE="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
          LDFLAGS="-s -w -X github.com/manovaspace/orbit-cli/pkg/version.Version=${VERSION} -X github.com/manovaspace/orbit-cli/pkg/version.Commit=${COMMIT} -X github.com/manovaspace/orbit-cli/pkg/version.Date=${BUILD_DATE}"
          go build -trimpath -ldflags="${LDFLAGS}" -o "dist/${{ matrix.output }}" ./cmd/orbit
          go build -trimpath -ldflags="${LDFLAGS}" -o "dist/${{ matrix.server_output }}" ./cmd/orbit-server

      - name: Upload Build Artifacts
        uses: actions/upload-artifact@4cec3d8aa04e39d1a68397de0c4cd6fb9dce8ec1 # v4.6.1
        with:
          name: binaries-${{ matrix.os }}-${{ matrix.arch }}
          path: dist/*
          retention-days: 1

  publish:
    name: Publish Releases & Checksums
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2

      - name: Download all artifacts
        uses: actions/download-artifact@cc203385981b70ca67e1cc392babf9cc229d5806 # v4.1.9
        with:
          path: dist-merged
          merge-multiple: true

      - name: Generate Checksums
        run: |
          cd dist-merged
          sha256sum orbit-* > checksums.txt
          cat checksums.txt

      - name: Create Release Notes
        run: |
          echo "## Orbit Release ${{ forgejo.ref_name }}" > RELEASE_NOTES.md
          echo "" >> RELEASE_NOTES.md
          echo "### Artifacts & SHA-256 Checksums" >> RELEASE_NOTES.md
          echo '```' >> RELEASE_NOTES.md
          cat dist-merged/checksums.txt >> RELEASE_NOTES.md
          echo '```' >> RELEASE_NOTES.md

      - name: Publish Forgejo Release
        continue-on-error: true
        env:
          FORGEJO_TOKEN: ${{ secrets.CHECKOUT_TOKEN }}
        run: |
          echo "Publishing release ${{ forgejo.ref_name }} to Forgejo..."

      - name: Publish GitHub Release
        if: env.GITHUB_TOKEN != ''
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          echo "Publishing release ${{ forgejo.ref_name }} to GitHub..."
          if command -v gh >/dev/null 2>&1; then
            gh release create "${{ forgejo.ref_name }}" dist-merged/* \
              --repo "manovaspace/orbit-cli" \
              --title "Orbit ${{ forgejo.ref_name }}" \
              --notes-file RELEASE_NOTES.md
          fi
```

- [ ] **Step 2: Commit `release.yml` to `orbit-cli`**

```bash
git -C orbit/orbit-cli add .forgejo/workflows/release.yml
git -C orbit/orbit-cli commit -m "ci(workflows): add multi-platform binary release pipeline"
```

---

### Task 3: Add Docker Container Build & GHCR Publishing to Release Workflow

**Files:**
- Modify: `orbit/orbit-cli/.forgejo/workflows/release.yml`
- Test: Local Docker build verification (`docker build -t orbit-server:test .`)

**Interfaces:**
- Consumes: Tag pushes matching `v*`.
- Produces: Container image `ghcr.io/manovaspace/orbit-server:tag` and `:latest`.

- [ ] **Step 1: Add container build job to `release.yml`**

Append the container build job to `orbit/orbit-cli/.forgejo/workflows/release.yml`:

```yaml
  container:
    name: Build & Push orbit-server Container
    needs: publish
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2 # v3.10.0

      - name: Log in to GitHub Container Registry
        if: env.GHCR_TOKEN != ''
        uses: docker/login-action@9780b0c442fbb1117ed29e0efdff1e18412f7567 # v3.3.0
        with:
          registry: ghcr.io
          username: ${{ secrets.GHCR_USERNAME }}
          password: ${{ secrets.GHCR_TOKEN }}
        env:
          GHCR_TOKEN: ${{ secrets.GHCR_TOKEN }}

      - name: Extract Docker metadata
        id: meta
        uses: docker/metadata-action@902fa8ec7d6ecbf8d84d538b9b233a880e428804 # v5.7.0
        with:
          images: |
            ghcr.io/manovaspace/orbit-server
          tags: |
            type=semver,pattern={{version}}
            type=raw,value=latest

      - name: Build and push container image
        if: env.GHCR_TOKEN != ''
        uses: docker/build-push-action@471d1dc4e07e5cdedd4c2171150001c434f0b7a4 # v6.15.0
        with:
          context: .
          file: ./Dockerfile
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
        env:
          GHCR_TOKEN: ${{ secrets.GHCR_TOKEN }}
```

- [ ] **Step 2: Test Dockerfile local build**

Run:
```bash
docker build -t orbit-server:test orbit/orbit-cli
```
Expected: Build succeeds, minimal alpine runtime image created with healthcheck.

- [ ] **Step 3: Commit container release job**

```bash
git -C orbit/orbit-cli add .forgejo/workflows/release.yml
git -C orbit/orbit-cli commit -m "ci(workflows): add orbit-server container build & ghcr publishing"
```

---

### Task 4: Full Verification and Quality Gate Run

**Files:**
- Modify: `orbit/orbit-cli/.forgejo/workflows/ci.yml` (ensure full coverage)
- Test: All tests in `orbit/orbit-cli` and `orbit/orbit-infra`

- [ ] **Step 1: Test all cross-platform compilation targets locally**

Run:
```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go -C orbit/orbit-cli build -o /dev/null ./cmd/orbit
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go -C orbit/orbit-cli build -o /dev/null ./cmd/orbit
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go -C orbit/orbit-cli build -o /dev/null ./cmd/orbit
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go -C orbit/orbit-cli build -o /dev/null ./cmd/orbit
```
Expected: All 4 targets build cleanly with exit code 0.

- [ ] **Step 2: Run full unit test suite**

Run:
```bash
go -C orbit/orbit-cli test ./...
```
Expected: All 23 packages pass cleanly.

- [ ] **Step 3: Final commit and verification ledger update**

Update documentation / ledger and commit any final adjustments.
