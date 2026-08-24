package migrate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestEnvironment(t *testing.T) (string, string) {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("SHELL", "/bin/bash")
	// Disable real systemctl interactions in tests
	t.Setenv("MANOVA_FORCE_DETACHED", "1")

	// Create mock .bashrc
	bashrc := filepath.Join(tmpHome, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# Mock bashrc\n"), 0644); err != nil {
		t.Fatalf("failed to create mock bashrc: %v", err)
	}

	statePath := filepath.Join(tmpHome, ".manova", "state.json")
	return tmpHome, statePath
}

func TestRunPostUpdateMigrations_NonInteractive(t *testing.T) {
	tmpHome, statePath := setupTestEnvironment(t)

	var out bytes.Buffer
	ctx := &PostUpdateContext{
		Interactive: false,
		In:          strings.NewReader(""),
		Out:         &out,
		ExecPath:    "/usr/local/bin/manova",
		PrevVersion: "0.1.0",
		NewVersion:  "0.2.1",
		StatePath:   statePath,
	}

	results, err := RunPostUpdateMigrations(ctx)
	if err != nil {
		t.Fatalf("RunPostUpdateMigrations failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 migration results, got %d", len(results))
	}

	// 001_upgrade_systemd_worker should succeed (skipped systemd because MANOVA_FORCE_DETACHED=1)
	if results[0].ID != "001_upgrade_systemd_worker" || !results[0].Success || results[0].Skipped {
		t.Errorf("expected 001 to succeed and not be skipped, got: %+v", results[0])
	}

	// 002_ensure_shell_completion should succeed
	if results[1].ID != "002_ensure_shell_completion" || !results[1].Success || results[1].Skipped {
		t.Errorf("expected 002 to succeed and not be skipped, got: %+v", results[1])
	}

	// Verify shell completion was added to .bashrc
	bashrcData, err := os.ReadFile(filepath.Join(tmpHome, ".bashrc"))
	if err != nil {
		t.Fatalf("failed to read .bashrc: %v", err)
	}
	if !strings.Contains(string(bashrcData), "manova completion") {
		t.Errorf("expected shell completion in .bashrc, got:\n%s", string(bashrcData))
	}

	// 003_prompt_m_alias should be skipped in non-interactive mode
	if results[2].ID != "003_prompt_m_alias" || !results[2].Success || !results[2].Skipped {
		t.Errorf("expected 003 to be skipped in non-interactive mode, got: %+v", results[2])
	}

	// Verify state.json was written with 001 and 002 applied, but not 003
	engine := NewEngine("", statePath)
	state, err := engine.LoadState()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if !state.IsApplied("001_upgrade_systemd_worker") {
		t.Errorf("expected 001 to be recorded in state")
	}
	if !state.IsApplied("002_ensure_shell_completion") {
		t.Errorf("expected 002 to be recorded in state")
	}
	if state.IsApplied("003_prompt_m_alias") {
		t.Errorf("expected 003 NOT to be recorded in state when skipped")
	}
	if state.Version != "0.2.1" {
		t.Errorf("expected state version to be 0.2.1, got %s", state.Version)
	}
}

