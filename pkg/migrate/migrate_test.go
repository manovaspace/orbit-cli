package migrate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationStatePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, ".orbit", "migrations.json")

	engine := NewEngine(tmpDir, stateFile)
	applied, err := engine.Apply([]Migration{
		{ID: "001_install_hooks", Description: "Install git hooks", Run: func(string) error { return nil }},
		{ID: "002_symlink_rules", Description: "Symlink Cursor rules", Run: func(string) error { return nil }},
	})
	if err != nil || len(applied) != 2 {
		t.Fatalf("expected 2 applied migrations, got %d (err: %v)", len(applied), err)
	}

	// Running again should apply 0
	appliedSecond, err := engine.Apply([]Migration{
		{ID: "001_install_hooks", Description: "Install git hooks", Run: func(string) error { return nil }},
		{ID: "002_symlink_rules", Description: "Symlink Cursor rules", Run: func(string) error { return nil }},
	})
	if err != nil {
		t.Fatalf("unexpected error on second apply: %v", err)
	}
	if len(appliedSecond) != 0 {
		t.Errorf("expected 0 new migrations, got %d", len(appliedSecond))
	}
}

func TestEngineLoadAndSaveState(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "custom", "state.json")

	engine := NewEngine(tmpDir, stateFile)

	// Check WorkspaceRoot and StatePath getters
	if engine.WorkspaceRoot() != tmpDir {
		t.Errorf("expected workspaceRoot %s, got %s", tmpDir, engine.WorkspaceRoot())
	}
	if engine.StatePath() != stateFile {
		t.Errorf("expected statePath %s, got %s", stateFile, engine.StatePath())
	}

	// Nil MigrationState receiver
	var nilState *MigrationState
	if nilState.IsApplied("test") {
		t.Errorf("expected nilState.IsApplied() to be false")
	}

	// Loading non-existent state should return empty state
	state, err := engine.LoadState()
	if err != nil {
		t.Fatalf("unexpected error loading non-existent state: %v", err)
	}
	if state.Version != DefaultStateVersion {
		t.Errorf("expected default version %s, got %s", DefaultStateVersion, state.Version)
	}
	if len(state.Applied) != 0 {
		t.Errorf("expected 0 applied migrations, got %d", len(state.Applied))
	}
	if state.IsApplied("test") {
		t.Errorf("expected IsApplied('test') to be false")
	}

	// Save nil state should error
	if err := engine.SaveState(nil); err == nil {
		t.Errorf("expected error saving nil state, got nil")
	}

	// Save valid state with empty version and nil applied (should normalize)
	stateToSave := &MigrationState{}
	stateToSave.Applied = append(stateToSave.Applied, AppliedMigrationRecord{
		ID:          "test_001",
		Description: "Initial test migration",
	})
	if err := engine.SaveState(stateToSave); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Reload state and verify
	loaded, err := engine.LoadState()
	if err != nil {
		t.Fatalf("failed to reload state: %v", err)
	}
	if !loaded.IsApplied("test_001") {
		t.Errorf("expected IsApplied('test_001') to be true")
	}
	if len(loaded.Applied) != 1 {
		t.Errorf("expected 1 applied record, got %d", len(loaded.Applied))
	}
	if loaded.Applied[0].ID != "test_001" {
		t.Errorf("expected ID 'test_001', got %s", loaded.Applied[0].ID)
	}

	// Corrupted state file
	_ = os.WriteFile(stateFile, []byte("{invalid json"), 0644)
	if _, err := engine.LoadState(); err == nil {
		t.Errorf("expected error loading corrupted state file, got nil")
	}

	// Save to uncreatable path
	badEngine := NewEngine(tmpDir, filepath.Join(stateFile, "cannot_create_dir_under_file", "state.json"))
	if err := badEngine.SaveState(&MigrationState{}); err == nil {
		t.Errorf("expected error saving state to invalid directory path, got nil")
	}
}

