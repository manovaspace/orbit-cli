package system

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

// mapFS is a map-backed engine.FS. Paths in files exist; others report ErrNotExist.
type mapFS struct {
	files map[string]string
}

type fileInfo struct{ fs.FileInfo }

var errReadOnly = errors.New("mapFS: write not supported")

func (m mapFS) Stat(path string) (fs.FileInfo, error) {
	if _, ok := m.files[path]; ok {
		return fileInfo{}, nil
	}
	return nil, fs.ErrNotExist
}
func (m mapFS) Lstat(path string) (fs.FileInfo, error) { return m.Stat(path) }
func (m mapFS) ReadFile(path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(data), nil
}
func (m mapFS) WriteFile(string, []byte, os.FileMode) error { return errReadOnly }
func (m mapFS) MkdirAll(string, os.FileMode) error          { return errReadOnly }
func (m mapFS) Symlink(string, string) error                { return errReadOnly }
func (m mapFS) Readlink(string) (string, error)             { return "", errReadOnly }
func (m mapFS) Remove(string) error                         { return errReadOnly }
func (m mapFS) Rename(string, string) error                 { return errReadOnly }

func noLookPath(string) (string, error) { return "", errors.New("not found") }

// envRecorder stands in for os.Setenv so tests never touch the process PATH.
type envRecorder struct {
	calls [][2]string
}

func (e *envRecorder) setenv(key, value string) error {
	e.calls = append(e.calls, [2]string{key, value})
	return nil
}

const githubCLIScript = "install -d -m 0755 /etc/apt/keyrings && " +
	"curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg && " +
	"chmod a+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && " +
	"echo \"deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main\" > /etc/apt/sources.list.d/github-cli.list"

const wantHomebrewCommand = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

func TestSystemDarwinInstallsHomebrewFirst(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	tty := &runnertest.Fake{}
	tty.On("/bin/bash", runner.Result{}, nil)
	tty.On("brew install", runner.Result{}, nil)
	rec := &envRecorder{}
	s := &engine.State{Platform: "darwin", Run: &runnertest.Fake{}, Tty: tty, FS: mapFS{}}

	plan, err := part{lookPath: noLookPath, setenv: rec.setenv}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("actions = %d, want 2 (Homebrew, brew install)", len(plan.Actions))
	}
	brewAction := plan.Actions[0]
	if brewAction.Summary != "Install Homebrew" || !brewAction.Sudo || brewAction.Browser || brewAction.Part != "system" {
		t.Fatalf("first action = %+v", brewAction)
	}
	if plan.Actions[1].Summary != "brew install git gh curl" {
		t.Fatalf("second action = %q", plan.Actions[1].Summary)
	}
	if len(tty.Calls()) != 0 || len(rec.calls) != 0 {
		t.Fatalf("Plan ran commands or set PATH: calls=%+v env=%v", tty.Calls(), rec.calls)
	}

	if err := brewAction.Do(context.Background()); err != nil {
		t.Fatalf("Homebrew Do: %v", err)
	}
	calls := tty.Calls()
	if len(calls) != 1 || !calls[0].Interactive || calls[0].Name != "/bin/bash" {
		t.Fatalf("installer calls = %+v", calls)
	}
	if got := strings.Join(calls[0].Env, ","); got != "NONINTERACTIVE=1" {
		t.Fatalf("installer env = %q", got)
	}
	if len(calls[0].Args) != 2 || calls[0].Args[0] != "-c" || calls[0].Args[1] != wantHomebrewCommand {
		t.Fatalf("installer args = %q, want [-c %q]", calls[0].Args, wantHomebrewCommand)
	}
	if len(rec.calls) != 1 || rec.calls[0][0] != "PATH" || !strings.HasPrefix(rec.calls[0][1], "/opt/homebrew/bin:") && !strings.HasPrefix(rec.calls[0][1], "/usr/local/bin:") {
		t.Fatalf("PATH updates = %v, want Homebrew prefix first", rec.calls)
	}
}

func TestSystemDarwinBrewOffPathFixesPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	run := &runnertest.Fake{}
	run.On("brew list --versions", runner.Result{Stdout: "installed 1.0"}, nil)
	tty := &runnertest.Fake{}
	rec := &envRecorder{}
	s := &engine.State{Platform: "darwin", Run: run, Tty: tty, FS: mapFS{files: map[string]string{"/usr/local/bin/brew": ""}}}

	plan, err := part{lookPath: noLookPath, setenv: rec.setenv}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Summary == "Install Homebrew" {
			t.Fatalf("Homebrew planned although brew exists at a fixed path: %+v", plan.Actions)
		}
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %+v, want none (brew present and base packages installed)", plan.Actions)
	}
	want := [][2]string{{"PATH", "/usr/local/bin:/usr/bin:/bin"}}
	if fmt.Sprint(rec.calls) != fmt.Sprint(want) {
		t.Fatalf("PATH updates = %v, want %v", rec.calls, want)
	}
	if len(tty.Calls()) != 0 {
		t.Fatalf("Plan ran installer commands: %+v", tty.Calls())
	}
}

func TestSystemDarwinSkipsHomebrewWhenOnPath(t *testing.T) {
	run := &runnertest.Fake{}
	run.On("brew list --versions", runner.Result{Stdout: "git 2.47.0"}, nil)
	rec := &envRecorder{}
	s := &engine.State{Platform: "darwin", Run: run, Tty: &runnertest.Fake{}, FS: mapFS{}}
	onPath := func(string) (string, error) { return "/opt/homebrew/bin/brew", nil }

	plan, err := part{lookPath: onPath, setenv: rec.setenv}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Summary == "Install Homebrew" {
			t.Fatalf("Homebrew planned although brew is on PATH: %+v", plan.Actions)
		}
	}
	if len(rec.calls) != 0 {
		t.Fatalf("PATH changed although brew is on PATH: %v", rec.calls)
	}
}

func TestSystemLinuxBatchesAptRepoThenInstall(t *testing.T) {
	run := &runnertest.Fake{}
	run.On("dpkg-query -W -f=${Status} git", runner.Result{Stdout: "install ok installed"}, nil)
	run.On("dpkg-query -W -f=${Status} curl", runner.Result{Stdout: "install ok installed"}, nil)
	run.On("dpkg-query -W -f=${Status} ca-certificates", runner.Result{Stdout: "install ok installed"}, nil)
	run.On("dpkg-query -W -f=${Status} gh", runner.Result{ExitCode: 1}, errors.New("exit status 1"))
	tty := &runnertest.Fake{}
	tty.On("sudo", runner.Result{}, nil)
	s := &engine.State{Platform: "linux", Run: run, Tty: tty, FS: mapFS{}}

	plan, err := part{lookPath: noLookPath}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("actions = %d, want 2 (github-cli repo, one install)", len(plan.Actions))
	}
	repo, install := plan.Actions[0], plan.Actions[1]
	if repo.Subject != "github-cli" || !repo.Sudo || repo.Part != "system" {
		t.Fatalf("repo action = %+v", repo)
	}
	if install.Summary != "apt-get update, then install gh" || !install.Sudo {
		t.Fatalf("install action = %+v", install)
	}

	if err := repo.Do(context.Background()); err != nil {
		t.Fatalf("repo Do: %v", err)
	}
	if err := install.Do(context.Background()); err != nil {
		t.Fatalf("install Do: %v", err)
	}
	calls := tty.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if !calls[0].Interactive || calls[0].Name != "sudo" || strings.Join(calls[0].Args[:2], " ") != "sh -c" || calls[0].Args[2] != githubCLIScript {
		t.Fatalf("repo call = %+v", calls[0])
	}
	want := "apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y gh"
	if calls[1].Name != "sudo" || calls[1].Args[2] != want {
		t.Fatalf("install call = %+v, want script %q", calls[1], want)
	}
}

func TestSystemNothingMissingNoActions(t *testing.T) {
	run := &runnertest.Fake{}
	for _, pkg := range []string{"git", "gh", "curl"} {
		run.On("dpkg-query -W -f=${Status} "+pkg, runner.Result{Stdout: "install ok installed"}, nil)
	}
	s := &engine.State{Platform: "linux", Run: run, Tty: &runnertest.Fake{}, FS: mapFS{}}

	plan, err := part{lookPath: noLookPath}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %+v, want none", plan.Actions)
	}
}

func TestSystemNameAndNeeds(t *testing.T) {
	p := New()
	if p.Name() != "system" || len(p.Needs()) != 0 {
		t.Fatalf("Name=%q Needs=%v", p.Name(), p.Needs())
	}
}
