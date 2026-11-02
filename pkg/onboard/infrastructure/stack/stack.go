// Package stack starts the local development stack with docker compose.
package stack

import (
	"context"

	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Stack launches the orbit-infra compose project.
type Stack struct {
	Runner runner.Runner
}

// New builds a Stack over r.
func New(r runner.Runner) *Stack {
	return &Stack{Runner: r}
}

// Start runs `docker compose up -d` in infraDir. The process streams through the
// caller's terminal via the runner's environment.
func (s *Stack) Start(ctx context.Context, infraDir string) error {
	if s.Runner == nil {
		return nil
	}
	_, err := s.Runner.Run(ctx, infraDir, "docker", "compose", "up", "-d")
	return err
}
