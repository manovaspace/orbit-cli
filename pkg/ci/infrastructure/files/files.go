// Package files implements the CI file port with the os package.
package files

import "os"

// Files reads secret source files and per-repository deploy keys.
type Files struct{}

// New builds a Files.
func New() *Files { return &Files{} }

// ReadFile reads path.
func (f *Files) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// Perm returns the unix permission bits of path.
func (f *Files) Perm(path string) (uint32, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return uint32(info.Mode().Perm()), nil
}
