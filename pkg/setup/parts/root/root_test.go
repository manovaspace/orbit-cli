package root

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const (
	testRoot  = "/ws"
	testState = "/state/orbit/root.json"
	specPath  = "/ws/handbook/agents/workspace/root.yaml"
)

// memNode is one entry of memFS: kind is "dir", "file" or "link".
type memNode struct {
	kind   string
	data   []byte
	target string
}

// memFS is a map-backed engine.FS. Methods the part never calls are left nil on purpose, so a call
// to them panics. WriteFile refuses to write through a symlink: a part that rewrites a link without
// removing it first fails the test.
type memFS struct {
	engine.FS
	nodes     map[string]*memNode
	dirModes  map[string]fs.FileMode
	fileModes map[string]fs.FileMode
}

func newMemFS() *memFS {
	return &memFS{
		nodes:     map[string]*memNode{testRoot: {kind: "dir"}},
		dirModes:  map[string]fs.FileMode{},
		fileModes: map[string]fs.FileMode{},
	}
}

// put stores n at p and creates every missing ancestor as a directory.
func (f *memFS) put(p string, n *memNode) *memFS {
	for dir := filepath.Dir(p); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if _, ok := f.nodes[dir]; !ok {
			f.nodes[dir] = &memNode{kind: "dir"}
		}
	}
	f.nodes[p] = n
	return f
}

func (f *memFS) dir(p string) *memFS { return f.put(p, &memNode{kind: "dir"}) }

func (f *memFS) file(p, content string) *memFS {
	return f.put(p, &memNode{kind: "file", data: []byte(content)})
}

func (f *memFS) link(p, target string) *memFS {
	return f.put(p, &memNode{kind: "link", target: target})
}

func (f *memFS) isDir(p string) bool {
	n, ok := f.nodes[p]
	return ok && n.kind == "dir"
}

type memInfo struct {
	name string
	node *memNode
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return int64(len(i.node.data)) }
func (i memInfo) Mode() fs.FileMode {
	switch i.node.kind {
	case "dir":
		return fs.ModeDir | 0o755
	case "link":
		return fs.ModeSymlink | 0o777
	}
	return 0o644
}
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.node.kind == "dir" }
func (i memInfo) Sys() any           { return nil }

func memResolve(dir, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Join(dir, target)
}

// follow resolves p through symlinks to the node it names.
func (f *memFS) follow(p string) (*memNode, error) {
	for i := 0; i < 8; i++ {
		n, ok := f.nodes[p]
		if !ok {
			return nil, fs.ErrNotExist
		}
		if n.kind != "link" {
			return n, nil
		}
		p = memResolve(filepath.Dir(p), n.target)
	}
	return nil, errors.New("memfs: too many symlinks")
}

func (f *memFS) Lstat(p string) (fs.FileInfo, error) {
	n, ok := f.nodes[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return memInfo{name: filepath.Base(p), node: n}, nil
}

func (f *memFS) Stat(p string) (fs.FileInfo, error) {
	n, err := f.follow(p)
	if err != nil {
		return nil, err
	}
	return memInfo{name: filepath.Base(p), node: n}, nil
}

func (f *memFS) ReadFile(p string) ([]byte, error) {
	n, err := f.follow(p)
	if err != nil {
		return nil, err
	}
	if n.kind == "dir" {
		return nil, errors.New("memfs: is a directory")
	}
	return append([]byte(nil), n.data...), nil
}

func (f *memFS) WriteFile(p string, data []byte, perm fs.FileMode) error {
	if n, ok := f.nodes[p]; ok {
		if n.kind == "link" {
			return errors.New("memfs: write through symlink at " + p)
		}
		if n.kind == "dir" {
			return errors.New("memfs: is a directory")
		}
	}
	if !f.isDir(filepath.Dir(p)) {
		return fs.ErrNotExist
	}
	f.nodes[p] = &memNode{kind: "file", data: append([]byte(nil), data...)}
	f.fileModes[p] = perm
	return nil
}

func (f *memFS) MkdirAll(p string, perm fs.FileMode) error {
	for dir := p; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if _, ok := f.nodes[dir]; !ok {
			f.nodes[dir] = &memNode{kind: "dir"}
		}
	}
	f.dirModes[p] = perm
	return nil
}

