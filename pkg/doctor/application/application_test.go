package application

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

// fakeProbe answers scripted command lines and records every call.
type fakeProbe struct {
	mu      sync.Mutex
	answers map[string]ProbeResult
	errors  map[string]error
	calls   []string
}

func newFakeProbe() *fakeProbe {
	return &fakeProbe{answers: map[string]ProbeResult{}, errors: map[string]error{}}
}

func (p *fakeProbe) On(command string, result ProbeResult, err error) {
	p.answers[command] = result
	p.errors[command] = err
}

func (p *fakeProbe) Run(_ context.Context, _ time.Duration, name string, args ...string) (ProbeResult, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	p.mu.Lock()
	p.calls = append(p.calls, line)
	result, ok := p.answers[line]
	err := p.errors[line]
	p.mu.Unlock()
	if !ok {
		return ProbeResult{}, errors.New("unexpected probe: " + line)
	}
	return result, err
}

func (p *fakeProbe) called(prefix string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, call := range p.calls {
		if strings.HasPrefix(call, prefix) {
			return true
		}
	}
	return false
}

// fakeHost is an in-memory machine.
type fakeHost struct {
	report   host.Report
	cwd      string
	env      map[string]string
	files    map[string][]byte
	dirs     map[string]bool
	gitDirs  map[string]bool
	lookPath map[string]string
	lookErr  error
}

func newFakeHost(cwd string) *fakeHost {
	return &fakeHost{
		report:   host.Report{OK: true},
		cwd:      cwd,
		env:      map[string]string{},
		files:    map[string][]byte{},
		dirs:     map[string]bool{},
		gitDirs:  map[string]bool{},
		lookPath: map[string]string{},
	}
}

func (h *fakeHost) Report() host.Report { return h.report }

