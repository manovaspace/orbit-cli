package application

import (
	"context"
	"io"
	"time"

	prdomain "github.com/manovaspace/orbit-cli/pkg/pr/domain"
)

// Repo is one workspace repository resolved from workspace.yaml.
type Repo struct {
	Name          string `json:"name"`
	Path          string `json:"path"` // slash-separated path relative to the workspace root
	Dir           string `json:"dir"`  // absolute directory
	DefaultBranch string `json:"defaultBranch"`
	Scope         string `json:"scope"`
	Slug          string `json:"slug"` // GitHub owner/name; empty for other hosts
}

// PullRequest is an open pull request as the release commands read it.
type PullRequest struct {
	Number int64  `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Head   string `json:"head"`
	Base   string `json:"base"`
	Author string `json:"author"`
}

// WorkflowRun is one GitHub Actions run.
type WorkflowRun struct {
	Name         string `json:"name"`
	DisplayTitle string `json:"displayTitle"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	HeadBranch   string `json:"headBranch"`
	CreatedAt    string `json:"createdAt"`
	URL          string `json:"url"`
}

// ReleasePR is the open bot release pull request and its checks.
type ReleasePR struct {
	Number     int64            `json:"number"`
	URL        string           `json:"url"`
	Title      string           `json:"title"`
	Head       string           `json:"head"`
	Base       string           `json:"base"`
	Checks     []prdomain.Check `json:"checks"`
	CheckState string           `json:"checkState"`
}

// DemoStatus is the state of the demo branch relative to the default branch.
type DemoStatus struct {
	Exists bool   `json:"exists"`
	SHA    string `json:"sha,omitempty"`
	Behind int    `json:"behind"`
}

// RepoStatus is what `release status` reports for one repository.
type RepoStatus struct {
	Name          string       `json:"name"`
	Path          string       `json:"path"`
	Slug          string       `json:"slug,omitempty"`
	ReleaseSetup  bool         `json:"releaseSetup"`
	Library       bool         `json:"library"`
	DefaultBranch string       `json:"defaultBranch"`
	HeadSHA       string       `json:"headSha"`
	LatestTag     string       `json:"latestTag"`
	ReleasePR     *ReleasePR   `json:"releasePr,omitempty"`
	Demo          DemoStatus   `json:"demo"`
	LastRun       *WorkflowRun `json:"lastRun,omitempty"`
	Unverified    []string     `json:"unverified,omitempty"`
}

// StatusResult is what `release status` produced.
type StatusResult struct {
	Repos []RepoStatus `json:"repos"`
}

// CheckResult is what one `release check` target produced.
type CheckResult struct {
	Repo      string           `json:"repo"`
	Slug      string           `json:"slug,omitempty"`
	Local     bool             `json:"local"`
	ReleasePR *ReleasePR       `json:"releasePr,omitempty"`
	Checks    []prdomain.Check `json:"checks,omitempty"`
	State     string           `json:"state"`
	Database  string           `json:"database,omitempty"`
}

// CheckOutput is what `release check` produced across every selected target.
type CheckOutput struct {
	Results []CheckResult `json:"results"`
}

// DemoResult is what `release demo` produced.
type DemoResult struct {
	Repo          string       `json:"repo"`
	Slug          string       `json:"slug"`
	MainSHA       string       `json:"mainSha"`
	CreatedBranch bool         `json:"createdBranch"`
	UpToDate      bool         `json:"upToDate"`
	PullRequest   *PullRequest `json:"pullRequest,omitempty"`
	Merged        bool         `json:"merged"`
}

// ProdResult is what `release prod` produced.
type ProdResult struct {
	Repo          string       `json:"repo"`
	Slug          string       `json:"slug"`
	Library       bool         `json:"library"`
	ReleasePR     *PullRequest `json:"releasePr,omitempty"`
	Merged        bool         `json:"merged"`
	Dispatched    string       `json:"dispatched,omitempty"`
	OwnerApproval string       `json:"ownerApproval,omitempty"`
}

// Workspace resolves the repositories declared by workspace.yaml.
type Workspace interface {
	Repos(root, manifestPath string) ([]Repo, error)
}

// Files reads repository files. Exists reports false for any stat failure.
type Files interface {
	ReadFile(path string) ([]byte, error)
	Exists(path string) bool
	ReadDir(path string) ([]string, error)
}

// Git reads local refs for release status. It never fetches.
type Git interface {
	RevParse(ctx context.Context, dir, ref string) (string, error)
	// LatestTag returns the highest version tag, or an empty string when the
	// repository has none.
	LatestTag(ctx context.Context, dir string) (string, error)
}

// Forge is the GitHub surface the release commands use.
type Forge interface {
	ListOpen(ctx context.Context, slug string) ([]PullRequest, error)
	Checks(ctx context.Context, slug, ref string) ([]prdomain.Check, error)
	// CommitChecks reads the check runs of one commit. `gh pr checks` only reads
	// a pull request; a demo gate reads the main commit it promotes.
	CommitChecks(ctx context.Context, slug, sha string) ([]prdomain.Check, error)
	MergePR(ctx context.Context, slug, ref string, method prdomain.Method) error
	Comment(ctx context.Context, slug, ref, body string) error
	// BranchSHA returns the branch head. A missing branch is exists=false with
	// a nil error.
	BranchSHA(ctx context.Context, slug, branch string) (sha string, exists bool, err error)
	CreateBranch(ctx context.Context, slug, branch, sha string) error
	CreatePR(ctx context.Context, slug, title, body, head, base string) (PullRequest, error)
	Runs(ctx context.Context, slug string) ([]WorkflowRun, error)
	Dispatch(ctx context.Context, slug, workflow, ref string) error
	// CompareAhead returns how many commits head has that base lacks.
	CompareAhead(ctx context.Context, slug, base, head string) (int, error)
}

// Local prepares the local release tier against the existing dev services. It
// creates and drops a throwaway database and never starts a container.
type Local interface {
	// Reachable returns a clierr.Unverified error when the dev database for
	// repoDir cannot be reached.
	Reachable(ctx context.Context, repoDir string) error
	CreateDatabase(ctx context.Context, repoDir, name string) error
	DropDatabase(ctx context.Context, repoDir, name string) error
	// Env returns the release-tier environment for the throwaway database
	// (TEST_DATABASE_URL, TEST_REDIS_URL, TEST_NATS_URL and BUS_*). The URL is
	// never part of an error.
	Env(repoDir, database string) (map[string]string, error)
}

// Tester runs the repository's own test command with the release-tier
// environment.
type Tester interface {
	Run(ctx context.Context, dir string, env map[string]string) error
}

// UI carries the decoration cmd/orbit already renders.
type UI struct {
	Arrow string
	OK    string
	Warn  string
	Title func(string) string
	Bold  func(string) string
}

// Service is the release use-case layer.
type Service struct {
	Workspace Workspace
	Files     Files
	Git       Git
	Forge     Forge
	Local     Local
	Tester    Tester

	Out io.Writer

	// Root and ManifestPath are the workspace coordinates status resolves.
	Root         string
	ManifestPath string

	Sleep func(time.Duration)
	Now   func() time.Time
	// PollInterval and RegisterWindow default to 10s and 60s.
	PollInterval, RegisterWindow time.Duration
	// WaitTimeout defaults to 20m for `release check`.
	WaitTimeout time.Duration
	// NewDatabaseName creates the throwaway database name. Tests replace it.
	NewDatabaseName func() string

	UI UI
}
