package audit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/audit/application"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// runnerCommands adapts runner.Runner to the audit command port and resolves
// editor binaries from PATH.
type runnerCommands struct {
	runner runner.Runner
}

func (c runnerCommands) Run(ctx context.Context, dir, name string, args ...string) (application.CommandResult, error) {
	res, err := c.runner.Run(ctx, dir, name, args...)
	return application.CommandResult{Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}, err
}

func (runnerCommands) LookPath(name string) (string, error) { return exec.LookPath(name) }

// RunIsolated runs a root-relative script from a disposable copy of its package
// directory, so a script that writes next to itself (eval.mjs writes
// eval-results.json) never dirties the checkout. A failure to build the copy is
// reported as a failed command so the audit names it instead of silently
// running the script in place.
func (c runnerCommands) RunIsolated(ctx context.Context, root, name, script string, args ...string) (application.CommandResult, error) {
	source := filepath.Join(root, filepath.FromSlash(script))
	dir, err := isolatePackage(filepath.Dir(source))
	if err != nil {
		return application.CommandResult{
			Stderr:   "cannot isolate " + script + ": " + err.Error(),
			ExitCode: 1,
		}, nil
	}
	defer func() { _ = os.RemoveAll(dir) }()
	return c.Run(ctx, dir, name, append([]string{filepath.Base(source)}, args...)...)
}

// isolatePackage builds the disposable copy: regular files are copied, so a
// relative write lands in the copy, and directories are symlinked, so imports
// and the handbook-mcp index resolve without duplicating node_modules.
func isolatePackage(sourceDir string) (string, error) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "orbit-audit-eval-")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		source := filepath.Join(sourceDir, entry.Name())
		target := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			err = os.Symlink(source, target)
		} else {
			err = copyFile(source, target)
		}
		if err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

// copyFile copies one regular file and keeps its permission bits.
func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, info.Mode().Perm())
}
