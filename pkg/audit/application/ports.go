// Package application implements the audit use cases over workspace, file,
// version-control, registry, advisory and GitHub ports. It performs no
// process execution and reaches the network only through those ports.
package application

import (
	"context"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
)

// Repo is one workspace repository resolved from workspace.yaml.
type Repo struct {
	Name          string
	Path          string // slash-separated path relative to the workspace root
	Dir           string // absolute directory
	DefaultBranch string
	RemoteURL     string // resolved remote, used to derive the GitHub slug
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
	// WalkAll visits every non-vendored file, including test files.
	WalkAll(root string, fn func(path string) error) error
	// InstalledExports returns the declaration file of pkg as installed for
	// fromFile, preferring the importing package's own node_modules.
	InstalledExports(repoDir, fromFile, pkg string) (string, bool)
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

// Advisories queries the OSV database for resolved package versions.
type Advisories interface {
	Query(ctx context.Context, packages []domain.PackageRef) ([]domain.AdvisoryHit, error)
}

// GitHub reads releases, merged pull requests and Actions billing through gh.
type GitHub interface {
	Releases(ctx context.Context, owner, repo string) (string, error)
	MergedPullRequests(ctx context.Context, slug string, since time.Time) ([]domain.MergedPullRequest, error)
	ActionsUsage(ctx context.Context, owner string) (domain.ActionsUsage, error)
}

// CommandResult is the outcome of one external agent command.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Commands runs the handbook agent scripts and resolves editor binaries.
type Commands interface {
	Run(ctx context.Context, dir, name string, args ...string) (CommandResult, error)
	// RunIsolated runs a root-relative script from a disposable copy of its
	// directory, so any relative write the script makes stays outside the
	// checkout. The copy is removed before RunIsolated returns.
	RunIsolated(ctx context.Context, root, name, script string, args ...string) (CommandResult, error)
	LookPath(name string) (string, error)
}
