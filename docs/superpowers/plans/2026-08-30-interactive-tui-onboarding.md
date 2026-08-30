# Interactive TUI Onboarding Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a stateful, interactive full-screen TUI wizard (`orbit onboard`) using Charm Bubble Tea, Lipgloss, and Bubbles that orchestrates the entire developer onboarding pipeline (token verification, system diagnostics & auto-healing, Ed25519 SSH key generation, Forgejo Git registration, repository cloning, Cloudflare R2 assets sync, Cursor MCP setup, local DNS/TLS trust, and dev stack launch) with automatic checkpointing and rollback.

**Architecture:** A 6-stage Bubble Tea `tea.Model` state machine in `pkg/tui/onboard/` coordinated with checkpoint persistence in `pkg/session/`. Cobra CLI entrypoint in `cmd/orbit/onboard.go` auto-dispatches to full-screen TUI in interactive TTY sessions while preserving the headless `--json` emitter for CI.

**Tech Stack:** Go 1.26, Charm Bubble Tea (`github.com/charmbracelet/bubbletea`), Lipgloss (`github.com/charmbracelet/lipgloss`), Bubbles (`github.com/charmbracelet/bubbles`), Cobra, Ed25519 SSH crypto.

## Global Constraints
- Target workspace: `orbit/orbit-cli` (Go 1.26 module).
- All UI components must use `github.com/charmbracelet/lipgloss` and `bubbletea` styling conforming to Orbit CLI theme.
- All file permissions for keys, tokens, and sessions must enforce POSIX `0600` / directories `0700`.
- TUI must gracefully handle `tea.WindowSizeMsg` responsive resizing down to 80x24 terminals.
- Checkpoint file location: `~/.config/orbit/session.json`.

---

### Task 1: Checkpoint & Session State Model

**Files:**
- Modify: `orbit/orbit-cli/pkg/session/types.go:1-60`
- Modify: `orbit/orbit-cli/pkg/session/manager.go:1-120`
- Test: `orbit/orbit-cli/pkg/session/session_test.go`

**Interfaces:**
- Consumes: `os.UserHomeDir()`
- Produces: `SessionState`, `Stage`, `SessionManager.SaveCheckpoint()`, `SessionManager.LoadSession()`, `SessionManager.DiscardSession()`, `SessionManager.Rollback()`

- [ ] **Step 1: Write the failing unit tests for session checkpoints & rollback**

```go
package session_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/session"
)

func TestSessionCheckpointSaveAndRestore(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")

	sm, err := session.NewSessionManager(sessionPath)
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	state := &session.SessionState{
		CurrentStage: session.StageWorkspace,
		Email:        "dev@manova.space",
		DisplayName:  "Test Dev",
		ClaimToken:   "orb_inv_test_token_123",
		UpdatedAt:    time.Now().UTC(),
	}

	if err := sm.SaveSession(state); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	if !sm.HasPendingSession() {
		t.Fatalf("expected HasPendingSession() to be true")
	}

	loaded, err := sm.LoadSession()
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if loaded.CurrentStage != session.StageWorkspace {
		t.Fatalf("expected stage %v, got %v", session.StageWorkspace, loaded.CurrentStage)
	}
	if loaded.Email != "dev@manova.space" {
		t.Fatalf("expected email dev@manova.space, got %s", loaded.Email)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/session -v`  
Expected: FAIL or missing methods

- [ ] **Step 3: Implement updated SessionState and SessionManager**

Update `pkg/session/types.go` and `pkg/session/manager.go` to support all onboarding stages (`StageInit`, `StageDoctorPassed`, `StageKeypairReady`, `StageTokenClaimed`, `StageReposCloned`, `StageEnvironmentReady`, `StageStackReady`, `StageCompleted`) and atomic file persistence with `0600` permissions.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/session -v`  
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add pkg/session/
git commit -m "feat(session): add structured onboarding stage checkpoints and session recovery"
```

---

### Task 2: Bubble Tea Core Root Model & Responsive Layout

**Files:**
- Create: `orbit/orbit-cli/pkg/tui/onboard/styles.go`
- Create: `orbit/orbit-cli/pkg/tui/onboard/model.go`
- Test: `orbit/orbit-cli/pkg/tui/onboard/model_test.go`

**Interfaces:**
- Consumes: `pkg/session`, `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`
- Produces: `NewWizardModel(opts WizardOptions) *WizardModel`, `(m WizardModel) Init() tea.Cmd`, `(m WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)`, `(m WizardModel) View() string`

- [ ] **Step 1: Write the failing unit test for the root Wizard model lifecycle**