func TestEnginePending(t *testing.T) {
	tmpDir := t.TempDir()
	engine := NewEngine(tmpDir, "")

	allMigrations := []Migration{
		{ID: "m1", Description: "Migration 1"},
		{ID: "m2", Description: "Migration 2"},
		{ID: "m3", Description: "Migration 3"},
	}

	// All should be pending initially
	pending, err := engine.Pending(allMigrations)
	if err != nil {
		t.Fatalf("unexpected error getting pending: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending, got %d", len(pending))
	}

	// Apply first migration (with nil Run func)
	_, err = engine.Apply([]Migration{allMigrations[0]})
	if err != nil {
		t.Fatalf("unexpected error applying m1: %v", err)
	}

	// Now m2 and m3 should be pending
	pending, err = engine.Pending(allMigrations)
	if err != nil {
		t.Fatalf("unexpected error getting pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
	if pending[0].ID != "m2" || pending[1].ID != "m3" {
		t.Errorf("expected m2 and m3 pending, got %v and %v", pending[0].ID, pending[1].ID)
	}

	// Apply remaining migrations
	_, err = engine.Apply(pending)
	if err != nil {
		t.Fatalf("unexpected error applying pending: %v", err)
	}

	// Now 0 should be pending
	pending, err = engine.Pending(allMigrations)
	if err != nil {
		t.Fatalf("unexpected error getting pending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}

	// Corrupted state file should fail Pending and Apply
	statePath := engine.StatePath()
	_ = os.WriteFile(statePath, []byte("{invalid json"), 0644)
	if _, err := engine.Pending(allMigrations); err == nil {
		t.Errorf("expected Pending to fail on corrupted state file")
	}
	if _, err := engine.Apply(allMigrations); err == nil {
		t.Errorf("expected Apply to fail on corrupted state file")
	}
}

func TestEngineApplyFailure(t *testing.T) {
	tmpDir := t.TempDir()
	engine := NewEngine(tmpDir, "")

	executed := make(map[string]bool)

	migrations := []Migration{
		{
			ID:          "m1",
			Description: "Migration 1",
			Run: func(root string) error {
				executed["m1"] = true
				return nil
			},
		},
		{
			ID:          "m2",
			Description: "Migration 2",
			Run: func(root string) error {
				executed["m2"] = true
				return fmt.Errorf("intentional failure in m2")
			},
		},
		{
			ID:          "m3",
			Description: "Migration 3",
			Run: func(root string) error {
				executed["m3"] = true
				return nil
			},
		},
	}

	results, err := engine.Apply(migrations)
	if err == nil {
		t.Fatalf("expected error from Apply when migration fails, got nil")
	}

	if !strings.Contains(err.Error(), "intentional failure in m2") {
		t.Errorf("expected error message to contain 'intentional failure in m2', got: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results (1 success, 1 failure), got %d", len(results))
	}

	if !results[0].Success || results[0].ID != "m1" {
		t.Errorf("expected result 0 to be m1 success, got %+v", results[0])
	}
	if results[1].Success || results[1].ID != "m2" || results[1].Error != "intentional failure in m2" {
		t.Errorf("expected result 1 to be m2 failure, got %+v", results[1])
	}

	if executed["m3"] {
		t.Errorf("migration m3 should not have executed after m2 failure")
	}

	// Check that state persisted only m1
	state, err := engine.LoadState()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if !state.IsApplied("m1") {
		t.Errorf("expected m1 to be recorded in state")
	}
	if state.IsApplied("m2") || state.IsApplied("m3") {
		t.Errorf("expected m2 and m3 NOT to be recorded in state")
	}
}

func TestEnsureWorkspaceDirs(t *testing.T) {
	tmpDir := t.TempDir()

	if err := EnsureWorkspaceDirs(tmpDir); err != nil {
		t.Fatalf("EnsureWorkspaceDirs failed: %v", err)
	}

	expectedDirs := []string{"orbit", "manovaspace", "clients", "documents", "share", "temp"}
	for _, dir := range expectedDirs {
		p := filepath.Join(tmpDir, dir)
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			t.Errorf("expected directory %s to exist, got err: %v", p, err)
		}
	}
}

func TestInstallGitHooks(t *testing.T) {
	// Case 1: No .githooks directory -> should return nil
	tmpDir1 := t.TempDir()
	if err := InstallGitHooks(tmpDir1); err != nil {
		t.Errorf("expected nil when .githooks does not exist, got: %v", err)
	}

	// Case 2: .githooks exists in a git repo -> should configure core.hooksPath
	tmpDir2 := t.TempDir()
	githooksDir := filepath.Join(tmpDir2, ".githooks")
	if err := os.MkdirAll(githooksDir, 0755); err != nil {
		t.Fatalf("failed to create .githooks: %v", err)
	}

	// Initialize git repo in tmpDir2
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tmpDir2
	if err := initCmd.Run(); err != nil {
		t.Skipf("skipping git test: git init failed: %v", err)
	}

	if err := InstallGitHooks(tmpDir2); err != nil {
		t.Fatalf("InstallGitHooks failed: %v", err)
	}

	// Verify git config core.hooksPath
	configCmd := exec.Command("git", "config", "--get", "core.hooksPath")
	configCmd.Dir = tmpDir2
	out, err := configCmd.Output()
	if err != nil {
		t.Fatalf("failed to read core.hooksPath: %v", err)
	}
	if strings.TrimSpace(string(out)) != ".githooks" {
		t.Errorf("expected core.hooksPath to be '.githooks', got: %s", string(out))
	}

	// Case 3: .githooks exists in a non-git directory -> should return nil gracefully
	tmpDir3 := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir3, ".githooks"), 0755)
	if err := InstallGitHooks(tmpDir3); err != nil {
		t.Errorf("expected nil when non-git dir has .githooks, got: %v", err)
	}
}

func TestSetupMCPEnvironment(t *testing.T) {
	// Case 1: .cursor/mcp.env already exists -> should not be overwritten
	tmpDir1 := t.TempDir()
	cursorDir1 := filepath.Join(tmpDir1, ".cursor")
	_ = os.MkdirAll(cursorDir1, 0755)
	existingPath := filepath.Join(cursorDir1, "mcp.env")
	_ = os.WriteFile(existingPath, []byte("EXISTING_TOKEN=123\n"), 0600)

	if err := SetupMCPEnvironment(tmpDir1); err != nil {
		t.Fatalf("SetupMCPEnvironment failed: %v", err)
	}
	content, _ := os.ReadFile(existingPath)
	if string(content) != "EXISTING_TOKEN=123\n" {
		t.Errorf("existing mcp.env was overwritten: %s", string(content))
	}

	// Case 2: .cursor/mcp.env missing, handbook/cursor/mcp.env.example exists -> copy it
	tmpDir2 := t.TempDir()
	handbookExampleDir := filepath.Join(tmpDir2, "handbook", "cursor")
	_ = os.MkdirAll(handbookExampleDir, 0755)
	_ = os.WriteFile(filepath.Join(handbookExampleDir, "mcp.env.example"), []byte("FORGEJO_ACCESS_TOKEN=example_token\n"), 0644)

	if err := SetupMCPEnvironment(tmpDir2); err != nil {
		t.Fatalf("SetupMCPEnvironment failed: %v", err)
	}
	createdEnv := filepath.Join(tmpDir2, ".cursor", "mcp.env")
	content2, err := os.ReadFile(createdEnv)
	if err != nil {
		t.Fatalf("failed to read created mcp.env: %v", err)
	}
	if string(content2) != "FORGEJO_ACCESS_TOKEN=example_token\n" {
		t.Errorf("unexpected mcp.env content: %s", string(content2))
	}
	fi, err := os.Stat(createdEnv)
	if err != nil {
		t.Fatalf("failed to stat created mcp.env: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %#o", fi.Mode().Perm())
	}

	// Case 3: No example file -> create default template
	tmpDir3 := t.TempDir()
	if err := SetupMCPEnvironment(tmpDir3); err != nil {
		t.Fatalf("SetupMCPEnvironment failed: %v", err)
	}
	createdEnv3 := filepath.Join(tmpDir3, ".cursor", "mcp.env")
	content3, err := os.ReadFile(createdEnv3)
	if err != nil {
		t.Fatalf("failed to read created mcp.env: %v", err)
	}
	if !strings.Contains(string(content3), "Cursor MCP") {
		t.Errorf("expected default header in mcp.env, got: %s", string(content3))
	}
}

func TestSymlinkCursorRules(t *testing.T) {
	// Case 1: handbook/cursor does not exist -> returns nil
	tmpDir1 := t.TempDir()
	if err := SymlinkCursorRules(tmpDir1); err != nil {
		t.Errorf("expected nil when handbook/cursor does not exist, got: %v", err)
	}

	// Case 2: handbook/cursor exists with rules, skills, and workspace files
	tmpDir2 := t.TempDir()
	handbookRules := filepath.Join(tmpDir2, "handbook", "cursor", "rules")
	handbookSkills := filepath.Join(tmpDir2, "handbook", "cursor", "skills", "test-skill")
	_ = os.MkdirAll(handbookRules, 0755)
	_ = os.MkdirAll(handbookSkills, 0755)

	_ = os.WriteFile(filepath.Join(handbookRules, "sample.mdc"), []byte("# Sample Rule"), 0644)
	_ = os.WriteFile(filepath.Join(handbookSkills, "SKILL.md"), []byte("# Sample Skill"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir2, "handbook", "cursor", ".cursorignore"), []byte("node_modules\n"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir2, "handbook", "cursor", "AGENTS.workspace.md"), []byte("# Agents"), 0644)

	if err := SymlinkCursorRules(tmpDir2); err != nil {
		t.Fatalf("SymlinkCursorRules failed: %v", err)
	}

	// Verify symlinks
	ruleSymlink := filepath.Join(tmpDir2, ".cursor", "rules", "sample.mdc")
	if target, err := os.Readlink(ruleSymlink); err != nil {
		t.Errorf("expected %s to be a symlink: %v", ruleSymlink, err)
	} else if target != filepath.Join(handbookRules, "sample.mdc") {
		t.Errorf("expected symlink target %s, got %s", filepath.Join(handbookRules, "sample.mdc"), target)
	}

	skillSymlink := filepath.Join(tmpDir2, ".cursor", "skills", "test-skill")
	if target, err := os.Readlink(skillSymlink); err != nil {
		t.Errorf("expected %s to be a symlink: %v", skillSymlink, err)
	} else if target != handbookSkills {
		t.Errorf("expected symlink target %s, got %s", handbookSkills, target)
	}

	agentsSymlink := filepath.Join(tmpDir2, "AGENTS.md")
	if target, err := os.Readlink(agentsSymlink); err != nil {
		t.Errorf("expected %s to be a symlink: %v", agentsSymlink, err)
	} else if target != filepath.Join(tmpDir2, "handbook", "cursor", "AGENTS.workspace.md") {
		t.Errorf("expected symlink target %s, got %s", filepath.Join(tmpDir2, "handbook", "cursor", "AGENTS.workspace.md"), target)
	}

	// Case 3: Re-running is idempotent
	if err := SymlinkCursorRules(tmpDir2); err != nil {
		t.Errorf("re-running SymlinkCursorRules failed: %v", err)
	}

	// Case 4: Overwriting existing regular file or outdated symlink
	dummyFile := filepath.Join(tmpDir2, ".cursor", "rules", "sample.mdc")
	_ = os.Remove(dummyFile)
	_ = os.WriteFile(dummyFile, []byte("old non-symlink file"), 0644)
	if err := SymlinkCursorRules(tmpDir2); err != nil {
		t.Fatalf("SymlinkCursorRules failed when replacing regular file: %v", err)
	}
	if target, err := os.Readlink(dummyFile); err != nil || target != filepath.Join(handbookRules, "sample.mdc") {
		t.Errorf("expected symlink to be restored, got target: %s, err: %v", target, err)
	}
}

func TestSetupWorkspace(t *testing.T) {
	tmpDir := t.TempDir()

	// Create handbook/cursor structure to test symlinking
	handbookRules := filepath.Join(tmpDir, "handbook", "cursor", "rules")
	_ = os.MkdirAll(handbookRules, 0755)
	_ = os.WriteFile(filepath.Join(handbookRules, "sample.mdc"), []byte("# Sample Rule"), 0644)

	if err := SetupWorkspace(tmpDir); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	// Verify standard directories created
	expectedDirs := []string{"orbit", "manovaspace", "clients", "documents", "share", "temp"}
	for _, dir := range expectedDirs {
		p := filepath.Join(tmpDir, dir)
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			t.Errorf("expected directory %s to exist, err: %v", p, err)
		}
	}

	// Verify .cursor/mcp.env created
	mcpEnv := filepath.Join(tmpDir, ".cursor", "mcp.env")
	if _, err := os.Stat(mcpEnv); err != nil {
		t.Errorf("expected .cursor/mcp.env to exist, err: %v", err)
	}

	// Verify symlinks created
	ruleSymlink := filepath.Join(tmpDir, ".cursor", "rules", "sample.mdc")
	if _, err := os.Readlink(ruleSymlink); err != nil {
		t.Errorf("expected rule symlink %s to exist: %v", ruleSymlink, err)
	}
}

