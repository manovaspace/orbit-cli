package pkgmgr

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
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

// mapFS is a map-backed engine.FS for tests. Paths in files exist; others report ErrNotExist.
type mapFS struct {
	files map[string]string
}

type fileInfo struct{ fs.FileInfo }

var errReadOnly = errors.New("mapFS: write not supported")

func (m mapFS) Lstat(path string) (fs.FileInfo, error)      { return m.Stat(path) }
func (m mapFS) WriteFile(string, []byte, fs.FileMode) error { return errReadOnly }
func (m mapFS) MkdirAll(string, fs.FileMode) error          { return errReadOnly }
func (m mapFS) Symlink(string, string) error                { return errReadOnly }
func (m mapFS) Readlink(string) (string, error)             { return "", errReadOnly }
func (m mapFS) Remove(string) error                         { return errReadOnly }
func (m mapFS) Rename(string, string) error                 { return errReadOnly }
func (m mapFS) ReadDir(string) ([]os.DirEntry, error)       { return nil, errReadOnly }
func (m mapFS) RemoveAll(string) error                      { return errReadOnly }

func (m mapFS) Stat(path string) (fs.FileInfo, error) {
	if _, ok := m.files[path]; ok {
		return fileInfo{}, nil
	}
	return nil, fs.ErrNotExist
}

func (m mapFS) ReadFile(path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(data), nil
}

func noLookPath(string) (string, error) { return "", errors.New("not found") }

func aptState(t *testing.T, files map[string]string) (*engine.State, *runnertest.Fake, *runnertest.Fake) {
	t.Helper()
	run := &runnertest.Fake{}
	run.On("dpkg-query -W -f=${Status} curl", runner.Result{Stdout: "install ok installed"}, nil)
	run.On("dpkg-query -W -f=${Status} ca-certificates", runner.Result{Stdout: "install ok installed"}, nil)
	tty := &runnertest.Fake{}
	s := &engine.State{Platform: "linux", Run: run, Tty: tty, FS: mapFS{files: files}}
	return s, run, tty
}

func TestAptPostgresDisablesMainCluster(t *testing.T) {
	s, _, tty := aptState(t, nil)
	tty.On("sudo", runner.Result{}, nil)
	mgr, err := ForLookPath(s, noLookPath)
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	actions := mgr.Install([]string{"postgres"})
	if len(actions) != 3 {
		t.Fatalf("actions = %d, want 3 (pgdg repo, main cluster, install)", len(actions))
	}
	if actions[0].Subject != "pgdg" || actions[1].Subject != pgAptPackage || actions[2].Subject != "apt" {
		t.Fatalf("order = %q %q %q", actions[0].Subject, actions[1].Subject, actions[2].Subject)
	}
	if err := actions[1].Do(context.Background()); err != nil {
		t.Fatalf("cluster Do: %v", err)
	}
	wantCluster := "install -d /etc/postgresql-common && echo 'create_main_cluster = false' >> /etc/postgresql-common/createcluster.conf"
	calls := tty.Calls()
	if len(calls) != 1 || !calls[0].Interactive || calls[0].Name != "sudo" || calls[0].Args[0] != "sh" || calls[0].Args[1] != "-c" || calls[0].Args[2] != wantCluster {
		t.Fatalf("cluster calls = %+v", calls)
	}
	if err := actions[2].Do(context.Background()); err != nil {
		t.Fatalf("install Do: %v", err)
	}
	wantInstall := `apt-get update -o DPkg::Lock::Timeout=300 && DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get install -y -o DPkg::Lock::Timeout=300 -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" postgresql-18`
	calls = tty.Calls()
	if got := calls[len(calls)-1].Args[2]; got != wantInstall {
		t.Fatalf("install script = %q, want %q", got, wantInstall)
	}
}

