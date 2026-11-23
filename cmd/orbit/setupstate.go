package main

import (
	"context"
	"runtime"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

// newSetupState loads the machine file and the workspace manifest into the engine state that the
// setup parts read. The process runs the real commands, filesystem and TTY. nonInteractive is
// recorded for parts that would otherwise prompt.
func newSetupState(ctx context.Context, nonInteractive bool) (*engine.State, error) {
	m, _, err := spec.LoadMachine(spec.DefaultMachinePath())
	if err != nil {
		return nil, err
	}
	s := &engine.State{
		Root:           m.Workspace,
		Machine:        m,
		Platform:       runtime.GOOS,
		Run:            runner.Exec{},
		Env:            runner.Exec{},
		Tty:            runner.Exec{},
		FS:             engine.OSFS{},
		NonInteractive: nonInteractive,
	}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}
