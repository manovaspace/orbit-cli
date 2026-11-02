// Package application implements the audit use cases over workspace, file,
// version-control and registry ports. It performs no process execution and
// reaches the network only through the registry port.
package application

import "context"

// Repo is one workspace repository resolved from workspace.yaml.
type Repo struct {
	Name          string
	Path          string // slash-separated path relative to the workspace root
	Dir           string // absolute directory
	DefaultBranch string
}

// Workspace resolves the repositories declared by workspace.yaml.
type Workspace interface {
	Repos(root, manifestPath string) ([]Repo, error)
}

// Files reads manifests and source files. WalkFiles visits source files only,
// skipping vendored trees, build output and worktrees.
type Files interface {
	ReadFile(path string) ([]byte, error)
	Exists(path string) bool
	WalkFiles(root string, fn func(path string) error) error
	// InstalledExports returns the installed package's dist/index.d.ts.
	InstalledExports(repoDir, pkg string) (string, bool)
}

// VCS reads local git refs without running git and without network access.
type VCS interface {
	// Head returns the local HEAD commit and the fetched origin default-branch
	// commit. An empty origin means the repository has no fetch to compare.
	Head(dir, branch string) (head, origin string, err error)
}

// Registry reads the latest published version of a package.
type Registry interface {
	Latest(ctx context.Context, pkg string) (string, error)
}
