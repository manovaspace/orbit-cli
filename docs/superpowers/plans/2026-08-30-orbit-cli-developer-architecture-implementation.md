# Orbit CLI & Server: Developer Architecture & Zero-Touch Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the streamlined developer-first architecture, zero-touch onboarding pipeline, doctor version tightening, bootstrap consolidation into `orbit init`, dev stack health checking, and clean documentation generation.

**Architecture:** Tighten toolchain checks to Go 1.26 and Node >= 24; migrate idempotent setup steps from stateful migrations into `orbit init` and doctor healer recipes; enhance `orbit onboard` with automated R2 assets pulling, dev zone DNS verification, shell exports, and Authelia TOTP terminal QR codes; add health polling to `orbit dev up`.

**Tech Stack:** Go 1.26, Cobra CLI, Lipgloss TUI, Bun 1.4.x, Docker Compose v2, SQLite / JSON Stores, SSH Ed25519.

## Global Constraints
- All Go code must compile with `go 1.26.0` (`go build ./cmd/...`).
- No direct server credentials or LDAP bind passwords in workstation CLI packages (`cmd/orbit`).
- All state and config writes must use atomic write patterns (`write temp file -> fsync -> os.Rename`) with POSIX `0600` permissions.
- Test coverage must be maintained with zero failing unit tests (`go test ./...`).

---

### Task 1: Tighten Go Version Floor in Doctor to 1.26

**Files:**
- Modify: `orbit/orbit-cli/pkg/doctor/doctor.go:130-153`
- Modify: `orbit/orbit-cli/pkg/doctor/doctor_test.go:45-80`

**Interfaces:**
- `EvaluateGoVersion(rawOutput string, execErr error) DiagnosticResult`

- [ ] **Step 1: Write the updated test cases for Go 1.26**