func (f *memFS) Symlink(oldname, newname string) error {
	if _, ok := f.nodes[newname]; ok {
		return fs.ErrExist
	}
	if !f.isDir(filepath.Dir(newname)) {
		return fs.ErrNotExist
	}
	f.nodes[newname] = &memNode{kind: "link", target: oldname}
	return nil
}

func (f *memFS) Readlink(p string) (string, error) {
	n, ok := f.nodes[p]
	if !ok {
		return "", fs.ErrNotExist
	}
	if n.kind != "link" {
		return "", errors.New("memfs: not a symlink")
	}
	return n.target, nil
}

func (f *memFS) Remove(p string) error {
	if _, ok := f.nodes[p]; !ok {
		return fs.ErrNotExist
	}
	delete(f.nodes, p)
	return nil
}

func (f *memFS) ReadDir(p string) ([]os.DirEntry, error) {
	n, ok := f.nodes[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if n.kind != "dir" {
		return nil, errors.New("memfs: not a directory")
	}
	var out []os.DirEntry
	for child, cn := range f.nodes {
		if filepath.Dir(child) == p {
			out = append(out, fs.FileInfoToDirEntry(memInfo{name: filepath.Base(child), node: cn}))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// withSpec writes the root declaration yaml at its workspace path.
func (f *memFS) withSpec(yaml string) *memFS {
	return f.file(specPath, yaml)
}

// testManifest has three repositories under clients/. Project app selects alpha and beta.
func testManifest() *manifest.WorkspaceManifest {
	repo := func(name string) manifest.RepoConfig {
		return manifest.RepoConfig{
			Name:          name,
			Path:          "clients/" + name,
			RemoteURL:     "git@github.com:manovaspace/" + name + ".git",
			DefaultBranch: "main",
		}
	}
	return &manifest.WorkspaceManifest{
		Groups: map[string]manifest.GroupConfig{
			"clients": {Repositories: []manifest.RepoConfig{repo("alpha"), repo("beta"), repo("gamma")}},
		},
		Projects: map[string]manifest.ProjectConfig{
			"app": {Repos: []string{"clients/alpha", "clients/beta"}},
		},
	}
}

func newState(fsys *memFS, run *runnertest.Fake, projects ...string) *engine.State {
	return &engine.State{
		Root:     testRoot,
		Machine:  spec.Machine{GitProtocol: "https", Projects: projects},
		Manifest: testManifest(),
		Run:      run,
		FS:       fsys,
	}
}

func planOf(t *testing.T, s *engine.State) engine.Plan {
	t.Helper()
	plan, err := part{state: testState}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return plan
}

func doAll(t *testing.T, plan engine.Plan) {
	t.Helper()
	for _, a := range plan.Actions {
		if err := a.Do(context.Background()); err != nil {
			t.Fatalf("Do %q: %v", a.Summary, err)
		}
	}
}

func summaries(plan engine.Plan) []string {
	var out []string
	for _, a := range plan.Actions {
		out = append(out, a.Summary)
	}
	return out
}

func TestRootCreatesLinksAndFiles(t *testing.T) {
	fsys := newMemFS().
		dir("/ws/handbook/agents/skills/x").
		file("/ws/handbook/agents/AGENTS.workspace.md", "# agents\n").
		file("/ws/stray.txt", "not declared, never touched").
		withSpec(`version: 1
links:
  .agents/skills/x: handbook/agents/skills/x
  AGENTS.md: handbook/agents/AGENTS.workspace.md
files:
  CLAUDE.md: "@AGENTS.md\n"
`)
	s := newState(fsys, &runnertest.Fake{})

	plan := planOf(t, s)
	want := []string{"Link .agents/skills/x", "Link AGENTS.md", "Write CLAUDE.md"}
	if got := summaries(plan); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	if len(plan.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", plan.Findings)
	}
	doAll(t, plan)

	if n := fsys.nodes["/ws/.agents/skills/x"]; n == nil || n.kind != "link" || n.target != "../../handbook/agents/skills/x" {
		t.Fatalf(".agents/skills/x = %+v, want relative link ../../handbook/agents/skills/x", n)
	}
	if n := fsys.nodes["/ws/AGENTS.md"]; n == nil || n.kind != "link" || n.target != "handbook/agents/AGENTS.workspace.md" {
		t.Fatalf("AGENTS.md = %+v, want relative link handbook/agents/AGENTS.workspace.md", n)
	}
	if got := string(fsys.nodes["/ws/CLAUDE.md"].data); got != "@AGENTS.md\n" {
		t.Fatalf("CLAUDE.md = %q, want the declared content", got)
	}
	if mode := fsys.fileModes["/ws/CLAUDE.md"]; mode != 0o644 {
		t.Fatalf("CLAUDE.md mode = %o, want 0644", mode)
	}
	if mode := fsys.dirModes["/ws/.agents/skills"]; mode != 0o755 {
		t.Fatalf("parent dir mode = %o, want 0755", mode)
	}
}

func TestRootConflictOnRegularFile(t *testing.T) {
	fsys := newMemFS().
		dir("/ws/handbook/agents/skills/x").
		file("/ws/handbook/agents/README.workspace.md", "readme\n").
		file("/ws/handbook/FUTURE-TRIGGERS.md", "triggers\n").
		file("/ws/handbook/agents/workspace/package.json", "{}\n").
		file("/ws/README.md", "mine\n").
		file("/ws/FUTURE-TRIGGERS.md", "triggers\n").
		file("/ws/package.json", "{}\n").
		dir("/ws/.claude/skills/x").
		withSpec(`version: 1
links:
  README.md: handbook/agents/README.workspace.md
  FUTURE-TRIGGERS.md: handbook/FUTURE-TRIGGERS.md
  package.json: handbook/agents/workspace/package.json
  .claude/skills/x: handbook/agents/skills/x
  missing.md: handbook/agents/gone.md
`)
	s := newState(fsys, &runnertest.Fake{})

	plan := planOf(t, s)
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %v, want none: identical copies are in place and conflicts are never changed", summaries(plan))
	}
	want := []engine.Finding{
		{Part: "root", Subject: ".claude/skills/x", Message: "conflict: .claude/skills/x is hand-made; move it away and rerun", Severity: engine.SeverityError},
		{Part: "root", Subject: "README.md", Message: "conflict: README.md is hand-made; move it away and rerun", Severity: engine.SeverityError},
		{Part: "root", Subject: "missing.md", Message: "source handbook/agents/gone.md is missing; skipped", Severity: engine.SeverityWarn},
	}
	if !reflect.DeepEqual(plan.Findings, want) {
		t.Fatalf("findings = %+v, want %+v", plan.Findings, want)
	}
	if got := string(fsys.nodes["/ws/README.md"].data); got != "mine\n" {
		t.Fatalf("hand-made README.md changed to %q", got)
	}
}

func TestGoWorkPicksHighestGoAndSkipsNodeModules(t *testing.T) {
	fsys := newMemFS().
		file("/ws/clients/alpha/go.mod", "module a\n\ngo 1.9\n").
		file("/ws/clients/alpha/tools/x/go.mod", "module x\n\ngo 1.27\n").
		file("/ws/clients/alpha/a/b/c/d/go.mod", "module d\n\ngo 1.27\n").
		file("/ws/clients/alpha/a/b/c/d/e/go.mod", "module e\n\ngo 1.60\n").
		file("/ws/clients/alpha/node_modules/pkg/go.mod", "go 1.99\n").
		file("/ws/clients/alpha/vendor/v/go.mod", "go 1.50\n").
		file("/ws/clients/alpha/.hidden/go.mod", "go 1.70\n").
		file("/ws/clients/alpha/testdata/t/go.mod", "go 1.80\n").
		file("/ws/clients/beta/go.mod", "module b\n\ngo 1.27.4\n").
		file("/ws/clients/gamma/go.mod", "go 1.80\n").
		withSpec(`version: 1
generated:
  go.work: go-work
`)
	run := &runnertest.Fake{}
	s := newState(fsys, run, "app")

	plan := planOf(t, s)
	if want := []string{"Write go.work"}; !reflect.DeepEqual(summaries(plan), want) {
		t.Fatalf("actions = %v, want %v", summaries(plan), want)
	}
	doAll(t, plan)

	want := "// generated by orbit setup; do not edit\n" +
		"go 1.27.4\n" +
		"\n" +
		"use (\n" +
		"\t./clients/alpha\n" +
		"\t./clients/alpha/a/b/c/d\n" +
		"\t./clients/alpha/tools/x\n" +
		"\t./clients/beta\n" +
		")\n"
	if got := string(fsys.nodes["/ws/go.work"].data); got != want {
		t.Fatalf("go.work =\n%s\nwant\n%s", got, want)
	}
}

func TestGoWorkFormat(t *testing.T) {
	got := GoWork("1.27.4", []string{"clients/beta", "./clients/alpha"})
	want := "// generated by orbit setup; do not edit\ngo 1.27.4\n\nuse (\n\t./clients/alpha\n\t./clients/beta\n)\n"
	if got != want {
		t.Fatalf("GoWork =\n%q\nwant\n%q", got, want)
	}
}

func TestRootGeneratorsRunOnChangeOnly(t *testing.T) {
	fsys := newMemFS().
		file("/ws/handbook/agents/AGENTS.workspace.md", "# agents\n").
		withSpec(`version: 1
links:
  AGENTS.md: handbook/agents/AGENTS.workspace.md
generators:
  - "python3 handbook/agents/setup-mcp.py"
`)
	run := &runnertest.Fake{}
	run.On("sh -c python3", runner.Result{}, nil)
	s := newState(fsys, run)

	first := planOf(t, s)
	if want := []string{"Link AGENTS.md", "Run python3 handbook/agents/setup-mcp.py"}; !reflect.DeepEqual(summaries(first), want) {
		t.Fatalf("first actions = %v, want %v", summaries(first), want)
	}
	doAll(t, first)

	calls := run.Calls()
	if len(calls) != 1 || calls[0].Name != "sh" || !reflect.DeepEqual(calls[0].Args, []string{"-c", "python3 handbook/agents/setup-mcp.py"}) || calls[0].Dir != testRoot {
		t.Fatalf("generator calls = %+v, want sh -c in %s", calls, testRoot)
	}
	if mode := fsys.dirModes["/state/orbit"]; mode != 0o700 {
		t.Fatalf("state dir mode = %o, want 0700", mode)
	}
	if mode := fsys.fileModes[testState]; mode != 0o600 {
		t.Fatalf("state file mode = %o, want 0600", mode)
	}
	var state struct {
		Generators map[string]string `json:"generators"`
	}
	if err := json.Unmarshal(fsys.nodes[testState].data, &state); err != nil {
		t.Fatalf("state file is not JSON: %v", err)
	}
	stamp, ok := state.Generators["python3 handbook/agents/setup-mcp.py"]
	if !ok {
		t.Fatalf("state = %+v, want the generator recorded", state)
	}
	if _, err := time.Parse(time.RFC3339, stamp); err != nil {
		t.Fatalf("recorded time %q is not RFC3339: %v", stamp, err)
	}

	if again := planOf(t, s); len(again.Actions) != 0 {
		t.Fatalf("unchanged root planned %v, want nothing", summaries(again))
	}

	if err := fsys.Remove("/ws/AGENTS.md"); err != nil {
		t.Fatalf("remove link: %v", err)
	}
	changed := planOf(t, s)
	if want := []string{"Link AGENTS.md", "Run python3 handbook/agents/setup-mcp.py"}; !reflect.DeepEqual(summaries(changed), want) {
		t.Fatalf("changed actions = %v, want %v", summaries(changed), want)
	}
}

func TestRootSecondRunNoActions(t *testing.T) {
	fsys := newMemFS().
		dir("/ws/handbook/agents/skills/x").
		file("/ws/handbook/agents/AGENTS.workspace.md", "# agents\n").
		file("/ws/clients/alpha/go.mod", "module a\n\ngo 1.27\n").
		withSpec(`version: 1
links:
  .agents/skills/x: handbook/agents/skills/x
  AGENTS.md: handbook/agents/AGENTS.workspace.md
files:
  CLAUDE.md: "@AGENTS.md\n"
generated:
  go.work: go-work
generators:
  - "python3 handbook/agents/setup-mcp.py"
`)
	run := &runnertest.Fake{}
	run.On("sh -c python3", runner.Result{}, nil)
	s := newState(fsys, run, "app")

	first := planOf(t, s)
	if len(first.Actions) == 0 {
		t.Fatal("first run planned nothing")
	}
	doAll(t, first)

	second := planOf(t, s)
	if len(second.Actions) != 0 || len(second.Findings) != 0 {
		t.Fatalf("second run actions = %v, findings = %+v, want none", summaries(second), second.Findings)
	}
}

func TestRootRejectsUnknownVersion(t *testing.T) {
	fsys := newMemFS().withSpec("version: 2\n")
	_, err := part{state: testState}.Plan(context.Background(), newState(fsys, &runnertest.Fake{}))
	if err == nil {
		t.Fatal("Plan accepted version 2")
	}
	if clierr.ExitCode(err) != clierr.ExitUsage {
		t.Fatalf("exit code = %d, want %d", clierr.ExitCode(err), clierr.ExitUsage)
	}
	if !strings.Contains(err.Error(), specPath) {
		t.Fatalf("error %q does not name %s", err, specPath)
	}
}

func TestRootMissingSpecIsUnverified(t *testing.T) {
	_, err := part{state: testState}.Plan(context.Background(), newState(newMemFS(), &runnertest.Fake{}))
	if clierr.ExitCode(err) != clierr.ExitUnverified {
		t.Fatalf("exit code = %d (%v), want unverified %d", clierr.ExitCode(err), err, clierr.ExitUnverified)
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatal("missing spec should be reported as unverified, not as the raw not-exist error")
	}
}

func TestRootRelinksWrongSymlink(t *testing.T) {
	fsys := newMemFS().
		file("/ws/handbook/agents/AGENTS.workspace.md", "# agents\n").
		file("/ws/handbook/other.md", "other\n").
		link("/ws/AGENTS.md", "handbook/other.md").
		withSpec(`version: 1
links:
  AGENTS.md: handbook/agents/AGENTS.workspace.md
`)
	s := newState(fsys, &runnertest.Fake{})

	plan := planOf(t, s)
	if want := []string{"Relink AGENTS.md"}; !reflect.DeepEqual(summaries(plan), want) {
		t.Fatalf("actions = %v, want %v", summaries(plan), want)
	}
	doAll(t, plan)
	if n := fsys.nodes["/ws/AGENTS.md"]; n == nil || n.kind != "link" || n.target != "handbook/agents/AGENTS.workspace.md" {
		t.Fatalf("AGENTS.md = %+v, want link to handbook/agents/AGENTS.workspace.md", n)
	}
	if again := planOf(t, s); len(again.Actions) != 0 {
		t.Fatalf("after relink planned %v, want nothing", summaries(again))
	}
}

func TestRootAcceptsAbsoluteLinkToSource(t *testing.T) {
	fsys := newMemFS().
		file("/ws/handbook/agents/AGENTS.workspace.md", "# agents\n").
		file("/ws/handbook/agents/README.workspace.md", "# readme\n").
		link("/ws/AGENTS.md", "/ws/handbook/agents/AGENTS.workspace.md").
		link("/ws/README.md", "./handbook/agents/../agents/README.workspace.md").
		withSpec(`version: 1
links:
  AGENTS.md: handbook/agents/AGENTS.workspace.md
  README.md: handbook/agents/README.workspace.md
`)
	plan := planOf(t, newState(fsys, &runnertest.Fake{}))
	if len(plan.Actions) != 0 || len(plan.Findings) != 0 {
		t.Fatalf("actions = %v, findings = %+v, want a link to the source accepted as is", summaries(plan), plan.Findings)
	}
}

func TestRootFileDiffersIsWarning(t *testing.T) {
	fsys := newMemFS().
		file("/ws/handbook/same.md", "same\n").
		file("/ws/CLAUDE.md", "hand edited\n").
		file("/ws/NOTES.md", "my notes\n").
		link("/ws/SAME.md", "handbook/same.md").
		withSpec(`version: 1
files:
  CLAUDE.md: "@AGENTS.md\n"
  NOTES.md: "declared\n"
  SAME.md: "same\n"
`)
	plan := planOf(t, newState(fsys, &runnertest.Fake{}))
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %v, want none: a differing file is never overwritten", summaries(plan))
	}
	want := []engine.Finding{
		{Part: "root", Subject: "CLAUDE.md", Message: "CLAUDE.md differs from root.yaml; delete it to restore the declared content", Severity: engine.SeverityWarn},
		{Part: "root", Subject: "NOTES.md", Message: "NOTES.md differs from root.yaml; delete it to restore the declared content", Severity: engine.SeverityWarn},
	}
	if !reflect.DeepEqual(plan.Findings, want) {
		t.Fatalf("findings = %+v, want %+v", plan.Findings, want)
	}
	if got := string(fsys.nodes["/ws/CLAUDE.md"].data); got != "hand edited\n" {
		t.Fatalf("CLAUDE.md changed to %q", got)
	}
}

func TestGoWorkRemovesOnlyMarked(t *testing.T) {
	fsys := newMemFS().
		file("/ws/go.work", "// generated by orbit setup; do not edit\ngo 1.27\n").
		withSpec(`version: 1
generated:
  go.work: go-work
`)
	s := newState(fsys, &runnertest.Fake{}, "app")
	plan := planOf(t, s)
	if want := []string{"Remove go.work"}; !reflect.DeepEqual(summaries(plan), want) {
		t.Fatalf("marked go.work with no modules: actions = %v, want %v", summaries(plan), want)
	}
	doAll(t, plan)
	if _, ok := fsys.nodes["/ws/go.work"]; ok {
		t.Fatal("marked go.work not removed")
	}

	fsys.file("/ws/go.work", "go 1.27\n")
	plan = planOf(t, s)
	if len(plan.Actions) != 0 {
		t.Fatalf("unmarked go.work with no modules: actions = %v, want none", summaries(plan))
	}
	if !reflect.DeepEqual(plan.Findings, []engine.Finding{{Part: "root", Subject: "go.work", Message: "go.work is hand-made; delete it to let orbit setup manage it", Severity: engine.SeverityWarn}}) {
		t.Fatalf("unmarked go.work with no modules: findings = %+v, want the hand-made warning", plan.Findings)
	}
	if got := string(fsys.nodes["/ws/go.work"].data); got != "go 1.27\n" {
		t.Fatalf("hand-made go.work changed to %q", got)
	}
}

func TestGoWorkHandMadeIsWarning(t *testing.T) {
	fsys := newMemFS().
		file("/ws/clients/alpha/go.mod", "module a\n\ngo 1.27\n").
		file("/ws/go.work", "go 1.9\nuse ./mine\n").
		withSpec(`version: 1
generated:
  go.work: go-work
`)
	plan := planOf(t, newState(fsys, &runnertest.Fake{}, "app"))
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %v, want none for a hand-made go.work", summaries(plan))
	}
	if !reflect.DeepEqual(plan.Findings, []engine.Finding{{Part: "root", Subject: "go.work", Message: "go.work is hand-made; delete it to let orbit setup manage it", Severity: engine.SeverityWarn}}) {
		t.Fatalf("findings = %+v, want the hand-made warning", plan.Findings)
	}
}

func TestGoWorkNoGoDirectiveIsWarning(t *testing.T) {
	fsys := newMemFS().
		file("/ws/clients/alpha/go.mod", "module a\n").
		withSpec(`version: 1
generated:
  go.work: go-work
`)
	plan := planOf(t, newState(fsys, &runnertest.Fake{}, "app"))
	if len(plan.Actions) != 0 || len(plan.Findings) != 1 || plan.Findings[0].Severity != engine.SeverityWarn {
		t.Fatalf("actions = %v, findings = %+v, want one warning and no action", summaries(plan), plan.Findings)
	}
}

func TestRootGeneratorFailureIsToolError(t *testing.T) {
	var lines []string
	for i := 1; i <= 25; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	fsys := newMemFS().
		file("/ws/handbook/agents/AGENTS.workspace.md", "# agents\n").
		withSpec(`version: 1
generators:
  - "python3 handbook/agents/setup-mcp.py"
`)
	run := &runnertest.Fake{}
	run.On("sh -c python3", runner.Result{Stdout: strings.Join(lines, "\n") + "\n", Stderr: "boom\n", ExitCode: 2}, nil)
	s := newState(fsys, run)

	plan := planOf(t, s)
	err := plan.Actions[0].Do(context.Background())
	if err == nil {
		t.Fatal("failing generator returned no error")
	}
	if clierr.ExitCode(err) != clierr.ExitTool {
		t.Fatalf("exit code = %d, want tool %d", clierr.ExitCode(err), clierr.ExitTool)
	}
	msg := err.Error()
	if !strings.Contains(msg, "python3 handbook/agents/setup-mcp.py") || !strings.Contains(msg, "boom") || !strings.Contains(msg, "line 25") {
		t.Fatalf("error %q does not name the command and the output tail", msg)
	}
	if strings.Contains(msg, "line 5\n") {
		t.Fatalf("error %q keeps more than the last 20 lines", msg)
	}
	if _, ok := fsys.nodes[testState]; ok {
		t.Fatal("failed generator was recorded as run")
	}
}

func TestRootCorruptStateFileReruns(t *testing.T) {
	fsys := newMemFS().
		file("/ws/handbook/agents/AGENTS.workspace.md", "# agents\n").
		file(testState, "{not json").
		withSpec(`version: 1
generators:
  - "python3 handbook/agents/setup-mcp.py"
`)
	run := &runnertest.Fake{}
	run.On("sh -c python3", runner.Result{}, nil)
	s := newState(fsys, run)

	plan, err := part{state: testState}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("corrupt state file: Plan error %v", err)
	}
	if want := []string{"Run python3 handbook/agents/setup-mcp.py"}; !reflect.DeepEqual(summaries(plan), want) {
		t.Fatalf("actions = %v, want %v", summaries(plan), want)
	}
	doAll(t, plan)
	var state generatorState
	if err := json.Unmarshal(fsys.nodes[testState].data, &state); err != nil {
		t.Fatalf("state not rewritten as JSON: %v", err)
	}
	if _, ok := state.Generators["python3 handbook/agents/setup-mcp.py"]; !ok {
		t.Fatalf("state = %+v, want the generator recorded", state)
	}
}

