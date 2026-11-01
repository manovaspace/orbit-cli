// Package workspace wires the workspace use cases to the real git, filesystem
// and manifest adapters.
package workspace

import (
	"context"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/workspace/application"
	"github.com/manovaspace/orbit-cli/pkg/workspace/domain"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/gitcli"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/manifestfile"
	"github.com/manovaspace/orbit-cli/pkg/workspace/infrastructure/osfs"
)

// NewService builds the workspace use cases over the real adapters.
func NewService(out io.Writer, ui application.UI) *application.Service {
	return &application.Service{
		Git:      gitcli.New(runner.Exec{}),
		FS:       osfs.New(),
		Manifest: manifestfile.New(),
		Out:      out,
		UI:       ui,
	}
}

// CloneTargets clones targets with the real adapters. The onboarding flow uses
// it as its default cloner.
func CloneTargets(root string, targets []manifest.RepoTarget, concurrency int, callback func(domain.CloneResult)) []domain.CloneResult {
	svc := NewService(io.Discard, application.UI{})
	return svc.CloneTargets(context.Background(), root, targets, concurrency, callback)
}
