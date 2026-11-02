// Package workspacefs implements the audit file port with the os package.
package workspacefs

import (
	"encoding/json"
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

// WalkAll visits every file under root, skipping vendored trees, build output,
// generated sites, worktrees and the temporary -wt- copies. Unlike WalkFiles
// it also visits tests and non-source files.
func (f *Files) WalkAll(root string, fn func(path string) error) error {
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
			if skipAllDir(entry.Name()) || strings.Contains(filepath.ToSlash(path), "-wt-") {
				return filepath.SkipDir
			}
			return nil
		}
		return fn(path)
	})
}

func skipAllDir(name string) bool {
	switch name {
	case "node_modules", ".git", ".next", ".worktrees", ".docusaurus", ".cache",
		"dist", "build", "coverage", "vendor", "target", "out", "bin", "testdata",
		"__pycache__", ".venv", "venv":
		return true
	}
	return false
}

// InstalledExports finds the installed package's declaration file. module may
// carry a subpath export such as "@manovaspace/ui/motion". Workspace members
// declared by package.json resolve before node_modules; the importing
// package's own node_modules is read before the repository root's, because
// Bun installs per package in monorepos. The declaration file follows the
// package's exports map when it has one.
func (f *Files) InstalledExports(repoDir, fromFile, module string) (string, bool) {
	name, sub := splitModule(module)
	if name == "" {
		return "", false
	}

	if member := workspaceMemberDir(repoDir, name); member != "" {
		if file, ok := packageExportFile(member, sub); ok {
			return file, true
		}
	}

	var nodeModules []string
	if fromFile != "" {
		if root := nearestPackageRoot(fromFile, repoDir); root != "" {
			nodeModules = append(nodeModules, filepath.Join(root, "node_modules"))
		}
	}
	nodeModules = append(nodeModules, filepath.Join(repoDir, "node_modules"))
	for _, root := range uniquePaths(nodeModules) {
		if file, ok := packageExportFile(filepath.Join(root, filepath.FromSlash(name)), sub); ok {
			return file, true
		}
	}

	found := ""
	_ = filepath.WalkDir(repoDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() || path == repoDir {
			return nil
		}
		if entry.Name() == "node_modules" {
			if file, ok := packageExportFile(filepath.Join(path, filepath.FromSlash(name)), sub); ok {
				found = file
				return filepath.SkipAll
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

// packageExportFile resolves the declaration file of one installed package
// directory for the root entry or a subpath export.
func packageExportFile(pkgDir, sub string) (string, bool) {
	if data, err := os.ReadFile(filepath.Join(pkgDir, "package.json")); err == nil {
		var pkg struct {
			Types   string          `json:"types"`
			Typings string          `json:"typings"`
			Exports json.RawMessage `json:"exports"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			if target := exportTarget(pkg.Exports, sub); target != "" {
				if candidate, ok := resolveExportTarget(pkgDir, target); ok {
					return candidate, true
				}
			}
			if sub == "" {
				for _, target := range []string{pkg.Types, pkg.Typings} {
					if target == "" {
						continue
					}
					if candidate, ok := resolveExportTarget(pkgDir, target); ok {
						return candidate, true
					}
				}
			}
		}
	}
	for _, candidate := range legacyCandidates(pkgDir, sub) {
		if exists(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// workspaceMemberDir finds a workspace member by package name through the
// repository package.json workspaces list or object.
func workspaceMemberDir(repoDir, name string) string {
	data, err := os.ReadFile(filepath.Join(repoDir, "package.json"))
	if err != nil {
		return ""
	}
	var root struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if json.Unmarshal(data, &root) != nil {
		return ""
	}
	for _, pattern := range workspacePatterns(root.Workspaces) {
		matches, err := filepath.Glob(filepath.Join(repoDir, filepath.FromSlash(pattern)))
		if err != nil {
			continue
		}
		for _, dir := range matches {
			memberData, err := os.ReadFile(filepath.Join(dir, "package.json"))
			if err != nil {
				continue
			}
			var member struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(memberData, &member) == nil && member.Name == name {
				return dir
			}
		}
	}
	return ""
}

// workspacePatterns reads the workspaces array or object form.
func workspacePatterns(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var object struct {
		Packages []string `json:"packages"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return object.Packages
	}
	return nil
}

// nearestPackageRoot walks up from fromFile to repoDir looking for the package
// that owns the importing file.
func nearestPackageRoot(fromFile, repoDir string) string {
	dir := filepath.Dir(fromFile)
	stop := filepath.Clean(repoDir)
	for {
		if exists(filepath.Join(dir, "package.json")) {
			return dir
		}
		if filepath.Clean(dir) == stop {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// exportTarget selects the exports entry for the root or ./sub path and reads
// its types declaration.
func exportTarget(raw json.RawMessage, sub string) string {
	if len(raw) == 0 {
		return ""
	}
	key := "."
	if sub != "" {
		key = "./" + sub
	}
	var mapping map[string]json.RawMessage
	if json.Unmarshal(raw, &mapping) == nil {
		if value, ok := mapping[key]; ok {
			return conditionTarget(value)
		}
		if sub == "" {
			for _, condition := range []string{"types", "typings", "import", "default", "require"} {
				if _, ok := mapping[condition]; ok {
					return conditionTarget(raw)
				}
			}
		}
		return ""
	}
	if sub == "" {
		return conditionTarget(raw)
	}
	return ""
}

// conditionTarget reads the types target of a condition tree, falling back to
// the first runtime entry so a sibling declaration can still be resolved.
func conditionTarget(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var mapping map[string]json.RawMessage
	if json.Unmarshal(raw, &mapping) != nil {
		return ""
	}
	for _, key := range []string{"types", "typings", "import", "default", "require", "node"} {
		if next, ok := mapping[key]; ok {
			if target := conditionTarget(next); target != "" {
				return target
			}
		}
	}
	return ""
}

// resolveExportTarget joins a package-relative exports target.
func resolveExportTarget(pkgDir, target string) (string, bool) {
	target = strings.TrimSpace(target)
	if target == "" || strings.Contains(target, "://") {
		return "", false
	}
	candidate := filepath.Join(pkgDir, filepath.FromSlash(strings.TrimPrefix(target, "./")))
	if !exists(candidate) {
		return "", false
	}
	return candidate, true
}

// legacyCandidates keeps working for packages without an exports map.
func legacyCandidates(pkgDir, sub string) []string {
	if sub == "" {
		return []string{filepath.Join(pkgDir, "dist", "index.d.ts"), filepath.Join(pkgDir, "index.d.ts")}
	}
	subPath := filepath.FromSlash(sub)
	return []string{
		filepath.Join(pkgDir, "dist", subPath, "index.d.ts"),
		filepath.Join(pkgDir, subPath, "index.d.ts"),
		filepath.Join(pkgDir, "dist", subPath+".d.ts"),
	}
}

// uniquePaths drops repeated directories while keeping order.
func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
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
