package toolchains

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

// envRecorder stands in for os.Setenv so tests never touch the process PATH.
type envRecorder struct {
	calls [][2]string
}

func (e *envRecorder) setenv(key, value string) error {
	e.calls = append(e.calls, [2]string{key, value})
	return nil
}

// brewFS reports a Homebrew brew binary at prefix/bin/brew, so darwin tests do not depend on the
// host having Homebrew.
type brewFS struct {
	engine.OSFS
	prefix string
}

func (f brewFS) Stat(path string) (fs.FileInfo, error) {
	if path == f.prefix+"/bin/brew" {
		return nil, nil
	}
	return f.OSFS.Stat(path)
}

func noLookPath(string) (string, error) { return "", errors.New("not found") }

func allLookPath(string) (string, error) { return "/usr/bin/x", nil }

// harness is one machine: a workspace root and a home directory on disk, the environment as a map
// and the command seams.
type harness struct {
	t        *testing.T
	root     string
	home     string
	env      map[string]string
	rec      *envRecorder
	lookPath func(string) (string, error)
	fsys     engine.FS
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{
		t:        t,
		root:     t.TempDir(),
		home:     t.TempDir(),
		env:      map[string]string{"PATH": "/usr/bin:/bin", "SHELL": "/bin/zsh"},
		rec:      &envRecorder{},
		lookPath: noLookPath,
		fsys:     engine.OSFS{},
	}
}

func (h *harness) part() part {
	return part{
		lookPath: h.lookPath,
		setenv:   h.rec.setenv,
		getenv:   func(key string) string { return h.env[key] },
		home:     func() (string, error) { return h.home, nil },
	}
}

// addRepo writes orbit.yaml and, when pkg is not empty, package.json into the repo at path.
func (h *harness) addRepo(path, orbit, pkg string) {
	h.t.Helper()
	dir := filepath.Join(h.root, path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orbit.yaml"), []byte(orbit), 0o644); err != nil {
		h.t.Fatal(err)
	}
	if pkg != "" {
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
			h.t.Fatal(err)
		}
	}
}

// state builds the engine state for an "app" project that selects repos.
func (h *harness) state(platform, services string, run, tty *runnertest.Fake, repos ...string) *engine.State {
	h.t.Helper()
	var configs []manifest.RepoConfig
	for _, path := range repos {
		name := filepath.Base(path)
		configs = append(configs, manifest.RepoConfig{
			Name:          name,
			Path:          path,
			RemoteURL:     "git@github.com:manovaspace/" + name + ".git",
			DefaultBranch: "main",
		})
	}
	return &engine.State{
		Root:     h.root,
		Machine:  spec.Machine{Projects: []string{"app"}, Services: services, GitProtocol: "https"},
		Platform: platform,
		Manifest: &manifest.WorkspaceManifest{
			Groups:   map[string]manifest.GroupConfig{"all": {Repositories: configs}},
			Projects: map[string]manifest.ProjectConfig{"app": {Repos: repos}},
		},
		Run: run,
		Tty: tty,
		FS:  h.fsys,
	}
}

func summaries(plan engine.Plan) []string {
	out := make([]string, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		out = append(out, a.Summary)
	}
	return out
}

func messages(findings []engine.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Message)
	}
	return out
}