func TestRunPendingMigrations(t *testing.T) {
	tmpDir := t.TempDir()

	results, err := RunPendingMigrations(tmpDir)
	if err != nil {
		t.Fatalf("RunPendingMigrations failed: %v", err)
	}

	builtins := GetBuiltinMigrations()
	if len(results) != len(builtins) {
		t.Errorf("expected %d results, got %d", len(builtins), len(results))
	}

	// Test with a custom engine and migration
	engine := NewEngine(tmpDir, "")
	if engine.StatePath() != filepath.Join(tmpDir, ".orbit", "migrations.json") {
		t.Errorf("expected state path %s, got %s", filepath.Join(tmpDir, ".orbit", "migrations.json"), engine.StatePath())
	}

	customResults, err := engine.Apply([]Migration{
		{
			ID:          "test_custom_001",
			Description: "Custom migration test",
			Run: func(root string) error {
				return nil
			},
		},
	})
	if err != nil || len(customResults) != 1 {
		t.Fatalf("expected 1 custom migration result, got %d (err: %v)", len(customResults), err)
	}

	// Verify state file was created at .orbit/migrations.json
	statePath := filepath.Join(tmpDir, ".orbit", "migrations.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state file %s to exist: %v", statePath, err)
	}
}

func TestGetBuiltinMigrations(t *testing.T) {
	builtins := GetBuiltinMigrations()
	if len(builtins) != 0 {
		t.Errorf("expected 0 default builtin migrations (bootstrapped in orbit init), got %d", len(builtins))
	}
}
