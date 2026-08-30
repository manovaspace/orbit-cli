package onboard_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestSetupWorkspaceEnvironment(t *testing.T) {
	tmp := t.TempDir()

	// Create handbook/cursor/rules source
	handbookRules := filepath.Join(tmp, "handbook", "cursor", "rules")
	os.MkdirAll(handbookRules, 0755)
	os.WriteFile(filepath.Join(handbookRules, "test.mdc"), []byte("# rule"), 0644)

	if err := tuiOnboard.SetupWorkspaceEnvironment(tmp); err != nil {
		t.Fatalf("SetupWorkspaceEnvironment failed: %v", err)
	}

	// Verify .cursor/rules/test.mdc symlink exists
	targetRule := filepath.Join(tmp, ".cursor", "rules", "test.mdc")
	fi, err := os.Lstat(targetRule)
	if err != nil {
		t.Errorf("expected symlinked rule at %s: %v", targetRule, err)
	} else if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to be a symlink", targetRule)
	}
}

func TestSetupWorkspaceEnvironmentNoHandbook(t *testing.T) {
	tmp := t.TempDir()
	// No handbook dir — should not error
	if err := tuiOnboard.SetupWorkspaceEnvironment(tmp); err != nil {
		t.Fatalf("expected no error with missing handbook: %v", err)
	}
}

func TestConfigureMCPEnvironment(t *testing.T) {
	tmp := t.TempDir()

	if err := tuiOnboard.ConfigureMCPEnvironment(tmp, "test-token-abc", "usr123"); err != nil {
		t.Fatalf("ConfigureMCPEnvironment failed: %v", err)
	}

	mcpEnv := filepath.Join(tmp, ".cursor", "mcp.env")
	data, err := os.ReadFile(mcpEnv)
	if err != nil {
		t.Fatalf("expected mcp.env to be created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "FORGEJO_TOKEN=test-token-abc") {
		t.Errorf("expected FORGEJO_TOKEN in mcp.env, got:\n%s", content)
	}
	if !strings.Contains(content, "FORGEJO_MCP_TOKEN=test-token-abc") {
		t.Errorf("expected FORGEJO_MCP_TOKEN in mcp.env, got:\n%s", content)
	}
	if !strings.Contains(content, "MANOVA_USER_UID=usr123") {
		t.Errorf("expected MANOVA_USER_UID in mcp.env, got:\n%s", content)
	}

	// Verify 0600 permissions
	fi, err := os.Stat(mcpEnv)
	if err != nil {
		t.Fatalf("stat mcp.env: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 on mcp.env, got %o", fi.Mode().Perm())
	}
}

func TestConfigureMCPEnvironmentIdempotent(t *testing.T) {
	tmp := t.TempDir()

	tuiOnboard.ConfigureMCPEnvironment(tmp, "token-v1", "uid1")
	tuiOnboard.ConfigureMCPEnvironment(tmp, "token-v2", "uid2")

	data, _ := os.ReadFile(filepath.Join(tmp, ".cursor", "mcp.env"))
	content := string(data)

	// Should have exactly one occurrence of each key
	if strings.Count(content, "FORGEJO_TOKEN=") != 1 {
		t.Errorf("expected exactly 1 FORGEJO_TOKEN= in mcp.env, got content:\n%s", content)
	}
	if !strings.Contains(content, "FORGEJO_TOKEN=token-v2") {
		t.Errorf("expected updated token-v2 in mcp.env, got:\n%s", content)
	}
}

func TestEnsureWireGuardConfig(t *testing.T) {
	// Override home dir via env var approach — write to tmpdir
	tmp := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", oldHome)

	configData := "[Interface]\nPrivateKey = testprivatekey\nAddress = 10.8.0.2/32\n"
	path, err := tuiOnboard.EnsureWireGuardConfig(configData)
	if err != nil {
		t.Fatalf("EnsureWireGuardConfig failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected wg0.conf to be created: %v", err)
	}
	if string(data) != configData {
		t.Errorf("unexpected wg0.conf content:\n%s", string(data))
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat wg0.conf: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 on wg0.conf, got %o", fi.Mode().Perm())
	}
}

func TestEnsureWireGuardConfigEmpty(t *testing.T) {
	tmp := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", oldHome)

	path, err := tuiOnboard.EnsureWireGuardConfig("")
	if err != nil {
		t.Fatalf("EnsureWireGuardConfig with empty data should not error: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty path for empty config data, got %q", path)
	}
}

func TestEnvModelInit(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	envModel := tuiOnboard.NewEnvModel(root)
	if envModel == nil {
		t.Fatal("expected non-nil EnvModel")
	}

	steps := envModel.Steps()
	if len(steps) == 0 {
		t.Error("expected non-empty automation steps")
	}
	for _, s := range steps {
		if s.Name == "" {
			t.Error("expected non-empty step name")
		}
		if s.Status != "pending" {
			t.Errorf("expected initial status 'pending', got %q", s.Status)
		}
	}
}

func TestEnvModelViewRendering(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	envModel := tuiOnboard.NewEnvModel(root)
	view := envModel.View()

	if !strings.Contains(view, "Environment") {
		t.Errorf("expected 'Environment' in view, got:\n%s", view)
	}
	// Should mention at least one step name
	if !strings.Contains(view, "MCP") && !strings.Contains(view, "Cursor") {
		t.Errorf("expected step name in view, got:\n%s", view)
	}
}

func TestEnvModelAutomationRunnerSuccess(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})
	root.Session = &session.SessionState{
		ForgejoToken: "tok123",
		UID:          "devuid",
	}

	envModel := tuiOnboard.NewEnvModel(root)

	// Inject a runner that always succeeds
	envModel.SetAutomationRunner(func(workspaceRoot string, steps []tuiOnboard.EnvStepItem, parent *tuiOnboard.WizardModel) []tuiOnboard.EnvStepResult {
		results := make([]tuiOnboard.EnvStepResult, len(steps))
		for i, s := range steps {
			results[i] = tuiOnboard.EnvStepResult{Name: s.Name, Success: true}
		}
		return results
	})

	cmd := envModel.RunAutomation()
	if cmd == nil {
		t.Fatal("RunAutomation returned nil cmd")
	}
	msg := cmd()
	finished, ok := msg.(tuiOnboard.EnvAutomationFinishedMsg)
	if !ok {
		t.Fatalf("expected EnvAutomationFinishedMsg, got %T", msg)
	}
	if finished.Err != nil {
		t.Errorf("unexpected error: %v", finished.Err)
	}
	if len(finished.Results) == 0 {
		t.Error("expected non-empty results")
	}
	for _, r := range finished.Results {
		if !r.Success {
			t.Errorf("expected step %q to succeed", r.Name)
		}
	}
}

