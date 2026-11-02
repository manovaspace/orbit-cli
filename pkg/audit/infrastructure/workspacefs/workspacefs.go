// Package workspacefs implements the audit file port with the os package.
package workspacefs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Files reads workspace manifests and source files.
type Files struct{}

// New builds the adapter.
func New() *Files { return &Files{} }

// ReadFile reads path.
func (f *Files) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// Exists reports whether path exists.
func (f *Files) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// WalkFiles visits source files under root, skipping vendored trees, build
// output, worktrees and the temporary -wt- copies.
func (f *Files) WalkFiles(root string, fn func(path string) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if skipDir(entry.Name()) || strings.Contains(filepath.ToSlash(path), "-wt-") {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
			return fn(path)
		}
		return nil
	})
}

// InstalledExports finds the installed package's declaration file. module may
// carry a subpath export such as "@manovaspace/ui/motion". Workspace members
// keep their own node_modules, so the search descends into packages but never
// into a node_modules tree.
func (f *Files) InstalledExports(repoDir, module string) (string, bool) {
	name, sub := splitModule(module)
	candidates := func(nodeModules string) []string {
		dir := filepath.Join(nodeModules, filepath.FromSlash(name))
		if sub == "" {
			return []string{filepath.Join(dir, "dist", "index.d.ts"), filepath.Join(dir, "index.d.ts")}
		}
		subPath := filepath.FromSlash(sub)
		return []string{
			filepath.Join(dir, "dist", subPath, "index.d.ts"),
			filepath.Join(dir, subPath, "index.d.ts"),
			filepath.Join(dir, "dist", subPath+".d.ts"),
		}
	}
	for _, candidate := range candidates(filepath.Join(repoDir, "node_modules")) {
		if exists(candidate) {
			return candidate, true
		}
	}
	found := ""
	_ = filepath.WalkDir(repoDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() || path == repoDir {
			return nil
		}
		if entry.Name() == "node_modules" {
			for _, candidate := range candidates(path) {
				if exists(candidate) {
					found = candidate
					return filepath.SkipAll
				}
			}
			return filepath.SkipDir
		}
		if skipDir(entry.Name()) || strings.Contains(filepath.ToSlash(path), "-wt-") {
			return filepath.SkipDir
		}
		return nil
	})
	return found, found != ""
}

// splitModule separates "@scope/name/sub/path" into its package and subpath.
func splitModule(module string) (name, sub string) {
	parts := strings.Split(module, "/")
	base := 1
	if strings.HasPrefix(module, "@") && len(parts) >= 2 {
		base = 2
	}
	if len(parts) <= base {
		return module, ""
	}
	return strings.Join(parts[:base], "/"), strings.Join(parts[base:], "/")
}

func skipDir(name string) bool {
	switch name {
	case "node_modules", ".next", ".worktrees", ".git", "dist", "build", "coverage", "testdata":
		return true
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