```go
func TestEvaluateGoVersion_126(t *testing.T) {
	// Should fail for Go 1.24 or 1.25
	res := EvaluateGoVersion("go version go1.25.0 linux/amd64", nil)
	if res.Status != StatusError {
		t.Errorf("expected StatusError for Go 1.25, got %v", res.Status)
	}

	// Should pass for Go 1.26.0
	res = EvaluateGoVersion("go version go1.26.0 linux/amd64", nil)
	if res.Status != StatusOK {
		t.Errorf("expected StatusOK for Go 1.26, got %v", res.Status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails on Go 1.25**

Run: `go -C orbit/orbit-cli test ./pkg/doctor -run TestEvaluateGoVersion_126 -v`
Expected: FAIL (currently accepts >= 1.24)

- [ ] **Step 3: Update `EvaluateGoVersion` in `doctor.go`**

```go
if CompareVersions(v, "1.26.0") < 0 {
	return DiagnosticResult{
		Category:      category,
		Name:          name,
		Status:        StatusError,
		Message:       fmt.Sprintf("Go v%s is below required version (>= 1.26 required)", v),
		FixSuggestion: "Install Go 1.26: sudo snap install go --channel=1.26/stable --classic or visit https://go.dev/dl/.",
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go -C orbit/orbit-cli test ./pkg/doctor -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C orbit/orbit-cli add pkg/doctor/doctor.go pkg/doctor/doctor_test.go
git -C orbit/orbit-cli commit -m "fix(doctor): require Go >= 1.26 toolchain"
```

---

### Task 2: Consolidate Idempotent Bootstrap into `orbit init` & Doctor Healer

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/init.go:40-105`
- Modify: `orbit/orbit-cli/pkg/migrate/builtins.go:10-50`
- Modify: `orbit/orbit-cli/pkg/doctor/healer/recipes.go:40-120`
- Test: `orbit/orbit-cli/pkg/doctor/healer/healer_test.go`

**Interfaces:**
- `EnsureWorkspaceStructure(workspaceRoot string) error`
- `SymlinkCursorRules(workspaceRoot string) error`

- [ ] **Step 1: Write test for direct workspace initialization in `cmd/orbit/init.go`**

```go
func TestInit_EnsuresWorkspaceDirsAndSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	// Test that init creates orbit, manovaspace, clients directories
	err := runInitSetup(tmpDir)
	if err != nil {
		t.Fatalf("runInitSetup failed: %v", err)
	}
	for _, dir := range []string{"orbit", "manovaspace", "clients"} {
		if fi, err := os.Stat(filepath.Join(tmpDir, dir)); err != nil || !fi.IsDir() {
			t.Errorf("expected directory %s to exist", dir)
		}
	}
}
```

- [ ] **Step 2: Run test to verify behavior**

Run: `go -C orbit/orbit-cli test ./cmd/orbit -run TestInit_ -v`

- [ ] **Step 3: Update `cmd/orbit/init.go` and clear `pkg/migrate/builtins.go`**

Add direct workspace directory creation and symlinking into `runInit` in `cmd/orbit/init.go`. Keep `GetBuiltinMigrations()` in `pkg/migrate/builtins.go` returning an empty slice `[]Migration{}` for a clean slate.

- [ ] **Step 4: Run all migrate and init tests**

Run: `go -C orbit/orbit-cli test ./pkg/migrate ./cmd/orbit -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C orbit/orbit-cli add cmd/orbit/init.go pkg/migrate/builtins.go pkg/doctor/healer/recipes.go
git -C orbit/orbit-cli commit -m "refactor(init): consolidate workspace bootstrap into orbit init"
```

---

### Task 3: Enhance `orbit onboard` Pipeline with Automated R2 Pull & Network Readiness

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/onboard.go:200-450`
- Modify: `orbit/orbit-cli/pkg/session/types.go:10-35`
- Test: `orbit/orbit-cli/cmd/orbit/onboard_test.go`

**Interfaces:**
- `onboardAssetsSync(ctx context.Context, workspaceRoot string) error`
- `onboardNetworkValidate(ctx context.Context) error`

- [ ] **Step 1: Write unit tests for assets sync and network validation stages**

```go
func TestOnboard_AssetsSyncStage(t *testing.T) {
	// Test that onboarding runs assets pull if orbit-assets.yaml exists
	tmpDir := t.TempDir()
	err := runOnboardAssetsPhase(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("runOnboardAssetsPhase failed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify failure/stub**

Run: `go -C orbit/orbit-cli test ./cmd/orbit -run TestOnboard_AssetsSyncStage -v`

- [ ] **Step 3: Implement automated R2 assets sync and network setup in `cmd/orbit/onboard.go`**

Integrate `assets.Pull()` during `StageReposCloned -> StageDevStackReady` and verify `/etc/hosts` / DNS resolution for `*.dev.manova.space`.

- [ ] **Step 4: Run onboarding tests**

Run: `go -C orbit/orbit-cli test ./cmd/orbit -run TestOnboard -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C orbit/orbit-cli add cmd/orbit/onboard.go pkg/session/types.go cmd/orbit/onboard_test.go
git -C orbit/orbit-cli commit -m "feat(onboard): add automated R2 assets sync and network validation"
```

---

### Task 4: Add Health Readiness Probing to `orbit dev up`

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/dev.go:72-115`
- Test: `orbit/orbit-cli/cmd/orbit/dev_test.go`

**Interfaces:**
- `waitForServiceHealth(ctx context.Context, host string, port int, timeout time.Duration) bool`

- [ ] **Step 1: Write test for service health probing in `dev.go`**

```go
func TestWaitForServiceHealth_Success(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	if !checkPortReady("127.0.0.1", port, 1*time.Second) {
		t.Errorf("expected port %d to be ready", port)
	}
}
```

- [ ] **Step 2: Run test to verify failure/pass**

Run: `go -C orbit/orbit-cli test ./cmd/orbit -run TestWaitForServiceHealth -v`

- [ ] **Step 3: Implement health probe loop and URL banner in `cmd/orbit/dev.go`**

Add TCP socket probing for core services (Postgres `:10332`, NATS `:10482`, LLDAP `:10389`) after running compose up, then render the rich URL status table.

- [ ] **Step 4: Run dev tests**

Run: `go -C orbit/orbit-cli test ./cmd/orbit -run TestDev -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C orbit/orbit-cli add cmd/orbit/dev.go
git -C orbit/orbit-cli commit -m "feat(dev): add service health checking and endpoint output banner"
```

---

### Task 5: Full Verification, Documentation & Man Page Regeneration

**Files:**
- Modify: `orbit/orbit-cli/docs/cli/*.md`
- Modify: `orbit/orbit-cli/docs/cli/man/*`

- [ ] **Step 1: Run full test suite across all packages**

Run: `go -C orbit/orbit-cli test ./... -v`
Expected: All packages PASS

- [ ] **Step 2: Build CLI and daemon binaries**

Run:
```bash
go -C orbit/orbit-cli build -o bin/orbit ./cmd/orbit
go -C orbit/orbit-cli build -o bin/orbit-server ./cmd/orbit-server
```
Expected: Zero compilation errors

- [ ] **Step 3: Regenerate documentation and man pages**

Run:
```bash
./orbit/orbit-cli/bin/orbit doc -f markdown -o orbit/orbit-cli/docs/cli
./orbit/orbit-cli/bin/orbit doc -f man -o orbit/orbit-cli/docs/cli/man
```

- [ ] **Step 4: Commit updated documentation**

```bash
git -C orbit/orbit-cli add docs/
git -C orbit/orbit-cli commit -m "docs(cli): regenerate CLI reference docs and man pages"
```