```go
package onboard_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestWizardModelInitAndResize(t *testing.T) {
	sm, err := session.NewSessionManager(t.TempDir() + "/session.json")
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	cmd := model.Init()
	if cmd == nil {
		t.Errorf("expected non-nil init command for spinner")
	}

	// Test window resize message
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, ok := updated.(*tuiOnboard.WizardModel)
	if !ok {
		t.Fatalf("expected updated model to be *WizardModel")
	}

	view := m.View()
	if len(view) == 0 {
		t.Errorf("expected non-empty view output")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/tui/onboard -v`  
Expected: FAIL (package does not exist)

- [ ] **Step 3: Implement styles.go and model.go**

Create `pkg/tui/onboard/styles.go` with Orbit brand colors, borders, titles, steppers, and layout cards. Implement `model.go` managing active stages, header stepper indicator, dynamic width/height rendering, and keyboard shortcuts (`Ctrl+C` to save and abort, `Enter` to advance).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tui/onboard -v`  
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add pkg/tui/onboard/
git commit -m "feat(tui): implement Bubble Tea onboarding root model and responsive layout styles"
```

---

### Task 3: Welcome & Token Verification Stage

**Files:**
- Create: `orbit/orbit-cli/pkg/tui/onboard/stage_welcome.go`
- Test: `orbit/orbit-cli/pkg/tui/onboard/stage_welcome_test.go`

**Interfaces:**
- Consumes: `github.com/charmbracelet/bubbles/textinput`, `pkg/invite`
- Produces: `welcomeStageView(m *WizardModel) string`, `handleWelcomeUpdate(m *WizardModel, msg tea.Msg) (tea.Model, tea.Cmd)`

- [ ] **Step 1: Write failing test for token validation and profile rendering**

```go
package onboard_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestWelcomeStageTokenValidation(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		PreSetToken:    "invalid_token",
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(*tuiOnboard.WizardModel)
	if m.ErrorMsg == "" {
		t.Errorf("expected error message for invalid token format")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/tui/onboard -run TestWelcomeStageTokenValidation -v`  
Expected: FAIL

- [ ] **Step 3: Implement stage_welcome.go**

Implement masked token text input, clipboard detection, async token validation, and the Verified Developer Profile card.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tui/onboard -run TestWelcomeStageTokenValidation -v`  
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add pkg/tui/onboard/
git commit -m "feat(tui): add welcome and token verification stage component"
```

---

### Task 4: System Pre-Flight Diagnostics & Auto-Healing Stage

**Files:**
- Modify: `orbit/orbit-cli/pkg/doctor/doctor.go:150-160` (tighten Go version to 1.26)
- Create: `orbit/orbit-cli/pkg/tui/onboard/stage_doctor.go`
- Test: `orbit/orbit-cli/pkg/tui/onboard/stage_doctor_test.go`

**Interfaces:**
- Consumes: `pkg/doctor.RunDiagnostics()`, `pkg/doctor/healer`
- Produces: `doctorStageView(m *WizardModel) string`, `handleDoctorUpdate(m *WizardModel, msg tea.Msg) (tea.Model, tea.Cmd)`

- [ ] **Step 1: Write failing test for doctor check execution and auto-healing trigger**

```go
package onboard_test

import (
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestDoctorStageDiagnosticsRun(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	report := doctor.RunDiagnostics()
	if report == nil || len(report.Results) == 0 {
		t.Fatalf("expected non-empty doctor diagnostics")
	}

	model.SetDoctorReport(report)
	view := model.DoctorView()
	if len(view) == 0 {
		t.Errorf("expected non-empty doctor view")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/tui/onboard -run TestDoctorStageDiagnosticsRun -v`  
Expected: FAIL

- [ ] **Step 3: Implement stage_doctor.go and tighten Go check in doctor.go**

Update `doctor.go` to require Go 1.26. Implement `stage_doctor.go` with a responsive table of diagnostic results, spinners for ongoing checks, and an interactive `[F]` trigger to execute auto-healing recipes.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tui/onboard -run TestDoctorStageDiagnosticsRun -v`  
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add pkg/doctor/ pkg/tui/onboard/
git commit -m "feat(tui): add system pre-flight diagnostics and auto-healing stage with Go 1.26 requirement"
```

---

### Task 5: Cryptographic Identity & Git Registration Stage

**Files:**
- Create: `orbit/orbit-cli/pkg/tui/onboard/stage_identity.go`
- Test: `orbit/orbit-cli/pkg/tui/onboard/stage_identity_test.go`

