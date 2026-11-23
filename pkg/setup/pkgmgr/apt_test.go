package pkgmgr

import (
	"context"
	"errors"
	"io/fs"
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
	wantInstall := "apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql-18"
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
	if actions[3].Sudo {
		t.Fatalf("script action must not sudo")
	}
}
