// Package engine runs the orbit setup reconciler: an ordered list of parts, each of which
// plans the changes it needs and applies them. Check plans only; Apply previews with Check,
// asks for confirmation, then re-plans and runs each part in order.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

// FS is the filesystem seam used by parts. OSFS is the production implementation.
type FS interface {
	Stat(path string) (fs.FileInfo, error)
	Lstat(path string) (fs.FileInfo, error)
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Symlink(oldname, newname string) error
	Readlink(path string) (string, error)
	Remove(path string) error
	Rename(oldpath, newpath string) error
}

// OSFS implements FS with package os.
type OSFS struct{}

var _ FS = OSFS{}

func (OSFS) Stat(path string) (fs.FileInfo, error)  { return os.Stat(path) }
func (OSFS) Lstat(path string) (fs.FileInfo, error) { return os.Lstat(path) }
func (OSFS) ReadFile(path string) ([]byte, error)   { return os.ReadFile(path) }
func (OSFS) WriteFile(path string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(path, data, perm)
}
func (OSFS) MkdirAll(path string, perm fs.FileMode) error { return os.MkdirAll(path, perm) }
func (OSFS) Symlink(oldname, newname string) error        { return os.Symlink(oldname, newname) }
func (OSFS) Readlink(path string) (string, error)         { return os.Readlink(path) }
func (OSFS) Remove(path string) error                     { return os.Remove(path) }
func (OSFS) Rename(oldpath, newpath string) error         { return os.Rename(oldpath, newpath) }

// State is everything a part may read or act through. Manifest is nil until the handbook
// is cloned; Reload refreshes it before each part runs during Apply.
type State struct {
	Root     string // absolute workspace root
	Machine  spec.Machine
	Platform string // "darwin" | "linux"
	Manifest *manifest.WorkspaceManifest
	Run      runner.Runner
	Env      runner.EnvRunner
	Tty      runner.Interactive
	FS       FS
	// NewDatabases is set by the services part and read by the env and prepare parts.
	NewDatabases   map[string]bool
	NonInteractive bool
}

// manifestRel is the workspace manifest location, relative to Root.
var manifestRel = filepath.Join("handbook", "agents", "workspace", "workspace.yaml")

// Reload re-reads the workspace manifest from <Root>/handbook/agents/workspace/workspace.yaml.
// A missing file leaves Manifest nil without error; a present but malformed file is a usage error.
func (s *State) Reload() error {
	path := filepath.Join(s.Root, manifestRel)
	if _, err := s.FS.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.Manifest = nil
			return nil
		}
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot read workspace manifest %s", path)))
	}
	m, err := manifest.Load(path)
	if err != nil {
		return clierr.Wrap(err, clierr.Usage(fmt.Sprintf("workspace manifest %s is malformed", path)))
	}
	s.Manifest = m
	return nil
}

// Selected returns the repositories of every project in Machine.Projects, sorted by Path and
// de-duplicated. Without a loaded manifest it reports that the check could not be made.
func (s *State) Selected() ([]manifest.RepoTarget, error) {
	if s.Manifest == nil {
		return nil, clierr.Unverified("workspace manifest not loaded: the handbook is not cloned yet")
	}
	seen := make(map[string]bool)
	var out []manifest.RepoTarget
	for _, name := range s.Machine.Projects {
		repos, err := s.Manifest.ProjectRepos(name)
		if err != nil {
			return nil, err
		}
		for _, repo := range repos {
			if seen[repo.Path] {
				continue
			}
			seen[repo.Path] = true
			out = append(out, repo)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Severity grades a Finding. An "error" finding fails its part; a "warn" finding never does.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
)

// Finding is a problem a part reports about the machine.
type Finding struct {
	Part     string   `json:"part"`
	Subject  string   `json:"subject"`
	Message  string   `json:"message"`
	Fix      string   `json:"fix"`
	Severity Severity `json:"severity"`
}

// Action is one change a part will make. Do runs it during Apply; Check never calls Do.
type Action struct {
	Part    string                      `json:"part"`
	Subject string                      `json:"subject"`
	Summary string                      `json:"summary"`
	Sudo    bool                        `json:"sudo"`
	Browser bool                        `json:"browser"`
	Do      func(context.Context) error `json:"-"`
}

// Plan is what a part would do (Actions) and what it found (Findings).
type Plan struct {
	Actions  []Action
	Findings []Finding
}

// Part is one unit of setup. Needs lists the names of parts that must run before it.
type Part interface {
	Name() string
	Needs() []string
	Plan(ctx context.Context, s *State) (Plan, error)
}

// Part statuses. Check uses ok, pending, failed, skipped and waits; Apply uses ok, changed,
// failed and skipped.
const (
	StatusOK      = "ok"
	StatusPending = "pending"
	StatusChanged = "changed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
	StatusWaits   = "waits"
)

// PartResult is the outcome of one part. Actions holds one line per action: its summary,
// prefixed with "[sudo] " and/or "[browser] " when the action needs them.
type PartResult struct {
	Part     string    `json:"part"`
	Status   string    `json:"status"`
	Actions  []string  `json:"actions"`
	Findings []Finding `json:"findings"`
	Error    string    `json:"error"`
}

// Result is the outcome of a Check or an Apply. Changed counts pending parts in Check and
// changed parts in Apply; Failed counts failed parts.
type Result struct {
	Parts   []PartResult `json:"parts"`
	Changed int          `json:"changed"`
	Failed  int          `json:"failed"`
}

// Engine runs Parts in the order given, so every Need must appear before the part that
// needs it. Confirm, when set, approves the preview before Apply changes anything; nil approves.
type Engine struct {
	Parts   []Part
	Confirm func(preview []PartResult) bool
}
