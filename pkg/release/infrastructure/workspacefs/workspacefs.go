// Package workspacefs implements the release files port with the os package.
package workspacefs

import (
	"os"
)

// FS reads repository files from the real filesystem.
type FS struct{}

// New builds an FS.
func New() *FS { return &FS{} }

// ReadFile reads path.
func (f *FS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// Exists reports whether path exists. Any stat failure is reported as absent.
func (f *FS) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadDir lists a directory's entries.
func (f *FS) ReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names, nil
}
