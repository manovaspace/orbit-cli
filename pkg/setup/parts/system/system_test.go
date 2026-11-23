package system

import (
	"context"
	"errors"
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

const githubCLIScript = "install -d -m 0755 /etc/apt/keyrings && " +
	"curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg && " +
	"chmod a+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && " +
	"echo \"deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main\" > /etc/apt/sources.list.d/github-cli.list"

func TestSystemDarwinInstallsHomebrewFirst(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	run := &runnertest.Fake{}
	run.On("brew list --versions", runner.Result{}, nil)
	tty := &runnertest.Fake{}
	tty.On("/bin/bash", runner.Result{}, nil)
	tty.On("brew install", runner.Result{}, nil)
	s := &engine.State{Platform: "darwin", Run: run, Tty: tty, FS: mapFS{}}

	plan, err := part{lookPath: noLookPath}.Plan(context.Background(), s)
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
	if len(tty.Calls()) != 0 {
		t.Fatalf("Plan ran commands: %+v", tty.Calls())
	}

	if err := brewAction.Do(context.Background()); err != nil {
		t.Fatalf("Homebrew Do: %v", err)
	}
	calls := tty.Calls()
	if len(calls) != 1 || !calls[0].Interactive || calls[0].Name != "/bin/bash" {
		t.Fatalf("installer calls = %+v", calls)
	}
	if strings.Join(calls[0].Env, ",") != "NONINTERACTIVE=1" {
		t.Fatalf("installer env = %v", calls[0].Env)
	}
	if got := strings.Join(calls[0].Args, " "); got != "-c "+homebrewInstall {
		t.Fatalf("installer args = %q", got)
	}
	if path := os.Getenv("PATH"); !strings.HasPrefix(path, "/opt/homebrew/bin:") && !strings.HasPrefix(path, "/usr/local/bin:") {
		t.Fatalf("PATH = %q, want Homebrew prefix first", path)
	}
}

func TestSystemDarwinSkipsHomebrewWhenPresent(t *testing.T) {
	run := &runnertest.Fake{}
	run.On("brew list --versions", runner.Result{Stdout: "git 2.47.0"}, nil)
	s := &engine.State{Platform: "darwin", Run: run, Tty: &runnertest.Fake{}, FS: mapFS{files: map[string]string{"/opt/homebrew/bin/brew": ""}}}

	plan, err := part{lookPath: noLookPath}.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Summary == "Install Homebrew" {
			t.Fatalf("Homebrew planned although brew exists: %+v", plan.Actions)
		}
	}
}

func TestSystemLinuxBatchesAptRepoThenInstall(t *testing.T) {
	run := &runnertest.Fake{}
	run.On("dpkg-query -W -f=${Status} git", runner.Result{Stdout: "install ok installed"}, nil)
	run.On("dpkg-query -W -f=${Status} gh", runner.Result{}, errors.New("exit 1"))
	run.On("dpkg-query -W -f=${Status} curl", runner.Result{}, errors.New("exit 1"))
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
	if install.Summary != "apt-get update, then install gh curl" || !install.Sudo {
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
	want := "apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y gh curl"
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