func TestEnvModelAutomationRunnerFailure(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{SessionManager: sm})
	envModel := tuiOnboard.NewEnvModel(root)

	envModel.SetAutomationRunner(func(workspaceRoot string, steps []tuiOnboard.EnvStepItem, parent *tuiOnboard.WizardModel) []tuiOnboard.EnvStepResult {
		results := make([]tuiOnboard.EnvStepResult, len(steps))
		for i, s := range steps {
			if i == 0 {
				results[i] = tuiOnboard.EnvStepResult{Name: s.Name, Success: false, Error: "simulated error"}
			} else {
				results[i] = tuiOnboard.EnvStepResult{Name: s.Name, Success: true}
			}
		}
		return results
	})

	cmd := envModel.RunAutomation()
	msg := cmd()
	finished, ok := msg.(tuiOnboard.EnvAutomationFinishedMsg)
	if !ok {
		t.Fatalf("expected EnvAutomationFinishedMsg, got %T", msg)
	}
	// Should have error in results
	hasFailure := false
	for _, r := range finished.Results {
		if !r.Success {
			hasFailure = true
		}
	}
	if !hasFailure {
		t.Error("expected at least one failed step")
	}
}

func TestEnvModelRetryKeySuccess(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{SessionManager: sm})
	envModel := tuiOnboard.NewEnvModel(root)

	// Simulate error state
	envModel.SetHasError(true)

	// [R] press should trigger retry
	_, cmd := envModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Error("expected non-nil cmd from retry [R]")
	}
}

func TestEnvModelSkipKey(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{SessionManager: sm})
	root.Session = &session.SessionState{}
	envModel := tuiOnboard.NewEnvModel(root)
	envModel.SetHasError(true)

	// [S] press should skip to next stage
	_, _ = envModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if root.ActiveStage() != session.StageStack {
		t.Errorf("expected skip to transition to StageStack, got %v", root.ActiveStage())
	}
}

func TestEnvModelWizardIntegration(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{SessionManager: sm})
	root.SetStage(session.StageEnvironment)

	view := root.View()
	if !strings.Contains(view, "Environment") {
		t.Errorf("expected Environment stage view in WizardModel, got:\n%s", view)
	}
}

func TestEnvModelFinishedTransitionsStage(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{SessionManager: sm})
	root.Session = &session.SessionState{}
	envModel := tuiOnboard.NewEnvModel(root)

	allSuccess := []tuiOnboard.EnvStepResult{}
	for _, s := range envModel.Steps() {
		allSuccess = append(allSuccess, tuiOnboard.EnvStepResult{Name: s.Name, Success: true})
	}

	_, _ = envModel.Update(tuiOnboard.EnvAutomationFinishedMsg{Results: allSuccess, Err: nil})

	if root.ActiveStage() != session.StageStack {
		t.Errorf("expected transition to StageStack after env success, got %v", root.ActiveStage())
	}
}

// Suppress unused import warning on httptest if not used
var _ = httptest.NewServer
var _ = http.HandlerFunc(nil)