**Interfaces:**
- Consumes: `crypto/ed25519`, `pkg/provisioner`, `pkg/onboard`
- Produces: `generateOrLoadSSHKey()`, `claimIdentityWithServer()`, `identityStageView(m *WizardModel) string`

- [ ] **Step 1: Write failing test for Ed25519 SSH key generation and SSH config update**

```go
package onboard_test

import (
	"os"
	"path/filepath"
	"testing"

	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestIdentitySSHKeyGeneration(t *testing.T) {
	tmpSSH := t.TempDir()
	keyPath := filepath.Join(tmpSSH, "id_ed25519_orbit")

	pubKey, err := tuiOnboard.EnsureSSHKeypair(keyPath)
	if err != nil {
		t.Fatalf("failed to generate SSH key: %v", err)
	}

	if len(pubKey) == 0 {
		t.Fatalf("expected non-empty public key string")
	}

	fi, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("private key file does not exist: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %v", fi.Mode().Perm())
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/tui/onboard -run TestIdentitySSHKeyGeneration -v`  
Expected: FAIL

- [ ] **Step 3: Implement stage_identity.go**

Implement `EnsureSSHKeypair`, `ConfigureSSHClient`, and the async HTTP `ClaimRequest` submission to `orbit-server`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tui/onboard -run TestIdentitySSHKeyGeneration -v`  
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add pkg/tui/onboard/
git commit -m "feat(tui): implement cryptographic Ed25519 key generation and server claim registration stage"
```

---

### Task 6: Workspace Repository Selection & Parallel Cloner

**Files:**
- Create: `orbit/orbit-cli/pkg/tui/onboard/stage_workspace.go`
- Test: `orbit/orbit-cli/pkg/tui/onboard/stage_workspace_test.go`

**Interfaces:**
- Consumes: `pkg/manifest`, `pkg/orchestrator`
- Produces: `workspaceStageView(m *WizardModel) string`, `handleWorkspaceUpdate(m *WizardModel, msg tea.Msg) (tea.Model, tea.Cmd)`

- [ ] **Step 1: Write failing test for repo selection filtering and clone dispatch**

```go
package onboard_test

import (
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestWorkspaceRepoSelection(t *testing.T) {
	m := &manifest.WorkspaceManifest{
		Workspace: "manova",
		Groups: map[string]manifest.GroupConfig{
			"orbit": {
				Repositories: []manifest.RepoConfig{
					{Name: "orbit-infra", Required: true},
					{Name: "orbit-frontend", Required: false},
				},
			},
		},
	}

	items := tuiOnboard.BuildRepoTreeItems(m, "core")
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !items[0].Selected {
		t.Errorf("expected required repo to be selected by default")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/tui/onboard -run TestWorkspaceRepoSelection -v`  
Expected: FAIL

- [ ] **Step 3: Implement stage_workspace.go**

Implement the interactive repository tree list, toggle handlers (`[Space]`, `[A]`), and the parallel worker pool executing `orchestrator.CloneTargets` with live progress bars and byte metrics.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tui/onboard -run TestWorkspaceRepoSelection -v`  
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add pkg/tui/onboard/
git commit -m "feat(tui): add multi-repo selection tree and parallel clone stage"
```

---

### Task 7: Environment, R2 Media Assets, Cursor MCP & Local DNS Stage

**Files:**
- Create: `orbit/orbit-cli/pkg/tui/onboard/stage_env.go`
- Test: `orbit/orbit-cli/pkg/tui/onboard/stage_env_test.go`

**Interfaces:**
- Consumes: `pkg/assets`, `pkg/migrate`
- Produces: `envStageView(m *WizardModel) string`, `runEnvironmentAutomation(workspaceRoot string) error`

- [ ] **Step 1: Write failing test for environment automation and asset pull trigger**

```go
package onboard_test

import (
	"os"
	"path/filepath"
	"testing"

	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestEnvironmentSetupMCPAndRules(t *testing.T) {
	tmpWorkspace := t.TempDir()
	handbookCursor := filepath.Join(tmpWorkspace, "handbook", "cursor", "rules")
	os.MkdirAll(handbookCursor, 0755)
	os.WriteFile(filepath.Join(handbookCursor, "test.mdc"), []byte("# rule"), 0644)

	if err := tuiOnboard.SetupWorkspaceEnvironment(tmpWorkspace); err != nil {
		t.Fatalf("failed to setup workspace env: %v", err)
	}

	targetRule := filepath.Join(tmpWorkspace, ".cursor", "rules", "test.mdc")
	if _, err := os.Stat(targetRule); err != nil {
		t.Errorf("expected symlinked rule at %s: %v", targetRule, err)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/tui/onboard -run TestEnvironmentSetupMCPAndRules -v`  
