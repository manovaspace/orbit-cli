package main

import (
	"context"
	"runtime"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

// machinePath locates machine.yaml; command tests replace it with a temp path.
var machinePath = spec.DefaultMachinePath

// newSetupState loads the machine file and the workspace manifest into the engine state that the
// setup parts read. The process runs the real commands, filesystem and TTY. nonInteractive is
// recorded for parts that would otherwise prompt.
func newSetupState(ctx context.Context, nonInteractive bool) (*engine.State, error) {
	m, _, err := spec.LoadMachine(machinePath())
	if err != nil {
		return nil, err
	}
	return stateFor(m, nonInteractive)
}

// stateFor builds the engine state for machine m and loads the workspace manifest when it exists.
// Every field the parts read is set: a part must never see a nil Env, FS or NewDatabases.
func stateFor(m spec.Machine, nonInteractive bool) (*engine.State, error) {
	s := &engine.State{
		Root:           m.Workspace,
		Machine:        m,
		Platform:       runtime.GOOS,
		Run:            runner.Exec{},
		Env:            runner.Exec{},
		Tty:            runner.Exec{},
		FS:             engine.OSFS{},
		NewDatabases:   map[string]bool{},
		NonInteractive: nonInteractive,
	}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}