func (h *fakeHost) LookPath(name string) (string, error) {
	if h.lookErr != nil {
		return "", h.lookErr
	}
	if path, ok := h.lookPath[name]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func (h *fakeHost) Getwd() (string, error) { return h.cwd, nil }

func (h *fakeHost) ReadFile(path string) ([]byte, error) {
	if data, ok := h.files[path]; ok {
		return data, nil
	}
	return nil, fs.ErrNotExist
}

func (h *fakeHost) Stat(path string) (FileInfo, error) {
	if h.dirs[path] {
		return FileInfo{IsDir: true}, nil
	}
	if _, ok := h.files[path]; ok {
		return FileInfo{}, nil
	}
	if h.gitDirs[path] {
		return FileInfo{IsDir: true}, nil
	}
	return FileInfo{}, fs.ErrNotExist
}

func (h *fakeHost) Lstat(path string) (FileInfo, error) {
	if h.gitDirs[path] {
		return FileInfo{IsDir: true}, nil
	}
	return h.Stat(path)
}

func (h *fakeHost) Getenv(name string) string { return h.env[name] }

type fakePorts struct{ inUse map[int]bool }

func (p fakePorts) Scan(start, end int) map[int]bool {
	results := make(map[int]bool, end-start+1)
	for port := start; port <= end; port++ {
		results[port] = p.inUse[port]
	}
	return results
}

type fakeManifest struct {
	loadErr error
	repos   []manifest.RepoConfig
}

func (m fakeManifest) Load(string) (*manifest.WorkspaceManifest, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	if len(m.repos) == 0 {
		return &manifest.WorkspaceManifest{}, nil
	}
	return &manifest.WorkspaceManifest{Groups: map[string]manifest.GroupConfig{
		"orbit": {Path: "orbit", Repositories: m.repos},
	}}, nil
}

func healthyProbe() *fakeProbe {
	probe := newFakeProbe()
	probe.On("git version", ProbeResult{Output: "git version 2.50.0"}, nil)
	probe.On("go version", ProbeResult{Output: "go version go1.26.7 linux/amd64"}, nil)
	probe.On("node -v", ProbeResult{Output: "v24.0.0"}, nil)
	probe.On("bun -v", ProbeResult{Output: "1.4.0"}, nil)
	probe.On("caddy version", ProbeResult{Output: "v2.10.0\n"}, nil)
	probe.On("typst --version", ProbeResult{Output: "typst 0.14.0"}, nil)
	return probe
}

func testService(t *testing.T, probe *fakeProbe, machine *fakeHost) *Service {
	t.Helper()
	return &Service{
		Probe:    probe,
		Host:     machine,
		Ports:    fakePorts{},
		Manifest: fakeManifest{},
		Healers:  domain.NewHealerRegistry(),
	}
}

type recordingHealer struct {
	name string
	runs int
}

func (h *recordingHealer) Name() string { return h.name }
func (h *recordingHealer) CanHeal(result domain.DiagnosticResult) bool {
	return result.Status == domain.StatusError
}
func (h *recordingHealer) Heal(context.Context, func(string)) error {
	h.runs++
	return nil
}

func TestDiagnoseLocalNeverProbesRemoteOrHeals(t *testing.T) {
	probe := healthyProbe()
	machine := newFakeHost(t.TempDir())
	machine.gitDirs[filepath.Join(machine.cwd, ".git")] = true
	healer := &recordingHealer{name: "recorder"}
	svc := testService(t, probe, machine)
	svc.Healers.Register(healer)

	result := svc.Diagnose(context.Background(), Options{})
	for _, forbidden := range []string{"ssh ", "docker info", "ssh-add"} {
		if probe.called(forbidden) {
			t.Errorf("local diagnostics called %q", forbidden)
		}
	}
	if healer.runs != 0 {
		t.Error("diagnostics healed without --fix")
	}
	categories := map[string]bool{}
	for _, res := range result.Report.Results {
		categories[res.Category] = true
	}
	for _, expected := range []string{"System", "Toolchain", "Runtime", "Container", "Authentication", "Ports", "Optional Tools"} {
		if !categories[expected] {
			t.Errorf("missing category %q", expected)
		}
	}
}

func TestDiagnoseRemoteProbesSSHAndDocker(t *testing.T) {
	probe := healthyProbe()
	probe.On("docker info", ProbeResult{Output: "Server: Docker Engine"}, nil)
	probe.On("docker compose version", ProbeResult{Output: "Docker Compose version 2.40.3"}, nil)
	probe.On("ssh-add -l", ProbeResult{Output: "256 SHA256:key"}, nil)
	probe.On("ssh -F /dev/null -o BatchMode=yes -o ConnectTimeout=3 -o UpdateHostKeys=no -o ControlMaster=no -o StrictHostKeyChecking=yes -T git@github.com",
		ProbeResult{Output: "Hi dev! You've successfully authenticated"}, nil)
	machine := newFakeHost(t.TempDir())
	machine.env["SSH_AUTH_SOCK"] = "/tmp/agent"
	machine.lookPath["docker"] = "/usr/bin/docker"
	svc := testService(t, probe, machine)

	result := svc.Diagnose(context.Background(), Options{Remote: true})
	if !probe.called("docker info") || !probe.called("ssh ") {
		t.Fatal("remote probes missing")
	}
	found := false
	for _, res := range result.Report.Results {
		if res.Name == "GitHub SSH (github.com)" && res.Status == domain.StatusOK {
			found = true
		}
	}
	if !found {
		t.Errorf("GitHub SSH result missing: %+v", result.Report.Results)
	}
}

func TestHealRequiresExplicitFix(t *testing.T) {
	probe := healthyProbe()
	machine := newFakeHost(t.TempDir())
	machine.gitDirs[filepath.Join(machine.cwd, ".git")] = true
	healer := &recordingHealer{name: "recorder"}
	svc := testService(t, probe, machine)
	svc.Healers.Register(healer)
	report := domain.NewReport()
	report.Add(domain.DiagnosticResult{Name: "Go Compiler", Status: domain.StatusError})

	result, err := svc.Heal(context.Background(), report, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Ran || healer.runs != 0 {
		t.Fatal("healer ran without the fix flag")
	}

	result, err = svc.Heal(context.Background(), report, Options{Fix: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Ran || healer.runs != 1 {
		t.Fatalf("healer did not run with --fix: ran=%v runs=%d", result.Ran, healer.runs)
	}
}

func TestToolchainUsesProjectBunPin(t *testing.T) {
	root := t.TempDir()
	machine := newFakeHost(root)
	machine.files[filepath.Join(root, "package.json")] = []byte(`{"packageManager":"bun@1.3.14","engines":{"node":">=20"}}`)
	probe := healthyProbe()
	probe.On("node -v", ProbeResult{Output: "20.10.0"}, nil)
	probe.On("bun -v", ProbeResult{Output: "1.3.14"}, nil)

	results := (toolchainCheck{probe: probe, host: machine}).Run(context.Background())
	for _, result := range results {
		if result.Name == "Bun" || result.Name == "Node.js" {
			if result.Status != domain.StatusOK {
				t.Errorf("project pinned runtime rejected: %+v", result)
			}
		}
	}
}

func TestToolchainReportsConflictingBunPin(t *testing.T) {
	root := t.TempDir()
	machine := newFakeHost(root)
	machine.files[filepath.Join(root, "package.json")] = []byte(`{"packageManager":"bun@1.3.14","engines":{"bun":">=1.4.0"}}`)
	probe := healthyProbe()
	probe.On("bun -v", ProbeResult{Output: "1.3.14"}, nil)

	found := false
	for _, result := range (toolchainCheck{probe: probe, host: machine}).Run(context.Background()) {
		if result.Name == "Bun manifest conflict" && result.Status == domain.StatusError {
			found = true
		}
	}
	if !found {
		t.Fatal("conflicting manifests not reported")
	}
}

func TestToolchainStopsAtRepositoryBoundary(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	machine := newFakeHost(repo)
	machine.gitDirs[filepath.Join(repo, ".git")] = true
	machine.files[filepath.Join(root, "package.json")] = []byte(`{"packageManager":"bun@1.4.0"}`)
	machine.files[filepath.Join(repo, "go.mod")] = []byte("module fixture\ngo 1.26.0\n")
	probe := healthyProbe()
	probe.On("bun -v", ProbeResult{Output: "1.3.14"}, nil)

	for _, result := range (toolchainCheck{probe: probe, host: machine}).Run(context.Background()) {
		if result.Name == "Bun" && result.Status != domain.StatusOK {
			t.Fatalf("workspace package pin leaked into independent Go repo: %+v", result)
		}
	}
}

func TestWorkspaceCheckIsReadOnlyAndFailsUnverifiedManifests(t *testing.T) {
	root := t.TempDir()
	machine := newFakeHost(root)
	machine.files[filepath.Join(root, "workspace.yaml")] = []byte("version: '1'\n")
	machine.dirs[filepath.Join(root, "orbit")] = true
	machine.dirs[filepath.Join(root, "orbit", "fixture")] = true
	machine.gitDirs[filepath.Join(root, "orbit", "fixture", ".git")] = true
	probe := healthyProbe()

	check := workspaceCheck{host: machine, manifest: fakeManifest{repos: []manifest.RepoConfig{{Name: "fixture", Path: "fixture"}}}, root: root, path: filepath.Join(root, "workspace.yaml")}
	results := check.Run(context.Background())
	if len(results) != 1 || results[0].Status != domain.StatusOK {
		t.Fatalf("results = %+v", results)
	}
	if probe.called("git") {
		t.Error("workspace check ran git")
	}

	for _, kind := range []string{"missing", "unloadable", "empty"} {
		t.Run(kind, func(t *testing.T) {
			machine := newFakeHost(root)
			check := workspaceCheck{host: machine, manifest: fakeManifest{}, root: root, path: filepath.Join(root, "workspace.yaml")}
			switch kind {
			case "unloadable":
				machine.files[check.path] = []byte("bad")
				check.manifest = fakeManifest{loadErr: errors.New("bad")}
			case "empty":
				machine.files[check.path] = []byte("version: '1'")
				check.manifest = fakeManifest{}
			}
			results := check.Run(context.Background())
			if len(results) != 1 || results[0].Status != domain.StatusError || !strings.Contains(results[0].Message, "unverified") {
				t.Fatalf("%s manifest not rejected: %+v", kind, results)
			}
		})
	}
}

func TestAcceptHostKeysOnlyChangesStrictHostKeyChecking(t *testing.T) {
	probe := healthyProbe()
	probe.On("ssh-add -l", ProbeResult{Output: "256 SHA256:key"}, nil)
	probe.On("ssh -F /dev/null -o BatchMode=yes -o ConnectTimeout=3 -o UpdateHostKeys=no -o ControlMaster=no -o StrictHostKeyChecking=accept-new -T git@github.com",
		ProbeResult{Output: "Hi dev! You've successfully authenticated"}, nil)
	machine := newFakeHost(t.TempDir())
	machine.env["SSH_AUTH_SOCK"] = "/tmp/agent"

	check := sshCheck{probe: probe, host: machine, remote: true, acceptHostKeys: true}
	results := check.Run(context.Background())
	if len(results) != 2 || results[1].Status != domain.StatusOK {
		t.Fatalf("results = %+v", results)
	}
	if probe.called("ssh -F /dev/null -o BatchMode=yes -o ConnectTimeout=3 -o UpdateHostKeys=no -o ControlMaster=no -o StrictHostKeyChecking=accept-new") == false {
		t.Fatal("accept-new host-key mode not passed to ssh")
	}
}

var _ Probe = (*fakeProbe)(nil)
var _ Host = (*fakeHost)(nil)
