// Package doctor wires the layered diagnostics service for callers outside the
// doctor command group, such as onboarding and the asset diagnostics.
package doctor

import (
	"context"
	"io"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/doctor/application"
	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	"github.com/manovaspace/orbit-cli/pkg/doctor/infrastructure/execprobe"
	"github.com/manovaspace/orbit-cli/pkg/doctor/infrastructure/machine"
	"github.com/manovaspace/orbit-cli/pkg/doctor/infrastructure/recipes"
	"github.com/manovaspace/orbit-cli/pkg/ports/infrastructure/netprobe"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/manifestfile"
)

// DiagnosticOptions selects explicit remote probes and host-key mutation.
type DiagnosticOptions struct {
	Remote         bool
	AcceptHostKeys bool
}

// DoctorReport aggregates diagnostic results.
type DoctorReport = domain.DoctorReport

// Report is an alias for DoctorReport.
type Report = domain.DoctorReport

// DiagnosticResult is one check result.
type DiagnosticResult = domain.DiagnosticResult

// CheckResult is an alias for DiagnosticResult.
type CheckResult = domain.DiagnosticResult

// CheckStatus is the result status.
type CheckStatus = domain.CheckStatus

// Status constants re-exported for onboarding.
const (
	StatusUnverified = domain.StatusUnverified
	StatusOK         = domain.StatusOK
	StatusPass       = domain.StatusPass
	StatusWarning    = domain.StatusWarning
	StatusWarn       = domain.StatusWarn
	StatusError      = domain.StatusError
)

// Category constants re-exported for onboarding.
const (
	CategoryCore      = domain.CategoryCore
	CategoryToolchain = domain.CategoryToolchain
	CategoryRuntime   = domain.CategoryRuntime
	CategoryContainer = domain.CategoryContainer
	CategoryNetwork   = domain.CategoryNetwork
	CategorySecurity  = domain.CategorySecurity
	CategoryAuth      = domain.CategoryAuth
	CategoryPorts     = domain.CategoryPorts
	CategoryOptional  = domain.CategoryOptional
	CategoryDevTools  = domain.CategoryDevTools
	CategoryWorkspace = domain.CategoryWorkspace
)

// NewReport creates an empty report.
func NewReport() *DoctorReport { return domain.NewReport() }

// probeEnvironment keeps toolchain probes local and offline, matching the
// historical doctor behavior.
var probeEnvironment = []string{"GOTOOLCHAIN=local", "GOTELEMETRY=off", "GOPROXY=off", "GOSUMDB=off"}

// DefaultRunner is the runner doctor probes use when the caller supplies none.
func DefaultRunner() runner.Runner {
	return runner.Exec{Env: append(os.Environ(), probeEnvironment...)}
}

// NewService wires the diagnostics service to the real adapters.
func NewService(r runner.Runner, out io.Writer, ui application.UI) *application.Service {
	if r == nil {
		r = DefaultRunner()
	}
	return &application.Service{
		Probe:    execprobe.New(r),
		Host:     machine.New(),
		Ports:    netprobe.New(),
		Manifest: manifestfile.New(),
		Healers:  recipes.NewDefaultRegistry(r),
		Out:      out,
		UI:       ui,
	}
}

// RunDiagnostics performs local inspection without network probes or writes.
func RunDiagnostics() *DoctorReport {
	return RunDiagnosticsWithOptions(DiagnosticOptions{})
}

// RunDiagnosticsWithOptions executes the selected suite without assets or
// workspace checks, matching onboarding's use.
func RunDiagnosticsWithOptions(opts DiagnosticOptions) *DoctorReport {
	service := NewService(nil, io.Discard, application.UI{})
	return service.Diagnose(context.Background(), application.Options{
		Remote:         opts.Remote,
		AcceptHostKeys: opts.AcceptHostKeys,
	}).Report
}
