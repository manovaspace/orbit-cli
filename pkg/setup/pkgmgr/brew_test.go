package pkgmgr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

func TestBrewMissingUsesListVersions(t *testing.T) {
	run := &runnertest.Fake{}
	run.On("brew list --versions git", runner.Result{Stdout: "git 2.47.0\n"}, nil)
	run.On("brew list --versions gh", runner.Result{ExitCode: 1}, errors.New("exit status 1"))
	s := &engine.State{Platform: "darwin", Run: run, Tty: &runnertest.Fake{}, FS: mapFS{}}
	mgr, err := ForLookPath(s, func(string) (string, error) { return "/x/bun", nil })
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	got, err := mgr.Missing(context.Background(), []string{"gh", "git", "bun"})
	if err != nil {
		t.Fatalf("Missing: %v", err)
	}
	if len(got) != 1 || got[0] != "gh" {
		t.Fatalf("Missing = %v, want [gh]", got)
	}
}

func TestBrewInstallOneAction(t *testing.T) {
	tty := &runnertest.Fake{}
	tty.On("brew", runner.Result{}, nil)
	tty.On("bash", runner.Result{}, nil)
	s := &engine.State{Platform: "darwin", Run: &runnertest.Fake{}, Tty: tty, FS: mapFS{}}
	mgr, err := ForLookPath(s, noLookPath)
	if err != nil {
		t.Fatalf("ForLookPath: %v", err)
	}
	actions := mgr.Install([]string{"git", "gh", "curl", "bun"})
	if len(actions) != 2 {
		t.Fatalf("actions = %d, want 2 (brew install, bun script)", len(actions))
	}
	if actions[0].Summary != "brew install git gh curl" || actions[0].Sudo {
		t.Fatalf("brew action = %+v", actions[0])
	}
	if err := actions[0].Do(context.Background()); err != nil {
		t.Fatalf("brew Do: %v", err)
	}
	if err := actions[1].Do(context.Background()); err != nil {
		t.Fatalf("bun Do: %v", err)
	}
	calls := tty.Calls()
	if len(calls) != 2 || !calls[0].Interactive || strings.Join(calls[0].Args, " ") != "install git gh curl" {
		t.Fatalf("brew call = %+v", calls)
	}
	if got := strings.Join(calls[1].Args, " "); got != "-c curl -fsSL https://bun.sh/install | bash" {
		t.Fatalf("bun call args = %q", got)
	}
}