func TestToolchainsCollectsFromRepoSpecs(t *testing.T) {
	h := newHarness(t)
	h.addRepo("clients/alpha", "version: 1\ntoolchains: [go, node]\nservices:\n  postgres: {}\n", "")
	h.addRepo("clients/beta", "version: 1\ntoolchains: [bun]\nservices:\n  mail: true\n", "")
	run := &runnertest.Fake{}
	run.On("brew list --versions", runner.Result{}, nil)
	tty := &runnertest.Fake{}

	plan, err := h.part().Plan(context.Background(), h.state("darwin", "native", run, tty, "clients/alpha", "clients/beta"))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{
		"brew install go node@24 postgresql@18 mailpit",
		"install bun from https://bun.sh/install",
		"Update ~/.zshrc",
	}
	if got := summaries(plan); !slices.Equal(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	if len(tty.Calls()) != 0 {
		t.Fatalf("Plan ran commands: %+v", tty.Calls())
	}
}

func TestToolchainsSkipsMailpitInExternalMode(t *testing.T) {
	h := newHarness(t)
	h.addRepo("clients/alpha", "version: 1\ntoolchains: [go, node]\nservices:\n  postgres: {}\n", "")
	h.addRepo("clients/beta", "version: 1\ntoolchains: [bun]\nservices:\n  mail: true\n", "")
	run := &runnertest.Fake{}
	run.On("brew list --versions", runner.Result{}, nil)

	plan, err := h.part().Plan(context.Background(), h.state("darwin", "external", run, &runnertest.Fake{}, "clients/alpha", "clients/beta"))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{
		"brew install go node@24",
		"install bun from https://bun.sh/install",
		"Update ~/.zshrc",
	}
	if got := summaries(plan); !slices.Equal(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	for _, c := range run.Calls() {
		if strings.Contains(strings.Join(c.Args, " "), "mailpit") || strings.Contains(strings.Join(c.Args, " "), "postgresql") {
			t.Fatalf("external mode queried a local service: %+v", c)
		}
	}
}

func TestToolchainsBunPinnedInstall(t *testing.T) {
	const pin = `{"packageManager": "bun@1.3.4"}`
	const wantCmd = "curl -fsSL https://bun.sh/install | bash -s bun-v1.3.4"
	cases := []struct {
		name     string
		lookPath func(string) (string, error)
		version  string
		want     bool
	}{
		{"installed with older minor", allLookPath, "1.2.9\n", true},
		{"patch difference only", allLookPath, "1.3.1\n", false},
		{"missing", noLookPath, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.lookPath = tc.lookPath
			h.addRepo("clients/alpha", "version: 1\ntoolchains: [bun]\n", pin)
			run := &runnertest.Fake{}
			if tc.version != "" {
				run.On("bun --version", runner.Result{Stdout: tc.version}, nil)
			}
			tty := &runnertest.Fake{}
			tty.On("bash -c", runner.Result{}, nil)

			plan, err := h.part().Plan(context.Background(), h.state("darwin", "native", run, tty, "clients/alpha"))
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if len(plan.Findings) != 0 {
				t.Fatalf("findings = %+v, want none", plan.Findings)
			}
			var bun []engine.Action
			for _, a := range plan.Actions {
				if a.Subject == "bun" {
					bun = append(bun, a)
				}
			}
			if !tc.want {
				if len(bun) != 0 {
					t.Fatalf("bun actions = %q, want none", summaries(engine.Plan{Actions: bun}))
				}
				return
			}
			if len(bun) != 1 || bun[0].Summary != "Install bun 1.3.4" || bun[0].Sudo {
				t.Fatalf("bun actions = %+v, want one unprivileged Install bun 1.3.4", bun)
			}
			if err := bun[0].Do(context.Background()); err != nil {
				t.Fatalf("bun Do: %v", err)
			}
			calls := tty.Calls()
			if len(calls) != 1 || !calls[0].Interactive || calls[0].Name != "bash" || len(calls[0].Args) != 2 ||
				calls[0].Args[0] != "-c" || calls[0].Args[1] != wantCmd {
				t.Fatalf("bun install calls = %+v, want bash -c %q", calls, wantCmd)
			}
		})
	}
}

func TestToolchainsBashWritesBashrcAndProfile(t *testing.T) {
	h := newHarness(t)
	h.env["SHELL"] = "/bin/bash"
	s := h.state("linux", "native", &runnertest.Fake{}, &runnertest.Fake{})

	plan, err := h.part().Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{"Update ~/.bashrc", "Update ~/.profile"}
	if got := summaries(plan); !slices.Equal(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	for _, a := range plan.Actions {
		if err := a.Do(context.Background()); err != nil {
			t.Fatalf("%s Do: %v", a.Summary, err)
		}
	}
	for _, name := range []string{".bashrc", ".profile"} {
		path := filepath.Join(h.home, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(data) != Block("linux", h.home) {
			t.Fatalf("%s = %q, want the linux block", name, data)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("%s mode = %v, %v; want 0644", name, info.Mode().Perm(), err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.home, ".zshrc")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf(".zshrc created for a bash login shell: %v", err)
	}
}

func TestToolchainsPrependsPathForLaterParts(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		path     func(h *harness) string
		brew     bool
		want     func(h *harness) string
	}{
		{
			name:     "linux adds the user tool dirs first",
			platform: "linux",
			path:     func(*harness) string { return "/usr/bin:/bin" },
			want: func(h *harness) string {
				return h.home + "/.local/bin:" + h.home + "/.bun/bin:" + h.home + "/go/bin:/usr/bin:/bin"
			},
		},
		{
			name:     "dir already on PATH is not repeated",
			platform: "linux",
			path:     func(h *harness) string { return "/usr/bin:" + h.home + "/go/bin:/bin" },
			want: func(h *harness) string {
				return h.home + "/.local/bin:" + h.home + "/.bun/bin:/usr/bin:" + h.home + "/go/bin:/bin"
			},
		},
		{
			name:     "darwin with Homebrew adds the keg-only formulae",
			platform: "darwin",
			path:     func(*harness) string { return "/usr/bin:/bin" },
			brew:     true,
			want: func(h *harness) string {
				return h.home + "/.local/bin:" + h.home + "/.bun/bin:" + h.home + "/go/bin:" +
					"/opt/homebrew/opt/node@24/bin:/opt/homebrew/opt/postgresql@18/bin:/usr/bin:/bin"
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.env["PATH"] = tc.path(h)
			if tc.brew {
				h.fsys = brewFS{prefix: "/opt/homebrew"}
			}
			s := h.state(tc.platform, "native", &runnertest.Fake{}, &runnertest.Fake{})

			if _, err := h.part().Plan(context.Background(), s); err != nil {
				t.Fatalf("Plan: %v", err)
			}
			want := [2]string{"PATH", tc.want(h)}
			if len(h.rec.calls) != 1 || h.rec.calls[0] != want {
				t.Fatalf("PATH updates = %v, want [%v]", h.rec.calls, want)
			}
		})
	}
}

func TestToolchainsSecondRunNoActions(t *testing.T) {
	h := newHarness(t)
	h.addRepo("clients/alpha", "version: 1\ntoolchains: [go, node, bun]\n",
		`{"engines": {"node": ">=24"}, "packageManager": "bun@1.3.4"}`)

	// First run: nothing installed. Only the rc update is applied; the install actions are not run.
	first := &runnertest.Fake{}
	first.On("brew list --versions", runner.Result{}, nil)
	plan, err := h.part().Plan(context.Background(), h.state("darwin", "native", first, &runnertest.Fake{}, "clients/alpha"))
	if err != nil {
		t.Fatalf("first Plan: %v", err)
	}
	var rc []engine.Action
	for _, a := range plan.Actions {
		if strings.HasPrefix(a.Summary, "Update ") {
			rc = append(rc, a)
		}
	}
	if len(rc) != 1 {
		t.Fatalf("first run rc actions = %q, want one", summaries(plan))
	}
	if err := rc[0].Do(context.Background()); err != nil {
		t.Fatalf("rc Do: %v", err)
	}

	// Second run: everything installed and matching.
	second := &runnertest.Fake{}
	second.On("brew list --versions", runner.Result{Stdout: "installed 1.0\n"}, nil)
	second.On("go env GOVERSION", runner.Result{Stdout: "go1.27.4\n"}, nil)
	second.On("node --version", runner.Result{Stdout: "v24.1.0\n"}, nil)
	second.On("bun --version", runner.Result{Stdout: "1.3.4\n"}, nil)
	h.lookPath = allLookPath
	plan, err = h.part().Plan(context.Background(), h.state("darwin", "native", second, &runnertest.Fake{}, "clients/alpha"))
	if err != nil {
		t.Fatalf("second Plan: %v", err)
	}
	if len(plan.Actions) != 0 || len(plan.Findings) != 0 {
		t.Fatalf("second run actions = %q, findings = %+v; want none", summaries(plan), plan.Findings)
	}
}

func TestToolchainsWarnsOldGoAndNode(t *testing.T) {
	h := newHarness(t)
	h.addRepo("clients/alpha", "version: 1\ntoolchains: [go, node]\n", `{"engines": {"node": ">=24"}}`)
	run := &runnertest.Fake{}
	run.On("brew list --versions", runner.Result{Stdout: "installed 1.0\n"}, nil)
	run.On("go env GOVERSION", runner.Result{Stdout: "go1.20.5\n"}, nil)
	run.On("node --version", runner.Result{Stdout: "v22.11.0\n"}, nil)
	h.lookPath = allLookPath

	plan, err := h.part().Plan(context.Background(), h.state("darwin", "native", run, &runnertest.Fake{}, "clients/alpha"))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{"go 1.20.5 is older than 1.21; upgrade it", "node 22.11.0 found; 24 required"}
	if got := messages(plan.Findings); !slices.Equal(got, want) {
		t.Fatalf("findings = %q, want %q", got, want)
	}
	for _, f := range plan.Findings {
		if f.Severity != engine.SeverityWarn {
			t.Fatalf("finding %+v severity, want warn", f)
		}
	}
	if got := summaries(plan); !slices.Equal(got, []string{"Update ~/.zshrc"}) {
		t.Fatalf("actions = %q, want only the rc update (no install or upgrade action)", got)
	}
}

func TestToolchainsUnknownShellWarnsWithBlock(t *testing.T) {
	h := newHarness(t)
	h.env["SHELL"] = "/usr/bin/fish"
	plan, err := h.part().Plan(context.Background(), h.state("linux", "native", &runnertest.Fake{}, &runnertest.Fake{}))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %q, want none", summaries(plan))
	}
	if len(plan.Findings) != 1 || plan.Findings[0].Severity != engine.SeverityWarn {
		t.Fatalf("findings = %+v, want one warn", plan.Findings)
	}
	if plan.Findings[0].Fix != Block("linux", h.home) {
		t.Fatalf("finding Fix = %q, want the block", plan.Findings[0].Fix)
	}
}

func TestToolchainsBadPackageJSONIsWarning(t *testing.T) {
	cases := []struct {
		name  string
		orbit string
		pkg   string
		want  []string
	}{
		{
			name:  "malformed JSON skips both checks",
			orbit: "version: 1\ntoolchains: [node, bun]\n",
			pkg:   `{"engines": {"node": ">=24"`,
			want: []string{
				"clients/alpha/package.json: malformed JSON; skipped the node version check",
				"clients/alpha/package.json: malformed JSON; skipped the bun pin",
			},
		},
		{
			name:  "engines.node missing",
			orbit: "version: 1\ntoolchains: [node]\n",
			pkg:   `{}`,
			want:  []string{"clients/alpha/package.json: engines.node is missing; skipped the node version check"},
		},
		{
			name:  "engines.node without a major",
			orbit: "version: 1\ntoolchains: [node]\n",
			pkg:   `{"engines": {"node": "latest"}}`,
			want:  []string{`clients/alpha/package.json: engines.node "latest" has no major version; skipped the node version check`},
		},
		{
			name:  "prerelease bun pin",
			orbit: "version: 1\ntoolchains: [bun]\n",
			pkg:   `{"packageManager": "bun@1.3.4-canary.1"}`,
			want:  []string{`clients/alpha/package.json: packageManager "bun@1.3.4-canary.1" is not bun@<major>.<minor>.<patch>; skipped the bun pin`},
		},
		{
			name:  "another package manager",
			orbit: "version: 1\ntoolchains: [bun]\n",
			pkg:   `{"packageManager": "npm@10"}`,
			want:  []string{`clients/alpha/package.json: packageManager "npm@10" is not bun@<major>.<minor>.<patch>; skipped the bun pin`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.addRepo("clients/alpha", tc.orbit, tc.pkg)
			run := &runnertest.Fake{}
			run.On("brew list --versions", runner.Result{Stdout: "installed 1.0\n"}, nil)
			h.lookPath = allLookPath

			plan, err := h.part().Plan(context.Background(), h.state("darwin", "native", run, &runnertest.Fake{}, "clients/alpha"))
			if err != nil {
				t.Fatalf("Plan returned an error, want warnings: %v", err)
			}
			if got := messages(plan.Findings); !slices.Equal(got, tc.want) {
				t.Fatalf("findings = %q, want %q", got, tc.want)
			}
			for _, f := range plan.Findings {
				if f.Severity != engine.SeverityWarn {
					t.Fatalf("finding %+v severity, want warn", f)
				}
			}
			if got := summaries(plan); !slices.Equal(got, []string{"Update ~/.zshrc"}) {
				t.Fatalf("actions = %q, want only the rc update", got)
			}
		})
	}
}