func TestInspectDirect(t *testing.T) {
	fsys := newMemFS().
		file("/ws/handbook/agents/AGENTS.workspace.md", "# agents\n").
		file("/ws/handbook/agents/README.workspace.md", "# readme\n").
		file("/ws/handbook/FUTURE-TRIGGERS.md", "triggers\n").
		link("/ws/AGENTS.md", "handbook/agents/AGENTS.workspace.md").
		file("/ws/FUTURE-TRIGGERS.md", "hand-made\n")
	rs, err := spec.ParseRootSpec([]byte(`version: 1
links:
  AGENTS.md: handbook/agents/AGENTS.workspace.md
  README.md: handbook/agents/README.workspace.md
  FUTURE-TRIGGERS.md: handbook/FUTURE-TRIGGERS.md
`))
	if err != nil {
		t.Fatalf("ParseRootSpec: %v", err)
	}
	got := Inspect(fsys, testRoot, rs)
	want := []engine.Finding{
		{Part: "root", Subject: "FUTURE-TRIGGERS.md", Message: "conflict: FUTURE-TRIGGERS.md is hand-made; move it away and rerun", Severity: engine.SeverityError},
		{Part: "root", Subject: "README.md", Message: "not as declared in root.yaml: Link README.md", Severity: engine.SeverityWarn},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Inspect =\n%+v\nwant\n%+v", got, want)
	}
}
