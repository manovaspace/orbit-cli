package manpage_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/manpage"
	"github.com/spf13/cobra"
)

func createTestRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "manova",
		Short: "Developer onboarding and workspace orchestrator",
		Long:  "Fast, zero-leak developer onboarding and dev stack orchestrator.",
	}
	subCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run pre-flight diagnostics",
	}
	rootCmd.AddCommand(subCmd)
	return rootCmd
}

func TestGenerateManPages(t *testing.T) {
	dir := t.TempDir()
	cmd := createTestRootCmd()

	files, err := manpage.GenerateManPages(cmd, dir)
	if err != nil {
		t.Fatalf("GenerateManPages failed: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("expected generated man page files, got 0")
	}

	// Verify manova.1 exists and contains troff header
	manova1 := filepath.Join(dir, "manova.1")
	data, err := os.ReadFile(manova1)
	if err != nil {
		t.Fatalf("failed to read manova.1: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, ".TH \"MANOVA\"") {
		t.Errorf("expected .TH header in manova.1, got: %s", content)
	}

	// Verify alias m.1 exists
	m1 := filepath.Join(dir, "m.1")
	if _, err := os.Stat(m1); err != nil {
		t.Errorf("expected m.1 to exist: %v", err)
	}
}

func TestInstallAndUninstallManPages(t *testing.T) {
	dir := t.TempDir()
	cmd := createTestRootCmd()

	if err := manpage.InstallToDir(cmd, dir); err != nil {
		t.Fatalf("InstallToDir failed: %v", err)
	}

	// Verify manova.1 and m.1 exist
	if _, err := os.Stat(filepath.Join(dir, "manova.1")); err != nil {
		t.Errorf("manova.1 missing after install")
	}

	if err := manpage.UninstallFromDir(dir); err != nil {
		t.Fatalf("UninstallFromDir failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "manova.1")); !os.IsNotExist(err) {
		t.Errorf("manova.1 should be removed after uninstall")
	}
}
