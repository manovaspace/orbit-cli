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
		Use:   "orbit",
		Short: "Developer onboarding and workspace orchestrator",
		Long:  "Fast, zero-leak developer onboarding and dev stack orchestrator.",
	}
	rootCmd.AddGroup(&cobra.Group{ID: "core", Title: "Core Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "workspace", Title: "Workspace Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "system", Title: "System & Tooling:"})

	docCmd := &cobra.Command{
		Use:     "doctor",
		GroupID: "core",
		Short:   "Run pre-flight diagnostics",
	}
	docCmd.Flags().Bool("fix", false, "Auto-heal issues")
	rootCmd.AddCommand(docCmd)

	syncCmd := &cobra.Command{
		Use:     "sync",
		GroupID: "workspace",
		Short:   "Sync repositories",
	}
	rootCmd.AddCommand(syncCmd)

	userCmd := &cobra.Command{
		Use:     "user",
		GroupID: "system",
		Short:   "Manage users",
	}
	rootCmd.AddCommand(userCmd)

	return rootCmd
}

func TestGenerateManPages(t *testing.T) {
	dir := t.TempDir()
	cmd := createTestRootCmd()

	// Seed a legacy fragmented file to test cleanup
	legacyFile := filepath.Join(dir, "orbit-doctor.1")
	_ = os.WriteFile(legacyFile, []byte("legacy"), 0644)

	files, err := manpage.GenerateManPages(cmd, dir)
	if err != nil {
		t.Fatalf("GenerateManPages failed: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected exactly 2 files (orbit.1, o.1), got %d: %v", len(files), files)
	}

	// Verify legacy file was purged
	if _, err := os.Stat(legacyFile); !os.IsNotExist(err) {
		t.Errorf("expected legacy subpage orbit-doctor.1 to be purged, but it still exists")
	}

	// Verify orbit.1 exists and contains troff header and all sections
	orbit1 := filepath.Join(dir, "orbit.1")
	data, err := os.ReadFile(orbit1)
	if err != nil {
		t.Fatalf("failed to read orbit.1: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, ".TH \"ORBIT\"") {
		t.Errorf("expected .TH header in orbit.1, got:\n%s", content)
	}
	if !strings.Contains(content, ".SH CORE COMMANDS") {
		t.Errorf("expected .SH CORE COMMANDS section, got:\n%s", content)
	}
	if !strings.Contains(content, ".SH WORKSPACE COMMANDS") {
		t.Errorf("expected .SH WORKSPACE COMMANDS section, got:\n%s", content)
	}
	if !strings.Contains(content, ".SH ENVIRONMENT VARIABLES") {
		t.Errorf("expected .SH ENVIRONMENT VARIABLES section, got:\n%s", content)
	}
	if !strings.Contains(content, ".SH FILES") {
		t.Errorf("expected .SH FILES section, got:\n%s", content)
	}
	if !strings.Contains(content, "--fix") {
		t.Errorf("expected flag --fix in orbit.1, got:\n%s", content)
	}

	// Verify alias o.1 exists
	o1 := filepath.Join(dir, "o.1")
	if _, err := os.Stat(o1); err != nil {
		t.Errorf("expected o.1 to exist: %v", err)
	}
}

func TestInstallAndUninstallManPages(t *testing.T) {
	dir := t.TempDir()
	cmd := createTestRootCmd()

	if err := manpage.InstallToDir(cmd, dir); err != nil {
		t.Fatalf("InstallToDir failed: %v", err)
	}

	// Verify orbit.1 and o.1 exist
	if _, err := os.Stat(filepath.Join(dir, "orbit.1")); err != nil {
		t.Errorf("orbit.1 missing after install")
	}
	if _, err := os.Stat(filepath.Join(dir, "o.1")); err != nil {
		t.Errorf("o.1 missing after install")
	}

	if err := manpage.UninstallFromDir(dir); err != nil {
		t.Fatalf("UninstallFromDir failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "orbit.1")); !os.IsNotExist(err) {
		t.Errorf("orbit.1 should be removed after uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, "o.1")); !os.IsNotExist(err) {
		t.Errorf("o.1 should be removed after uninstall")
	}
}
