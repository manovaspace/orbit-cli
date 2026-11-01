// Package osfs implements the environment filesystem and schema discovery
// ports with the os package.
package osfs

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/manovaspace/orbit-cli/pkg/env/application"
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

// Chmod sets the permission bits of path.
func (f *FS) Chmod(path string, perm uint32) error { return os.Chmod(path, os.FileMode(perm)) }

// MkdirAll creates path and its parents.
func (f *FS) MkdirAll(path string) error { return os.MkdirAll(path, 0o755) }

// Stat reports a missing path as exists=false with a nil error.
func (f *FS) Stat(path string) (application.FileInfo, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return application.FileInfo{}, false, nil
		}
		return application.FileInfo{}, false, err
	}
	return application.FileInfo{IsDir: info.IsDir(), Perm: uint32(info.Mode().Perm())}, true, nil
}

// Find walks root for .env.schema.yaml contracts, skipping dependency and
// build directories, and returns them sorted.
func (f *FS) Find(root string) ([]string, error) {
	var schemas []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "temp", ".trash", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == ".env.schema.yaml" || d.Name() == ".env.schema.yml" {
			schemas = append(schemas, path)
		}
		return nil
	})
	sort.Strings(schemas)
	return schemas, err
}