func TestAptPostgresSkipsClusterWhenConfigured(t *testing.T) {
	files := map[string]string{clusterConfig: "# comment\ncreate_main_cluster = false\n"}
	s, _, _ := aptState(t, files)
	mgr, err := ForLookPath(s, noLookPath)
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	for _, action := range mgr.Install([]string{"postgres"}) {
		if strings.Contains(action.Summary, "main cluster") {
			t.Fatalf("unexpected main-cluster action %q", action.Summary)
		}
	}
}

func TestAptRepoSkippedWhenListExists(t *testing.T) {
	files := map[string]string{aptRepos["pgdg"].List: "deb ...\n"}
	s, _, _ := aptState(t, files)
	mgr, err := ForLookPath(s, noLookPath)
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	actions := mgr.Install([]string{"postgres"})
	if len(actions) != 2 || actions[0].Subject != pgAptPackage {
		t.Fatalf("actions = %+v, want cluster and install only", actions)
	}
}

func TestAptReposHaveRecipes(t *testing.T) {
	pkgs, err := spec.Packages()
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	for name, p := range pkgs {
		if p.AptRepo == "" {
			continue
		}
		if _, ok := aptRepos[p.AptRepo]; !ok {
			t.Errorf("package %q: apt_repo %q has no recipe", name, p.AptRepo)
		}
	}
}

func TestAptMissingSkipsNoRecipe(t *testing.T) {
	s, run, _ := aptState(t, nil)
	run.On("dpkg-query -W -f=${Status} git", runner.Result{Stdout: "install ok installed"}, nil)
	run.On("dpkg-query -W -f=${Status} gh", runner.Result{}, nil)
	mgr, err := ForLookPath(s, func(string) (string, error) { return "", errors.New("not found") })
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	// mailpit has no apt or script recipe, so it is skipped rather than reported missing.
	got, err := mgr.Missing(context.Background(), []string{"mailpit", "gh", "git"})
	if err != nil {
		t.Fatalf("Missing: %v", err)
	}
	if len(got) != 1 || got[0] != "gh" {
		t.Fatalf("Missing = %v, want [gh]", got)
	}
}

func TestAptInstallOrderAndScripts(t *testing.T) {
	s, _, _ := aptState(t, nil)
	mgr, err := ForLookPath(s, noLookPath)
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	actions := mgr.Install([]string{"bun", "gh", "node"})
	var subjects []string
	for _, a := range actions {
		subjects = append(subjects, a.Subject)
	}
	want := []string{"github-cli", "nodesource-24", "apt", "bun"}
	if strings.Join(subjects, ",") != strings.Join(want, ",") {
		t.Fatalf("subjects = %v, want %v", subjects, want)
	}
	if actions[len(actions)-1].Sudo {
		t.Fatalf("script action must not sudo")
	}
}

func TestAptBootstrapsCurlBeforeRepos(t *testing.T) {
	run := &runnertest.Fake{}
	run.On("dpkg-query -W -f=${Status} ca-certificates", runner.Result{Stdout: "install ok installed"}, nil)
	run.On("dpkg-query -W -f=${Status} curl", runner.Result{ExitCode: 1}, errors.New("exit status 1"))
	tty := &runnertest.Fake{}
	tty.On("sudo", runner.Result{}, nil)
	s := &engine.State{Platform: "linux", Run: run, Tty: tty, FS: mapFS{}}
	mgr, err := ForLookPath(s, noLookPath)
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	actions := mgr.Install([]string{"gh", "curl"})
	if len(actions) != 3 {
		t.Fatalf("actions = %d, want 3 (prerequisites, github-cli repo, install gh)", len(actions))
	}
	if actions[0].Summary != "Install apt prerequisites" || !actions[0].Sudo {
		t.Fatalf("first action = %+v", actions[0])
	}
	if actions[1].Subject != "github-cli" {
		t.Fatalf("second action subject = %q, want github-cli", actions[1].Subject)
	}
	if actions[2].Summary != "apt-get update, then install gh" {
		t.Fatalf("install summary = %q, want gh only (curl bootstrapped)", actions[2].Summary)
	}
	for _, a := range actions[:2] {
		if err := a.Do(context.Background()); err != nil {
			t.Fatalf("Do %q: %v", a.Summary, err)
		}
	}
	calls := tty.Calls()
	want := `apt-get update -o DPkg::Lock::Timeout=300 && DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get install -y -o DPkg::Lock::Timeout=300 -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" ca-certificates curl`
	if calls[0].Name != "sudo" || calls[0].Args[2] != want {
		t.Fatalf("prerequisites call = %+v, want script %q", calls[0], want)
	}
}

