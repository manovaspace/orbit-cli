package manifestrepos

import (
	"path/filepath"
	"testing"
)

func TestReposResolvesManifestTargets(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "workspace"))
	if err != nil {
		t.Fatal(err)
	}

	repos, err := New().Repos(root, filepath.Join(root, "workspace.yaml"))
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(repos) == 0 {
		t.Fatal("no repositories resolved")
	}
	byPath := map[string]bool{}
	for _, repo := range repos {
		byPath[repo.Path] = true
		if !filepath.IsAbs(repo.Dir) {
			t.Errorf("%s dir = %q, want absolute", repo.Path, repo.Dir)
		}
	}
	for _, want := range []string{"handbook", "manovaspace/ci", "orbit/go-drift", "clients/demo/shadow"} {
		if !byPath[want] {
			t.Errorf("missing %s in %v", want, byPath)
		}
	}
}

func TestReposRejectsMissingManifest(t *testing.T) {
	if _, err := New().Repos(t.TempDir(), ""); err == nil {
		t.Fatal("empty manifest path accepted")
	}
}
