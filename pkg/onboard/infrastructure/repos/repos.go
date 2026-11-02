// Package repos clones the core manifest repositories and pulls their assets.
package repos

import (
	"context"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/workspace"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
)

// Cloner clones the resolved targets.
type Repos struct {
	// Clone defaults to workspace.CloneTargets.
	Clone func(root string, targets []manifest.RepoTarget, concurrency int, callback func(workspacedomain.CloneResult)) []workspacedomain.CloneResult
	// Assets pulls the gitignored assets of the cloned repositories. A nil
	// function skips the pull.
	Assets func(ctx context.Context, root string, rels []string) error
	// Load defaults to manifest.Load.
	Load func(path string) (Manifest, error)
}

// Manifest is the part of the workspace manifest the flow needs.
type Manifest interface {
	ResolveRepos(scope string) ([]manifest.RepoTarget, error)
}

// New builds Repos over the real workspace adapters.
func New() *Repos {
	return &Repos{}
}

// CloneCore resolves the core repositories and clones them.
func (r *Repos) CloneCore(ctx context.Context, root, manifestPath string, onStart func(count int), onResult func(workspacedomain.CloneResult)) ([]string, error) {
	m, err := r.load(manifestPath)
	if err != nil {
		return nil, err
	}

	targets, err := m.ResolveRepos("core")
	if err != nil || len(targets) == 0 {
		return nil, err
	}
	if onStart != nil {
		onStart(len(targets))
	}

	r.clone()(root, targets, 4, onResult)

	rels := make([]string, 0, len(targets))
	for _, t := range targets {
		rels = append(rels, t.Path)
	}
	return rels, nil
}

// PullAssets pulls the assets of rels when an assets adapter is configured.
func (r *Repos) PullAssets(ctx context.Context, root string, rels []string) error {
	if r.Assets == nil {
		return nil
	}
	return r.Assets(ctx, root, rels)
}

func (r *Repos) load(path string) (Manifest, error) {
	if r.Load != nil {
		return r.Load(path)
	}
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *Repos) clone() func(string, []manifest.RepoTarget, int, func(workspacedomain.CloneResult)) []workspacedomain.CloneResult {
	if r.Clone != nil {
		return r.Clone
	}
	return workspace.CloneTargets
}