func TestMissingPropagatesQueryError(t *testing.T) {
	broken := errors.New("cannot run dpkg-query: executable file not found")
	t.Run("apt", func(t *testing.T) {
		s, run, _ := aptState(t, nil)
		run.On("dpkg-query -W -f=${Status} git", runner.Result{}, broken)
		mgr, err := ForLookPath(s, noLookPath)
		if err != nil {
			t.Fatalf("ForLookPath: %v", err)
		}
		if _, err := mgr.Missing(context.Background(), []string{"git"}); !errors.Is(err, broken) {
			t.Fatalf("Missing err = %v, want %v", err, broken)
		}
	})
	t.Run("brew", func(t *testing.T) {
		run := &runnertest.Fake{}
		run.On("brew list --versions git", runner.Result{}, broken)
		s := &engine.State{Platform: "darwin", Run: run, Tty: &runnertest.Fake{}, FS: mapFS{}}
		mgr, err := ForLookPath(s, noLookPath)
		if err != nil {
			t.Fatalf("ForLookPath: %v", err)
		}
		if _, err := mgr.Missing(context.Background(), []string{"git"}); !errors.Is(err, broken) {
			t.Fatalf("Missing err = %v, want %v", err, broken)
		}
	})
}

func TestQueryNonZeroExitMeansMissing(t *testing.T) {
	s, run, _ := aptState(t, nil)
	run.On("dpkg-query -W -f=${Status} gh", runner.Result{ExitCode: 1}, errors.New("exit status 1"))
	mgr, err := ForLookPath(s, noLookPath)
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	got, err := mgr.Missing(context.Background(), []string{"gh"})
	if err != nil || len(got) != 1 || got[0] != "gh" {
		t.Fatalf("Missing = %v, %v; want [gh], nil", got, err)
	}
}

func TestAptNonInteractiveOptions(t *testing.T) {
	run := &runnertest.Fake{}
	run.On("dpkg-query -W -f=${Status} ca-certificates", runner.Result{ExitCode: 1}, errors.New("exit status 1"))
	run.On("dpkg-query -W -f=${Status} curl", runner.Result{Stdout: "install ok installed"}, nil)
	tty := &runnertest.Fake{}
	tty.On("sudo", runner.Result{}, nil)
	s := &engine.State{Platform: "linux", Run: run, Tty: tty, FS: mapFS{}}
	mgr, err := ForLookPath(s, noLookPath)
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	actions := mgr.Install([]string{"gh"})
	if len(actions) != 3 {
		t.Fatalf("actions = %d, want 3 (bootstrap, repo, install)", len(actions))
	}
	for _, idx := range []int{0, 2} {
		if err := actions[idx].Do(context.Background()); err != nil {
			t.Fatalf("Do[%d]: %v", idx, err)
		}
	}
	calls := tty.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	required := []string{
		"DEBIAN_FRONTEND=noninteractive",
		"NEEDRESTART_MODE=a",
		"-o DPkg::Lock::Timeout=300",
		`-o Dpkg::Options::="--force-confdef"`,
		`-o Dpkg::Options::="--force-confold"`,
	}
	for i, c := range calls {
		script := c.Args[2]
		for _, opt := range required {
			if !strings.Contains(script, opt) {
				t.Errorf("call[%d] script %q missing %q", i, script, opt)
			}
		}
	}
}
