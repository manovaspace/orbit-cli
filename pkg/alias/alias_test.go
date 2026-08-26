package alias

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsCommandTaken(t *testing.T) {
	// Standard Unix command that should always exist
	taken, reason := IsCommandTaken("ls")
	if !taken {
		t.Errorf("expected 'ls' to be taken, got false")
	}
	if reason == "" {
		t.Errorf("expected non-empty reason for taken command")
	}

	// High-entropy unlikely command name
	unlikelyCmd := "unlikely_orbit_fake_cmd_xyz_123"
	taken, _ = IsCommandTaken(unlikelyCmd)
	if taken {
		t.Errorf("expected %q not to be taken, got true", unlikelyCmd)
	}
}

func TestAddShellAlias(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("SHELL", "/bin/bash")

	rcFile := filepath.Join(tmpDir, ".bashrc")
	_ = os.WriteFile(rcFile, []byte("# Existing bashrc\n"), 0644)

	targetFile, err := AddShellAlias("o", "orbit")
	if err != nil {
		t.Fatalf("AddShellAlias failed: %v", err)
	}
	if targetFile != rcFile {
		t.Errorf("expected targetFile %q, got %q", rcFile, targetFile)
	}

	content, err := os.ReadFile(rcFile)
	if err != nil {
		t.Fatalf("failed to read rcFile: %v", err)
	}

	if !strings.Contains(string(content), `alias o="orbit"`) {
		t.Errorf("expected alias line in file, got: %s", string(content))
	}

	// Test idempotency (should not duplicate entry)
	_, err = AddShellAlias("o", "orbit")
	if err != nil {
		t.Fatalf("second AddShellAlias call failed: %v", err)
	}

	contentSecond, _ := os.ReadFile(rcFile)
	count := strings.Count(string(contentSecond), `alias o="orbit"`)
	if count != 1 {
		t.Errorf("expected exactly 1 instance of alias, found %d", count)
	}
}

func TestInstallShellCompletion(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("SHELL", "/bin/zsh")

	zshrc := filepath.Join(tmpDir, ".zshrc")
	_ = os.WriteFile(zshrc, []byte("# Existing zshrc\n"), 0644)

	targetFile, err := InstallShellCompletion(true)
	if err != nil {
		t.Fatalf("InstallShellCompletion failed: %v", err)
	}
	if targetFile != zshrc {
		t.Errorf("expected targetFile %q, got %q", zshrc, targetFile)
	}

	content, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatalf("failed to read zshrc: %v", err)
	}

	if !strings.Contains(string(content), "source <(orbit completion zsh)") {
		t.Errorf("expected completion source line in zshrc")
	}
	if !strings.Contains(string(content), "compdef o=orbit") {
		t.Errorf("expected compdef alias hook in zshrc")
	}

	// Test idempotency
	_, err = InstallShellCompletion(true)
	if err != nil {
		t.Fatalf("second InstallShellCompletion call failed: %v", err)
	}

	contentSecond, _ := os.ReadFile(zshrc)
	count := strings.Count(string(contentSecond), "# Orbit CLI Autocompletion")
	if count != 1 {
		t.Errorf("expected exactly 1 completion header, found %d", count)
	}
}

func TestRemoveShellConfiguration(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	bashrc := filepath.Join(tmpDir, ".bashrc")
	initialContent := `# Existing user line
# Orbit CLI shortcut
alias o="orbit"
# Orbit CLI Autocompletion
if command -v orbit >/dev/null 2>&1; then
  source <(orbit completion bash)
fi
# Trailing user line
`
	_ = os.WriteFile(bashrc, []byte(initialContent), 0644)

	RemoveShellConfiguration()

	cleaned, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatalf("failed to read cleaned bashrc: %v", err)
	}

	cleanedStr := string(cleaned)
	if strings.Contains(cleanedStr, "alias o=") {
		t.Errorf("expected alias o to be removed, got: %s", cleanedStr)
	}
	if strings.Contains(cleanedStr, "source <(orbit completion") {
		t.Errorf("expected completion to be removed, got: %s", cleanedStr)
	}
	if !strings.Contains(cleanedStr, "# Existing user line") || !strings.Contains(cleanedStr, "# Trailing user line") {
		t.Errorf("expected user lines to be preserved, got: %s", cleanedStr)
	}
}
