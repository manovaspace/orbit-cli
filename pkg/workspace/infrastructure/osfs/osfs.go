// Package osfs implements the workspace and uninstall filesystem ports with
// the os package.
package osfs

import (
	"io"
	"os"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/workspace/application"
)

// FS is a filesystem rooted in the real machine.
type FS struct{}

// New builds an FS.
func New() *FS { return &FS{} }

// Stat reports a missing path as exists=false with a nil error.
func (f *FS) Stat(path string) (application.FileInfo, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return application.FileInfo{}, false, nil
		}
		return application.FileInfo{}, false, err
	}
	return application.FileInfo{IsDir: info.IsDir()}, true, nil
}

// ReadDir lists the direct entries of path.
func (f *FS) ReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names, nil
}

// MkdirAll creates path and its parents.
func (f *FS) MkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

// TempDir creates a temporary directory.
func (f *FS) TempDir(pattern string) (string, error) {
	return os.MkdirTemp("", pattern)
}

// RemoveAll deletes path recursively.
func (f *FS) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// CopyTree copies src recursively into dst, preserving symlinks and modes.
func (f *FS) CopyTree(src, dst string) error {
	return copyTree(src, dst)
}

func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
