package migrate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SetupWorkspace runs all standard idempotent workspace bootstrap tasks:
// 1. Ensure standard workspace directory hierarchy exists.
// 2. Configure git core.hooksPath if .githooks exists.
// 3. Setup .cursor/mcp.env from templates if missing.
// 4. Symlink Cursor rules and skills from handbook/cursor into .cursor/.
func SetupWorkspace(workspaceRoot string) error {
	if err := EnsureWorkspaceDirs(workspaceRoot); err != nil {
		return err
	}
	if err := InstallGitHooks(workspaceRoot); err != nil {
		return err
	}
	if err := SetupMCPEnvironment(workspaceRoot); err != nil {
		return err
	}
	if err := SymlinkCursorRules(workspaceRoot); err != nil {
		return err
	}
	return nil
}

// GetBuiltinMigrations returns the canonical ordered list of built-in workspace migrations.
func GetBuiltinMigrations() []Migration {
	return []Migration{}
}

// EnsureWorkspaceDirs ensures that standard top-level workspace directories exist.
func EnsureWorkspaceDirs(workspaceRoot string) error {
	dirs := []string{"orbit", "manovaspace", "clients", "documents", "share", "temp"}
	for _, d := range dirs {
		target := filepath.Join(workspaceRoot, d)
		if err := os.MkdirAll(target, 0755); err != nil {
			return fmt.Errorf("failed to create workspace directory %s: %w", target, err)
		}
	}
	return nil
}

// InstallGitHooks configures git to use .githooks as its hooks directory if .githooks exists.
func InstallGitHooks(workspaceRoot string) error {
	githooksPath := filepath.Join(workspaceRoot, ".githooks")
	fi, err := os.Stat(githooksPath)
	if err != nil || !fi.IsDir() {
		// .githooks does not exist, nothing to configure
		return nil
	}

	cmd := exec.Command("git", "config", "core.hooksPath", ".githooks")
	cmd.Dir = workspaceRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		outStr := string(out)
		// If workspaceRoot is not a git repo, skip gracefully without failure
		if strings.Contains(outStr, "not in a git directory") || strings.Contains(outStr, "fatal: not a git repository") {
			return nil
		}
		return fmt.Errorf("failed to configure git core.hooksPath: %s: %w", strings.TrimSpace(outStr), err)
	}

	return nil
}

// SetupMCPEnvironment ensures .cursor/mcp.env exists, copying from mcp.env.example if available.
func SetupMCPEnvironment(workspaceRoot string) error {
	cursorDir := filepath.Join(workspaceRoot, ".cursor")
	targetEnv := filepath.Join(cursorDir, "mcp.env")

	// If .cursor/mcp.env already exists, do nothing
	if _, err := os.Stat(targetEnv); err == nil {
		return nil
	}

	// Candidate template paths in order of preference
	candidates := []string{
		filepath.Join(workspaceRoot, ".cursor", "mcp.env.example"),
		filepath.Join(workspaceRoot, "handbook", "cursor", "mcp.env.example"),
		filepath.Join(workspaceRoot, "mcp.env.example"),
	}

	var templateData []byte
	for _, c := range candidates {
		if data, err := os.ReadFile(c); err == nil {
			templateData = data
			break
		}
	}

	if templateData == nil {
		templateData = []byte("# Cursor MCP Environment Configuration\n# Set credentials for local MCP servers\n")
	}

	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		return fmt.Errorf("failed to create .cursor directory: %w", err)
	}

	if err := os.WriteFile(targetEnv, templateData, 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", targetEnv, err)
	}

	return nil
}

// SymlinkCursorRules symlinks Cursor rules and skills from handbook/cursor/ into .cursor/ if handbook is present.
func SymlinkCursorRules(workspaceRoot string) error {
	handbookCursor := filepath.Join(workspaceRoot, "handbook", "cursor")
	fi, err := os.Stat(handbookCursor)
	if err != nil || !fi.IsDir() {
		// handbook/cursor does not exist, nothing to symlink
		return nil
	}

	cursorDir := filepath.Join(workspaceRoot, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		return fmt.Errorf("failed to create .cursor directory: %w", err)
	}

	// Symlink rules from handbook/cursor/rules/ into .cursor/rules/
	handbookRules := filepath.Join(handbookCursor, "rules")
	if rfi, err := os.Stat(handbookRules); err == nil && rfi.IsDir() {
		targetRulesDir := filepath.Join(cursorDir, "rules")
		if err := os.MkdirAll(targetRulesDir, 0755); err != nil {
			return fmt.Errorf("failed to create .cursor/rules directory: %w", err)
		}

		entries, err := os.ReadDir(handbookRules)
		if err != nil {
			return fmt.Errorf("failed to read rules directory %s: %w", handbookRules, err)
		}

		for _, entry := range entries {
			src := filepath.Join(handbookRules, entry.Name())
			dest := filepath.Join(targetRulesDir, entry.Name())
			if err := createSymlink(src, dest); err != nil {
				return err
			}
		}
	}

	// Symlink skills from handbook/cursor/skills/ into .cursor/skills/
	handbookSkills := filepath.Join(handbookCursor, "skills")
	if sfi, err := os.Stat(handbookSkills); err == nil && sfi.IsDir() {
		targetSkillsDir := filepath.Join(cursorDir, "skills")
		if err := os.MkdirAll(targetSkillsDir, 0755); err != nil {
			return fmt.Errorf("failed to create .cursor/skills directory: %w", err)
		}

		entries, err := os.ReadDir(handbookSkills)
		if err != nil {
			return fmt.Errorf("failed to read skills directory %s: %w", handbookSkills, err)
		}

		for _, entry := range entries {
			src := filepath.Join(handbookSkills, entry.Name())
			dest := filepath.Join(targetSkillsDir, entry.Name())
			if err := createSymlink(src, dest); err != nil {
				return err
			}
		}
	}

	// Symlink additional workspace cursor artifacts if present
	links := []struct {
		src  string
		dest string
	}{
		{
			src:  filepath.Join(handbookCursor, ".cursorignore"),
			dest: filepath.Join(workspaceRoot, ".cursorignore"),
		},
		{
			src:  filepath.Join(handbookCursor, "AGENTS.workspace.md"),
			dest: filepath.Join(workspaceRoot, "AGENTS.md"),
		},
		{
			src:  filepath.Join(handbookCursor, "README.workspace.md"),
			dest: filepath.Join(workspaceRoot, "README.md"),
		},
		{
			src:  filepath.Join(handbookCursor, "setup-mcp.sh"),
			dest: filepath.Join(cursorDir, "setup-mcp.sh"),
		},
	}

	for _, link := range links {
		if _, err := os.Stat(link.src); err == nil {
			if err := createSymlink(link.src, link.dest); err != nil {
				return err
			}
		}
	}

	return nil
}

// createSymlink creates a symbolic link at dest pointing to src, replacing any existing symlink or file if necessary.
func createSymlink(src, dest string) error {
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	// Check if dest already exists as a symlink
	if target, err := os.Readlink(dest); err == nil {
		if target == src {
			return nil
		}
		_ = os.Remove(dest)
	} else if _, err := os.Lstat(dest); err == nil {
		// Existing file or broken symlink
		_ = os.RemoveAll(dest)
	}

	if err := os.Symlink(src, dest); err != nil {
		return fmt.Errorf("failed to symlink %s to %s: %w", src, dest, err)
	}

	return nil
}
