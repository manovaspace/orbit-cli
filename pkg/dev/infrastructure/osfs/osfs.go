// Package osfs implements the dev filesystem port with the os package.
package osfs

import (
	"os"
	"path/filepath"
)

// FS locates the orbit-infra checkout on the real filesystem.
type FS struct{}

// New builds an FS.
func New() *FS { return &FS{} }

// InfraDir finds orbit-infra from ORBIT_INFRA_DIR or the workspace layout.
func (f *FS) InfraDir(workspaceRoot string) string {
	if envDir := os.Getenv("ORBIT_INFRA_DIR"); envDir != "" {
		if fi, err := os.Stat(envDir); err == nil && fi.IsDir() {
			return envDir
		}
	}

	candidates := []string{
		filepath.Join(workspaceRoot, "orbit", "orbit-infra"),
		filepath.Join(workspaceRoot, "orbit-infra"),
		workspaceRoot,
	}

	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "compose.sh")); err == nil {
			return c
		}
		if _, err := os.Stat(filepath.Join(c, "core", "docker-compose.yml")); err == nil {
			return c
		}
	}

	return filepath.Join(workspaceRoot, "orbit", "orbit-infra")
}

// Exists reports whether path exists.
func (f *FS) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir reports whether path exists and is a directory.
func (f *FS) IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// ReadFile returns the bytes of the file at path.
func (f *FS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile creates the parent directory and writes data to path with mode 0644.
func (f *FS) WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
