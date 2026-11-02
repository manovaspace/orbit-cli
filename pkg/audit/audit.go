// Package audit wires the audit use cases to the real workspace, file, git-ref
// and registry adapters.
package audit

import (
	"github.com/manovaspace/orbit-cli/pkg/audit/application"
	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
	"github.com/manovaspace/orbit-cli/pkg/audit/infrastructure/gitrefs"
	"github.com/manovaspace/orbit-cli/pkg/audit/infrastructure/manifestrepos"
	"github.com/manovaspace/orbit-cli/pkg/audit/infrastructure/npmhttp"
	"github.com/manovaspace/orbit-cli/pkg/audit/infrastructure/workspacefs"
)

// DefaultRegistryURL is the public npm registry the audit may read.
const DefaultRegistryURL = "https://registry.npmjs.org"

// Finding is one audit problem.
type Finding = domain.Finding

// Report is one audit run.
type Report = domain.Report

// Result is one audit run plus its summary detail.
type Result = application.Result

// Severity levels re-exported for callers outside the audit package.
const (
	SeverityError = domain.SeverityError
	SeverityWarn  = domain.SeverityWarn
	SeverityInfo  = domain.SeverityInfo
)

// NewReport creates an empty audit report.
func NewReport() Report { return domain.NewReport() }

// NewService wires the audit service to the real adapters. The registry is
// only read when a caller asks for remote dependency checks.
func NewService(root, manifestPath string) *application.Service {
	return &application.Service{
		Root:         root,
		ManifestPath: manifestPath,
		Workspace:    manifestrepos.New(),
		Files:        workspacefs.New(),
		VCS:          gitrefs.New(),
		Registry:     npmhttp.New(DefaultRegistryURL),
	}
}
