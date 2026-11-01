// Package execprobe implements the diagnostics probe port over runner.Runner.
package execprobe

import (
	"context"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor/application"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Probe runs external diagnostic programs through a runner.Runner.
type Probe struct {
	Runner runner.Runner
}

// New builds a Probe over r.
func New(r runner.Runner) *Probe { return &Probe{Runner: r} }

// Run executes one probe with the caller's timeout and returns the combined
// stdout/stderr the version parsers read.
func (p *Probe) Run(ctx context.Context, timeout time.Duration, name string, args ...string) (application.ProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := p.Runner.Run(ctx, "", name, args...)
	return application.ProbeResult{Output: res.Stdout + res.Stderr, ExitCode: res.ExitCode}, err
}