func TestRunPostUpdateMigrations_Interactive_PromptAcceptY(t *testing.T) {
	tmpHome, statePath := setupTestEnvironment(t)

	var out bytes.Buffer
	ctx := &PostUpdateContext{
		Interactive: true,
		In:          strings.NewReader("y\n"),
		Out:         &out,
		ExecPath:    "/usr/local/bin/manova",
		PrevVersion: "0.1.0",
		NewVersion:  "0.2.1",
		StatePath:   statePath,
	}

	results, err := RunPostUpdateMigrations(ctx)
	if err != nil {
		t.Fatalf("RunPostUpdateMigrations failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 migration results, got %d", len(results))
	}

	// Check that prompt was printed
	promptOutput := out.String()
	if !strings.Contains(promptOutput, "? Set 'm' as a short shell alias for 'manova'? [Y/n]") {
		t.Errorf("expected prompt text in output, got: %q", promptOutput)
	}

	// Check 003 succeeded and not skipped
	if results[2].ID != "003_prompt_m_alias" || !results[2].Success || results[2].Skipped {
		t.Errorf("expected 003 to succeed, got: %+v", results[2])
	}

	// Verify alias 'alias m="manova"' was added to .bashrc
	bashrcData, err := os.ReadFile(filepath.Join(tmpHome, ".bashrc"))
	if err != nil {
		t.Fatalf("failed to read .bashrc: %v", err)
	}
	if !strings.Contains(string(bashrcData), "alias m=\"manova\"") && !strings.Contains(string(bashrcData), "alias m='manova'") {
		t.Errorf("expected alias m in .bashrc, got:\n%s", string(bashrcData))
	}

	// Verify state.json has all 3 recorded
	engine := NewEngine("", statePath)
	state, err := engine.LoadState()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if !state.IsApplied("003_prompt_m_alias") {
		t.Errorf("expected 003 to be recorded in state")
	}
}

func TestRunPostUpdateMigrations_Interactive_PromptDeclineN(t *testing.T) {
	tmpHome, statePath := setupTestEnvironment(t)

	var out bytes.Buffer
	ctx := &PostUpdateContext{
		Interactive: true,
		In:          strings.NewReader("n\n"),
		Out:         &out,
		ExecPath:    "/usr/local/bin/manova",
		PrevVersion: "0.1.0",
		NewVersion:  "0.2.1",
		StatePath:   statePath,
	}

	results, err := RunPostUpdateMigrations(ctx)
	if err != nil {
		t.Fatalf("RunPostUpdateMigrations failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 migration results, got %d", len(results))
	}

	// Check that prompt was printed
	promptOutput := out.String()
	if !strings.Contains(promptOutput, "? Set 'm' as a short shell alias for 'manova'? [Y/n]") {
		t.Errorf("expected prompt text in output, got: %q", promptOutput)
	}

	// 003 should succeed and be marked applied (declined by user, so migration is resolved)
	if results[2].ID != "003_prompt_m_alias" || !results[2].Success || results[2].Skipped {
		t.Errorf("expected 003 to succeed (resolved decline), got: %+v", results[2])
	}

	// Verify alias is NOT in .bashrc
	bashrcData, err := os.ReadFile(filepath.Join(tmpHome, ".bashrc"))
	if err != nil {
		t.Fatalf("failed to read .bashrc: %v", err)
	}
	if strings.Contains(string(bashrcData), "alias m=") {
		t.Errorf("alias m should not be present in .bashrc, got:\n%s", string(bashrcData))
	}

	// Verify state.json records 003 as applied so prompt is never repeated
	engine := NewEngine("", statePath)
	state, err := engine.LoadState()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if !state.IsApplied("003_prompt_m_alias") {
		t.Errorf("expected 003 to be recorded in state after user decline")
	}
}

func TestRunPostUpdateMigrations_Idempotency(t *testing.T) {
	_, statePath := setupTestEnvironment(t)

	// First run: interactive with 'y'
	ctx1 := &PostUpdateContext{
		Interactive: true,
		In:          strings.NewReader("y\n"),
		Out:         &bytes.Buffer{},
		ExecPath:    "/usr/local/bin/manova",
		StatePath:   statePath,
	}

	res1, err := RunPostUpdateMigrations(ctx1)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if len(res1) != 3 {
		t.Fatalf("expected 3 results on first run, got %d", len(res1))
	}

	// Second run: should be idempotent (0 new migrations executed)
	var out2 bytes.Buffer
	ctx2 := &PostUpdateContext{
		Interactive: true,
		In:          strings.NewReader("y\n"),
		Out:         &out2,
		ExecPath:    "/usr/local/bin/manova",
		StatePath:   statePath,
	}

	res2, err := RunPostUpdateMigrations(ctx2)
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if len(res2) != 0 {
		t.Errorf("expected 0 migrations on second run, got %d", len(res2))
	}
	if out2.Len() != 0 {
		t.Errorf("expected no output on second run, got: %q", out2.String())
	}
}