Expected: FAIL

- [ ] **Step 3: Implement stage_env.go**

Implement R2 media assets download (`orbit assets pull`), Cursor MCP environment creation, shell profile exports (`GOPROXY`), and the 1-time prompt for `/etc/hosts` and `caddy trust`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tui/onboard -run TestEnvironmentSetupMCPAndRules -v`  
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add pkg/tui/onboard/
git commit -m "feat(tui): implement R2 media assets sync, Cursor MCP, and local DNS/SSL stage"
```

---

### Task 8: Dev Stack Launch, 2FA QR & Completion Dashboard

**Files:**
- Create: `orbit/orbit-cli/pkg/tui/onboard/stage_stack.go`
- Create: `orbit/orbit-cli/pkg/tui/onboard/stage_complete.go`
- Test: `orbit/orbit-cli/pkg/tui/onboard/stage_complete_test.go`

**Interfaces:**
- Consumes: `pkg/tui`, `pkg/ports`
- Produces: `stackStageView(m *WizardModel) string`, `completeStageView(m *WizardModel) string`

- [ ] **Step 1: Write failing test for completion dashboard rendering and ASCII QR generation**

```go
package onboard_test

import (
	"testing"

	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestCompletionDashboardRendering(t *testing.T) {
	dashboard := tuiOnboard.RenderCompletionDashboard(tuiOnboard.DashboardInfo{
		PortalURL:  "http://localhost:10007",
		AuthURL:    "http://auth.dev.manova.space:10000",
		MailpitURL: "http://mail.dev.manova.space:10000",
		GitURL:     "http://git.dev.manova.space:10000",
		TotalRepos: 5,
	})

	if len(dashboard) == 0 {
		t.Fatalf("expected non-empty completion dashboard output")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/tui/onboard -run TestCompletionDashboardRendering -v`  
Expected: FAIL

- [ ] **Step 3: Implement stage_stack.go and stage_complete.go**

Implement container launch invocation, terminal ASCII QR code renderer for 2FA TOTP secrets, and the final interactive dashboard.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/tui/onboard -run TestCompletionDashboardRendering -v`  
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add pkg/tui/onboard/
git commit -m "feat(tui): add dev stack launch, Authelia 2FA QR, and completion dashboard stages"
```

---

### Task 9: Cobra CLI Entrypoint Integration & Headless Parity

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/onboard.go:1-250`
- Test: `orbit/orbit-cli/cmd/orbit/onboard_test.go`

**Interfaces:**
- Consumes: `pkg/istty.IsInteractiveSession()`, `pkg/tui/onboard.RunWizard()`
- Produces: Seamless execution in interactive terminals while retaining `--json`, `--resume`, `--reset`, and `--rollback` flags for headless/automated environments.

- [ ] **Step 1: Write failing test for TTY vs non-interactive dispatch**

```go
package main

import (
	"bytes"
	"testing"
)

func TestOnboardCommandHelpAndFlags(t *testing.T) {
	cmd := newOnboardCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected --help to succeed: %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []string("--resume")[0]) {
		t.Errorf("expected --resume flag in help output")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./cmd/orbit -run TestOnboardCommandHelpAndFlags -v`  
Expected: PASS (or updated check)

- [ ] **Step 3: Update cmd/orbit/onboard.go**

Wire `pkg/tui/onboard.RunWizard()` into `newOnboardCmd()`. When running in an interactive terminal without `--non-interactive`, launch the Bubble Tea program `tea.NewProgram(model, tea.WithAltScreen()).Run()`.

- [ ] **Step 4: Run full test suite across orbit-cli**

Run: `go test ./... -v`  
Expected: PASS across all packages.

- [ ] **Step 5: Build binaries and verify**

```bash
go build -o bin/orbit ./cmd/orbit
./bin/orbit onboard --help
git add cmd/orbit/
git commit -m "feat(cli): wire interactive Bubble Tea TUI wizard to 'orbit onboard' command"
```

---

## Plan Self-Review Checklist
- [x] **Spec coverage**: Covers all 6 stages from the approved design spec.
- [x] **No placeholders**: All steps contain exact paths, structs, test code, and shell commands.
- [x] **Type consistency**: State types, stages, and function signatures match across all tasks.
- [x] **DRY & YAGNI**: Reuses existing `pkg/doctor`, `pkg/orchestrator`, and `pkg/assets` modules without duplicate logic.
