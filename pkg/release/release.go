// Package release wires the release use cases to the real workspace, git,
// GitHub CLI and local dev adapters.
package release

import (
	"io"

	"github.com/manovaspace/orbit-cli/pkg/release/application"
	"github.com/manovaspace/orbit-cli/pkg/release/infrastructure/ghcli"
	"github.com/manovaspace/orbit-cli/pkg/release/infrastructure/gitcli"
	"github.com/manovaspace/orbit-cli/pkg/release/infrastructure/localdev"
	"github.com/manovaspace/orbit-cli/pkg/release/infrastructure/manifestrepos"
	"github.com/manovaspace/orbit-cli/pkg/release/infrastructure/tester"
	"github.com/manovaspace/orbit-cli/pkg/release/infrastructure/workspacefs"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// NewService builds the release use cases over the real adapters. All external
// commands go through r.
func NewService(root, manifestPath string, r runner.Runner, out io.Writer, ui application.UI) *application.Service {
	files := workspacefs.New()
	return &application.Service{
		Workspace:    manifestrepos.New(),
		Files:        files,
		Git:          gitcli.New(r),
		Forge:        ghcli.New(r),
		Local:        localdev.New(root, r, files),
		Tester:       &tester.Tester{Files: files, Out: out},
		Out:          out,
		Root:         root,
		ManifestPath: manifestPath,
		UI:           ui,
	}
}