func TestRunPostUpdateMigrations_CommandTakenSkipped(t *testing.T) {
	tmpHome, statePath := setupTestEnvironment(t)

	// Pre-populate .bashrc with existing m alias
	bashrc := filepath.Join(tmpHome, ".bashrc")
	_ = os.WriteFile(bashrc, []byte("alias m='custom_command'\n"), 0644)

	var out bytes.Buffer
	ctx := &PostUpdateContext{
		Interactive: true,
		In:          strings.NewReader("y\n"),
		Out:         &out,
		ExecPath:    "/usr/local/bin/manova",
		StatePath:   statePath,
	}

	results, err := RunPostUpdateMigrations(ctx)
	if err != nil {
		t.Fatalf("RunPostUpdateMigrations failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// 003 should have resolved because 'm' was already taken, not prompting the user
	if out.Len() != 0 {
		t.Errorf("expected no prompt when command is taken, got: %q", out.String())
	}

	// Verify original alias preserved
	data, _ := os.ReadFile(bashrc)
	if !strings.Contains(string(data), "custom_command") {
		t.Errorf("original custom alias should be preserved, got: %s", string(data))
	}
}

func TestRunPostUpdateMigrations_NilContext(t *testing.T) {
	_, statePath := setupTestEnvironment(t)

	// PostUpdateContext with defaults
	results, err := RunPostUpdateMigrations(&PostUpdateContext{
		StatePath: statePath,
	})
	if err != nil {
		t.Fatalf("expected no error with default context, got: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestPromptMAlias_ErrorHandling(t *testing.T) {
	tmpHome, _ := setupTestEnvironment(t)

	// Test EOF without input
	var out bytes.Buffer
	ctxEOF := &PostUpdateContext{
		Interactive: true,
		In:          strings.NewReader(""),
		Out:         &out,
	}
	applied, msg, err := PromptMAlias(ctxEOF)
	if err != nil {
		t.Fatalf("unexpected error on EOF: %v", err)
	}
	if !applied || !strings.Contains(msg, "no input") {
		t.Errorf("expected applied true on EOF, got applied=%v msg=%q", applied, msg)
	}

	// Test Read error
	ctxErr := &PostUpdateContext{
		Interactive: true,
		In:          errReader{},
		Out:         &out,
	}
	_, _, err = PromptMAlias(ctxErr)
	if err == nil {
		t.Errorf("expected error on read failure, got nil")
	}

	// Test default 'yes' when user presses Enter
	ctxEnter := &PostUpdateContext{
		Interactive: true,
		In:          strings.NewReader("\n"),
		Out:         &out,
	}
	applied, _, err = PromptMAlias(ctxEnter)
	if err != nil || !applied {
		t.Fatalf("expected Enter to accept default alias, err=%v, applied=%v", err, applied)
	}
	data, _ := os.ReadFile(filepath.Join(tmpHome, ".bashrc"))
	if !strings.Contains(string(data), "alias m=") {
		t.Errorf("expected alias m in .bashrc on Enter default")
	}
}

func TestResolvePostUpdateStatePath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Custom absolute path
	custom := filepath.Join(tmpHome, "custom.json")
	if resolvePostUpdateStatePath(custom) != custom {
		t.Errorf("expected %s, got %s", custom, resolvePostUpdateStatePath(custom))
	}

	// Default tilde path
	expectedDefault := filepath.Join(tmpHome, ".manova", "state.json")
	if resolvePostUpdateStatePath("") != expectedDefault {
		t.Errorf("expected %s, got %s", expectedDefault, resolvePostUpdateStatePath(""))
	}

	// Tilde only
	if resolvePostUpdateStatePath("~") != tmpHome {
		t.Errorf("expected %s, got %s", tmpHome, resolvePostUpdateStatePath("~"))
	}
}
