package migrate

import (
	"fmt"
	"log/slog"
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
func SetupWorkspace(workspaceRoot string, repoPaths ...string) error {
	if err := EnsureWorkspaceDirs(workspaceRoot); err != nil {
		return err
	}
	if err := InstallGitHooks(workspaceRoot, repoPaths...); err != nil {
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
		if err := ensureSafeDirectory(target); err != nil {
			return fmt.Errorf("failed to create workspace directory %s: %w", target, err)
		}
	}
	return nil
}

// InstallGitHooks installs shared hooks only in explicitly selected repository roots.
// With no selection it considers the workspace root itself, never its ancestor.
func InstallGitHooks(workspaceRoot string, repoPaths ...string) error {
	githooksPath := filepath.Join(workspaceRoot, ".githooks")
	fi, err := os.Stat(githooksPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("hooks path is not a directory: %s", githooksPath)
	}
	if len(repoPaths) == 0 {
		repoPaths = []string{"."}
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return err
	}
	for _, repoPath := range repoPaths {
		repoRoot := repoPath
		if !filepath.IsAbs(repoRoot) {
			repoRoot = filepath.Join(absRoot, repoRoot)
		}
		if err := checkDirectoryParents(repoRoot); err != nil {
			return err
		}
		rel, err := filepath.Rel(absRoot, repoRoot)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("selected repository escapes workspace: %s", repoPath)
		}
		if _, err := os.Lstat(filepath.Join(repoRoot, ".git")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		top, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return fmt.Errorf("selected path is not a worktree: %s", repoRoot)
		}
		realRoot, err := filepath.EvalSymlinks(repoRoot)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(top)) != realRoot {
			return fmt.Errorf("selected path is not a repository root: %s", repoRoot)
		}
		hooks := filepath.Join(absRoot, ".githooks")
		if rel == "." {
			hooks = ".githooks"
		}
		old, err := exec.Command("git", "-C", repoRoot, "config", "--local", "--get", "core.hooksPath").Output()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				return fmt.Errorf("read hooks config: %w", err)
			}
		}
		if value := strings.TrimSpace(string(old)); value != "" && value != hooks {
			return fmt.Errorf("refusing conflicting core.hooksPath in %s", repoRoot)
		}
		global, _ := exec.Command("git", "config", "--global", "--show-origin", "--get", "core.hooksPath").Output()
		if len(global) > 0 {
			slog.Warn("Global hooks override present; installing selected repository local override", "repository", repoRoot)
		}
		if out, err := exec.Command("git", "-C", repoRoot, "config", "--local", "core.hooksPath", hooks).CombinedOutput(); err != nil {
			return fmt.Errorf("configure repository hooks: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

// SetupMCPEnvironment ensures .cursor/mcp.env exists, copying from mcp.env.example if available.
func SetupMCPEnvironment(workspaceRoot string) error {
	cursorDir := filepath.Join(workspaceRoot, ".cursor")
	targetEnv := filepath.Join(cursorDir, "mcp.env")

	if err := checkDirectoryParents(cursorDir); err != nil {
		return err
	}
	// Existing regular environments are preserved; aliases and other entries are conflicts.
	if info, err := os.Lstat(targetEnv); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular environment path %s", targetEnv)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
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

	if err := ensureSafeDirectory(cursorDir); err != nil {
		return fmt.Errorf("failed to create .cursor directory: %w", err)
	}

	file, err := os.OpenFile(targetEnv, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("failed to create %s without replacement: %w", targetEnv, err)
	}
	_, writeErr := file.Write(templateData)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("failed to write %s: %w", targetEnv, writeErr)
	}
	if closeErr != nil {
		return closeErr
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
	for _, directory := range []string{cursorDir, filepath.Join(cursorDir, "rules"), filepath.Join(cursorDir, "skills")} {
		if err := checkDirectoryParents(directory); err != nil {
			return err
		}
	}
	if err := ensureSafeDirectory(cursorDir); err != nil {
		return fmt.Errorf("failed to create .cursor directory: %w", err)
	}

	// Symlink rules from handbook/cursor/rules/ into .cursor/rules/
	handbookRules := filepath.Join(handbookCursor, "rules")
	if rfi, err := os.Stat(handbookRules); err == nil && rfi.IsDir() {
		targetRulesDir := filepath.Join(cursorDir, "rules")
		if err := ensureSafeDirectory(targetRulesDir); err != nil {
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
		if err := ensureSafeDirectory(targetSkillsDir); err != nil {
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

// createSymlink refuses replacement conflicts and preserves existing originals.
func createSymlink(src, dest string) error {
	destDir := filepath.Dir(dest)
	if err := ensureSafeDirectory(destDir); err != nil {
		return fmt.Errorf("create directory %s: %w", destDir, err)
	}
	if target, err := os.Readlink(dest); err == nil && target == src {
		return nil
	}
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("refusing to replace existing path %s", dest)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(src, dest); err != nil {
		return fmt.Errorf("symlink %s to %s: %w", src, dest, err)
	}
	return nil
}

// checkDirectoryParents rejects aliases before bootstrap writes. Missing descendants
// are allowed, but every existing directory up to the filesystem root must be real.
func checkDirectoryParents(directory string) error {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	for path := absolute; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing aliased or non-directory destination %s", path)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	return nil
}

func ensureSafeDirectory(directory string) error {
	if err := checkDirectoryParents(directory); err != nil {
		return err
	}
	return os.MkdirAll(directory, 0755)
}
