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
