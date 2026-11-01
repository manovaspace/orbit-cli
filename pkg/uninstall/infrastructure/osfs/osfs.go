// Package osfs implements the uninstall filesystem port with the os package and
// a runner.Runner for privileged commands.
package osfs

import (
	"context"
	"os"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/uninstall/domain"
)

// FS is the real filesystem.
type FS struct {
	Runner runner.Runner
}

// New builds an FS over r.
func New(r runner.Runner) *FS { return &FS{Runner: r} }

// Stat reports a missing path as exists=false with a nil error.
func (f *FS) Stat(path string) (domain.FileInfo, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.FileInfo{}, false, nil
		}
		return domain.FileInfo{}, false, err
	}
	return domain.FileInfo{IsDir: info.IsDir()}, true, nil
}

// RemoveElevated removes path, falling back to sudo when the process lacks
// permission.
func (f *FS) RemoveElevated(ctx context.Context, path string) error {
	if err := os.Remove(path); err == nil {
		return nil
	}
	if os.Geteuid() != 0 {
		_, err := f.Runner.Run(ctx, "", "sudo", "rm", "-f", path)
		return err
	}
	return os.Remove(path)
}

// RemoveAll deletes path recursively.
func (f *FS) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// Glob expands a shell pattern.
func (f *FS) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// RefreshManPages rebuilds the man page index when mandb is available.
func (f *FS) RefreshManPages(ctx context.Context) {
	_, _ = f.Runner.Run(ctx, "", "mandb", "-q")
}

// HomeDir returns the current user's home directory.
func (f *FS) HomeDir() (string, error) {
	return os.UserHomeDir()
}

// Executable returns the running binary path.
func (f *FS) Executable() (string, error) {
	return os.Executable()
}
