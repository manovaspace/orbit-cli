// Package osfs implements the asset filesystem port with the os package.
package osfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/assets/application"
)

// FS is a filesystem rooted in the real machine.
type FS struct{}

// New builds an FS.
func New() *FS { return &FS{} }

// ReadFile reads path.
func (f *FS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// WriteFile writes data to path with perm.
func (f *FS) WriteFile(path string, data []byte, perm uint32) error {
	return os.WriteFile(path, data, os.FileMode(perm))
}

// MkdirAll creates path and its parents.
func (f *FS) MkdirAll(path string) error { return os.MkdirAll(path, 0o755) }

// Rename replaces newPath with oldPath.
func (f *FS) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

// Remove deletes path.
func (f *FS) Remove(path string) error { return os.Remove(path) }

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

// Open opens path for reading.
func (f *FS) Open(path string) (io.ReadCloser, error) { return os.Open(path) }

// HashFile returns the lowercase hex SHA-256 and size of path.
func (f *FS) HashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, file)
	if err != nil {
		return "", 0, fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
