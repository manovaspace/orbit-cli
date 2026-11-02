// Package manifestrepos implements the release workspace port with pkg/manifest.
package manifestrepos

import (
	"fmt"
	"path/filepath"
	"strings"

	cidomain "github.com/manovaspace/orbit-cli/pkg/ci/domain"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/release/application"
)

// Repos resolves the repositories declared by workspace.yaml.
type Repos struct{}

// New builds the adapter.
func New() *Repos { return &Repos{} }

// Repos returns every manifest repository, resolved against the workspace root.
func (r *Repos) Repos(root, manifestPath string) ([]application.Repo, error) {
	if manifestPath == "" {
		return nil, fmt.Errorf("no workspace manifest path")
	}
	loaded, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	targets := loaded.AllRepos()
	out := make([]application.Repo, 0, len(targets))
	for _, target := range targets {
		dir := target.Path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		slug, _ := cidomain.GitHubSlug(target.RemoteURL)
		out = append(out, application.Repo{
			Name:          target.Name,
			Path:          strings.ReplaceAll(filepath.ToSlash(target.Path), "\\", "/"),
			Dir:           filepath.Clean(dir),
			DefaultBranch: target.DefaultBranch,
			Scope:         target.Scope,
			Slug:          slug,
		})
	}
	return out, nil
}
