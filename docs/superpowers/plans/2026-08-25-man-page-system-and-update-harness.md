# Unix Man Page System & Release Update Harness — Implementation Plan

> **For agents:** Use the `executing-plans` skill to implement this plan task-by-task.

**Goal:** Implement automated Unix man page generation for `manova` and `m`, integrate automatic installation into environment bootstrap, wire up release update migrations, and support doc export.

**Architecture:** New package `pkg/manpage` leverages `github.com/spf13/cobra/doc` to generate troff man pages. Man pages are installed to `/usr/local/share/man/man1` or `~/.local/share/man/man1` during `init --bootstrap` and `onboard`. Post-update migration `004_refresh_man_pages` re-generates man pages on every `self-update`. `scripts/build-release.sh` packages man pages for releases.

**Tech Stack:** Go 1.23+, `github.com/spf13/cobra/doc`, Linux `mandb`.

**Spec:** `docs/superpowers/specs/2026-08-25-man-page-system-and-update-harness.md`

---

## Task 1: `pkg/manpage` — Generator, Installer & Removal Engine

**Files:**
- Create: `pkg/manpage/manpage.go`
- Create: `pkg/manpage/manpage_test.go`

### Step 1: Write failing tests in `pkg/manpage/manpage_test.go`

```go
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
	if !strings.Contains(content, ".TH \"MANOVA\" 1") {
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
```

### Step 2: Implement `pkg/manpage/manpage.go`

```go
package manpage

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

const (
	SystemManDir = "/usr/local/share/man/man1"
	UserManDir   = "~/.local/share/man/man1"
)

// Header returns the standard Manova manual page header.
func Header() *doc.GenManHeader {
	return &doc.GenManHeader{
		Title:   "MANOVA",
		Section: "1",
		Manual:  "Manova CLI Developer Reference",
		Source:  "Manova Orbit Toolkit",
	}
}

// GenerateManPages generates all man pages for cmd tree into targetDir.
func GenerateManPages(cmd *cobra.Command, targetDir string) ([]string, error) {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create man directory %q: %w", targetDir, err)
	}

	header := Header()
	if err := doc.GenManTree(cmd, header, targetDir); err != nil {
		return nil, fmt.Errorf("failed to generate man pages: %w", err)
	}

	// Create m.1 symlink pointing to manova.1 (or copy if symlink fails)
	manova1 := filepath.Join(targetDir, "manova.1")
	m1 := filepath.Join(targetDir, "m.1")
	_ = os.Remove(m1)
	if err := os.Symlink("manova.1", m1); err != nil {
		if data, rErr := os.ReadFile(manova1); rErr == nil {
			_ = os.WriteFile(m1, data, 0644)
		}
	}

	var files []string
	entries, _ := os.ReadDir(targetDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".1") {
			files = append(files, filepath.Join(targetDir, e.Name()))
		}
	}

	return files, nil
}

// ResolveManDir resolves the best writable man page directory.
func ResolveManDir() string {
	if os.Geteuid() == 0 {
		return SystemManDir
	}
	// Check if system man dir is writable
	if err := os.MkdirAll(SystemManDir, 0755); err == nil {
		testFile := filepath.Join(SystemManDir, ".test-write")
		if err := os.WriteFile(testFile, []byte(""), 0644); err == nil {
			_ = os.Remove(testFile)
			return SystemManDir
		}
	}

	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".local", "share", "man", "man1")
	}
	return "/tmp/man1"
}

// InstallManPages automatically resolves target directory and installs all man pages.
func InstallManPages(cmd *cobra.Command) error {
	dir := ResolveManDir()
	return InstallToDir(cmd, dir)
}

// InstallToDir generates and writes man pages to targetDir and triggers mandb.
func InstallToDir(cmd *cobra.Command, targetDir string) error {
	if _, err := GenerateManPages(cmd, targetDir); err != nil {
		return err
	}
	// Refresh mandb if available
	if mandb, err := exec.LookPath("mandb"); err == nil {
		_ = exec.Command(mandb, "-q").Run()
	}
	return nil
}

// UninstallFromDir removes all manova*.1 and m.1 files from targetDir.
func UninstallFromDir(targetDir string) error {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		name := e.Name()
		if (strings.HasPrefix(name, "manova") || name == "m.1") && strings.HasSuffix(name, ".1") {
			_ = os.Remove(filepath.Join(targetDir, name))
		}
	}
	return nil
}

// UninstallManPages cleans up man pages from system and user directories.
func UninstallManPages() error {
	_ = UninstallFromDir(SystemManDir)
	if home, err := os.UserHomeDir(); err == nil {
		_ = UninstallFromDir(filepath.Join(home, ".local", "share", "man", "man1"))
	}
	return nil
}
```

### Step 3: Run tests
```bash
cd orbit/orbit-cli && go test ./pkg/manpage/... -v
```

---

## Task 2: Subcommand `manova doc` (`doc man`, `doc markdown`)

**Files:**
- Create: `cmd/manova/doc.go`
- Modify: `cmd/manova/main.go` (register `newDocCmd(cmd)`)

---

## Task 3: Hook Man Page Installation into Bootstrap, Onboard & Uninstall

**Files:**
- Modify: `cmd/manova/init.go` (call `manpage.InstallManPages(cmd.Root())`)
- Modify: `cmd/manova/onboard.go` (call `manpage.InstallManPages(cmd.Root())`)
- Modify: `cmd/manova/uninstall.go` (call `manpage.UninstallManPages()`)

---

## Task 4: Release & Self-Update Migration Hook (`004_refresh_man_pages`)

**Files:**
- Modify: `pkg/migrate/post_update.go`: Add migration `004_refresh_man_pages` which regenerates man pages after binary replacement.

---

## Task 5: Build Harness (`scripts/build-release.sh`)

**Files:**
- Modify: `scripts/build-release.sh` to generate man pages into `dist/man/man1/`.

---

## Task 6: End-to-End Verification

- Test `manova doc man /tmp/man1`
- Test `man manova` and `man m` on local machine
- Build release, publish to GitHub
- Verify `man manova`, `man m`, `man manova-doctor` on target test environment.
