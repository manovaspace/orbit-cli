package repos_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/repos"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
)

// stubManifest resolves a fixed target list.
type stubManifest struct {
	targets []manifest.RepoTarget
	err     error
}

func (s stubManifest) ResolveRepos(string) ([]manifest.RepoTarget, error) {
	return s.targets, s.err
}

func TestCloneCoreReportsAndReturnsRelativePaths(t *testing.T) {
	var started int
	var reported []workspacedomain.CloneResult
	var clonedTargets []manifest.RepoTarget

	r := &repos.Repos{
		Load: func(string) (repos.Manifest, error) {
			return stubManifest{targets: []manifest.RepoTarget{
				{Name: "orbit-cli", Path: "orbit/orbit-cli"},
				{Name: "handbook", Path: "handbook"},
			}}, nil
		},
		Clone: func(root string, targets []manifest.RepoTarget, concurrency int, callback func(workspacedomain.CloneResult)) []workspacedomain.CloneResult {
			clonedTargets = targets
			var results []workspacedomain.CloneResult
			for _, target := range targets {
				result := workspacedomain.CloneResult{Name: target.Name, Path: target.Path, Success: true}
				results = append(results, result)
				callback(result)
			}
			return results
		},
	}

	rels, err := r.CloneCore(context.Background(), "/workspace", "/workspace/workspace.yaml",
		func(count int) { started = count },
		func(result workspacedomain.CloneResult) { reported = append(reported, result) },
	)
	if err != nil {
		t.Fatalf("CloneCore failed: %v", err)
	}
	if started != 2 {
		t.Errorf("started = %d, want 2", started)
	}
	if len(clonedTargets) != 2 {
		t.Errorf("cloned %d targets, want 2", len(clonedTargets))
	}
	if len(reported) != 2 || reported[0].Name != "orbit-cli" {
		t.Errorf("unexpected reported results: %+v", reported)
	}
	if strings.Join(rels, ",") != "orbit/orbit-cli,handbook" {
		t.Errorf("rels = %v", rels)
	}
}

func TestCloneCoreSkipsWhenThereAreNoTargets(t *testing.T) {
	r := &repos.Repos{
		Load: func(string) (repos.Manifest, error) { return stubManifest{}, nil },
		Clone: func(string, []manifest.RepoTarget, int, func(workspacedomain.CloneResult)) []workspacedomain.CloneResult {
			t.Error("clone must not run without targets")
			return nil
		},
	}

	rels, err := r.CloneCore(context.Background(), "/workspace", "/manifest.yaml", nil, nil)
	if err != nil {
		t.Fatalf("CloneCore failed: %v", err)
	}
	if len(rels) != 0 {
		t.Errorf("rels = %v, want none", rels)
	}
}

func TestCloneCorePropagatesManifestErrors(t *testing.T) {
	r := &repos.Repos{
		Load: func(string) (repos.Manifest, error) { return nil, errors.New("manifest missing") },
	}

	if _, err := r.CloneCore(context.Background(), "/workspace", "/manifest.yaml", nil, nil); err == nil {
		t.Fatal("expected the manifest error")
	}
}

func TestPullAssetsIsOptional(t *testing.T) {
	r := &repos.Repos{}
	if err := r.PullAssets(context.Background(), "/workspace", []string{"orbit/orbit-cli"}); err != nil {
		t.Fatalf("PullAssets without an adapter must be a no-op: %v", err)
	}

	var pulled []string
	r.Assets = func(_ context.Context, _ string, rels []string) error {
		pulled = append(pulled, rels...)
		return nil
	}
	if err := r.PullAssets(context.Background(), "/workspace", []string{"handbook"}); err != nil {
		t.Fatalf("PullAssets failed: %v", err)
	}
	if len(pulled) != 1 || pulled[0] != "handbook" {
		t.Errorf("pulled = %v", pulled)
	}
}
