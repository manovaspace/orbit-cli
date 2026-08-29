package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleManifestYAML = `
version: "1"
workspace: "manova"

remotes:
  forgejo: "ssh://git@git.dev.manova.space/manova"
  github_manovaspace: "git@github.com:manovaspace"

groups:
  handbook:
    path: "handbook"
    repo: "handbook.git"
    remote: "forgejo"
    required: true

  orbit:
    path: "orbit"
    description: "Orbit platform toolkit"
    defaults:
      remote: "forgejo"
      default_branch: "main"
    repositories:
      - name: "orbit-infra"
        required: true
      - name: "orbit-auth"
      - name: "orbit-render"
        default_branch: "develop"

  manovaspace:
    path: "manovaspace"
    description: "MIT open commons on GitHub"
    defaults:
      remote: "github_manovaspace"
    repositories:
      - name: "ts"
      - name: "design-system"
      - name: "docs"
        remote_url: "https://github.com/manovaspace/custom-docs.git"

  clients:
    path: "clients"
    description: "Client product clusters"
    clients:
      fryto:
        path: "clients/fryto"
        defaults:
          remote: "forgejo"
        repositories:
          - name: "fryto-infra"
          - name: "frynext"
          - name: "fryvoice"
      jtash:
        path: "clients/jtash"
        repositories:
          - name: "jtash-infra"
            remote: "forgejo"
          - name: "frontend"
            remote: "forgejo"
`

func TestParseAndResolveScope(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "workspace.yaml")
	if err := os.WriteFile(manifestPath, []byte(sampleManifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("unexpected error loading manifest: %v", err)
	}

	if m.Workspace != "manova" {
		t.Errorf("expected workspace 'manova', got %s", m.Workspace)
	}
	if m.Version != "1" {
		t.Errorf("expected version '1', got %s", m.Version)
	}

	// 1. Test core / default scope
	coreRepos := m.ResolveScope("core")
	if len(coreRepos) != 2 { // handbook + orbit-infra
		t.Fatalf("expected 2 core repos, got %d: %+v", len(coreRepos), coreRepos)
	}
	coreNames := map[string]bool{}
	for _, r := range coreRepos {
		coreNames[r.Name] = true
		if !r.Required {
			t.Errorf("expected repo %s to be required", r.Name)
		}
	}
	if !coreNames["handbook"] || !coreNames["orbit-infra"] {
		t.Errorf("expected handbook and orbit-infra in core scope, got %+v", coreRepos)
	}

	// Test empty scope defaults to core
	defaultRepos := m.ResolveScope("")
	if len(defaultRepos) != 2 {
		t.Errorf("expected 2 repos for empty scope (defaulting to core), got %d", len(defaultRepos))
	}

	// 2. Test orbit scope
	orbitRepos := m.ResolveScope("orbit")
	if len(orbitRepos) != 3 { // orbit-infra, orbit-auth, orbit-render
		t.Fatalf("expected 3 orbit repos, got %d", len(orbitRepos))
	}
	for _, r := range orbitRepos {
		if r.Scope != "orbit" {
			t.Errorf("expected scope 'orbit', got %s for repo %s", r.Scope, r.Name)
		}
		if r.Name == "orbit-render" && r.DefaultBranch != "develop" {
			t.Errorf("expected orbit-render branch 'develop', got %s", r.DefaultBranch)
		}
		if r.Name == "orbit-auth" {
			expectedURL := "ssh://git@git.dev.manova.space/manova/orbit-auth.git"
			if r.RemoteURL != expectedURL {
				t.Errorf("expected remote URL %s, got %s", expectedURL, r.RemoteURL)
			}
			if r.Path != "orbit/orbit-auth" {
				t.Errorf("expected path 'orbit/orbit-auth', got %s", r.Path)
			}
		}
	}

	// 3. Test manovaspace scope
	manovaRepos := m.ResolveScope("manovaspace")
	if len(manovaRepos) != 3 { // ts, design-system, docs
		t.Fatalf("expected 3 manovaspace repos, got %d", len(manovaRepos))
	}
	for _, r := range manovaRepos {
		if r.Scope != "manovaspace" {
			t.Errorf("expected scope 'manovaspace', got %s", r.Scope)
		}
		if r.Name == "ts" {
			expectedURL := "git@github.com:manovaspace/ts.git"
			if r.RemoteURL != expectedURL {
				t.Errorf("expected remote URL %s, got %s", expectedURL, r.RemoteURL)
			}
			if r.Path != "manovaspace/ts" {
				t.Errorf("expected path 'manovaspace/ts', got %s", r.Path)
			}
		}
		if r.Name == "docs" {
			expectedURL := "https://github.com/manovaspace/custom-docs.git"
			if r.RemoteURL != expectedURL {
				t.Errorf("expected custom remote URL %s, got %s", expectedURL, r.RemoteURL)
			}
		}
	}

	// 4. Test client scope: clients/fryto
	frytoRepos := m.ResolveScope("clients/fryto")
	if len(frytoRepos) != 3 {
		t.Fatalf("expected 3 fryto repos, got %d", len(frytoRepos))
	}
	frytoNames := map[string]bool{}
	for _, r := range frytoRepos {
		frytoNames[r.Name] = true
		if r.Scope != "clients/fryto" {
			t.Errorf("expected scope 'clients/fryto', got %s", r.Scope)
		}
		if r.Path != "clients/fryto/"+r.Name {
			t.Errorf("expected path clients/fryto/%s, got %s", r.Name, r.Path)
		}
		expectedURL := "ssh://git@git.dev.manova.space/manova/" + r.Name + ".git"
		if r.RemoteURL != expectedURL {
			t.Errorf("expected remote URL %s, got %s", expectedURL, r.RemoteURL)
		}
	}
	if !frytoNames["frynext"] || !frytoNames["fryto-infra"] || !frytoNames["fryvoice"] {
		t.Errorf("missing expected fryto repos: %+v", frytoRepos)
	}

	// 5. Test client scope shorthand: fryto
	frytoShortRepos := m.ResolveScope("fryto")
	if len(frytoShortRepos) != 3 {
		t.Fatalf("expected 3 fryto repos via shorthand, got %d", len(frytoShortRepos))
	}

	// 6. Test clients scope (all clients)
	allClientsRepos := m.ResolveScope("clients")
	if len(allClientsRepos) != 5 { // 3 fryto + 2 jtash
		t.Fatalf("expected 5 client repos, got %d", len(allClientsRepos))
	}

	// 7. Test all scope
	allRepos := m.ResolveScope("all")
	// 1 handbook + 3 orbit + 3 manovaspace + 3 fryto + 2 jtash = 12 repos
	if len(allRepos) != 12 {
		t.Fatalf("expected 12 total repos, got %d", len(allRepos))
	}

	// 8. Test specific repo scope
	singleRepo := m.ResolveScope("orbit-auth")
	if len(singleRepo) != 1 || singleRepo[0].Name != "orbit-auth" {
		t.Fatalf("expected 1 repo 'orbit-auth', got %+v", singleRepo)
	}

	// 9. Test unknown scope
	unknownRepos := m.ResolveScope("nonexistent-group")
	if len(unknownRepos) != 0 {
		t.Fatalf("expected 0 repos for unknown scope, got %d", len(unknownRepos))
	}
}

func TestRemoteURLResolution(t *testing.T) {
	manifestYAML := `
version: "1"
workspace: "manova"
remotes:
  forgejo: "ssh://git@git.dev.manova.space/manova/"
  github: "git@github.com:manovaspace"
  https_remote: "https://gitlab.com/manova"

groups:
  testgroup:
    path: "test"
    defaults:
      remote: "forgejo"
    repositories:
      - name: "repo-with-trailing-slash"
      - name: "repo-with-git-ext"
        repo: "custom-name.git"
      - name: "github-repo"
        remote: "github"
      - name: "https-repo"
        remote: "https_remote"
      - name: "raw-url-repo"
        remote_url: "git@custom.internal:foo/bar.git"
`
	m, err := Parse([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("unexpected error parsing manifest: %v", err)
	}

	repos := m.ResolveScope("testgroup")
	if len(repos) != 5 {
		t.Fatalf("expected 5 repos, got %d", len(repos))
	}

	expectedURLs := map[string]string{
		"repo-with-trailing-slash": "ssh://git@git.dev.manova.space/manova/repo-with-trailing-slash.git",
		"repo-with-git-ext":        "ssh://git@git.dev.manova.space/manova/custom-name.git",
		"github-repo":              "git@github.com:manovaspace/github-repo.git",
		"https-repo":               "https://gitlab.com/manova/https-repo.git",
		"raw-url-repo":             "git@custom.internal:foo/bar.git",
	}

	for _, r := range repos {
		expected, ok := expectedURLs[r.Name]
		if !ok {
			t.Errorf("unexpected repo name: %s", r.Name)
			continue
		}
		if r.RemoteURL != expected {
			t.Errorf("repo %s: expected URL %q, got %q", r.Name, expected, r.RemoteURL)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	// 1. Missing file
	_, err := Load("/path/to/nonexistent/workspace.yaml")
	if err == nil {
		t.Error("expected error loading nonexistent file, got nil")
	}

	// 2. Malformed YAML
	tmpDir := t.TempDir()
	malformedPath := filepath.Join(tmpDir, "malformed.yaml")
	if err := os.WriteFile(malformedPath, []byte("invalid: yaml: : : content [}"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = Load(malformedPath)
	if err == nil {
		t.Error("expected error loading malformed YAML, got nil")
	}

	// 3. Empty file
	emptyPath := filepath.Join(tmpDir, "empty.yaml")
	if err := os.WriteFile(emptyPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = Load(emptyPath)
	if err == nil {
		t.Error("expected error loading empty manifest file, got nil")
	}
}

func TestResolveRepos(t *testing.T) {
	m, err := Parse([]byte(sampleManifestYAML))
	if err != nil {
		t.Fatalf("unexpected error parsing manifest: %v", err)
	}

	// Valid scope
	targets, err := m.ResolveRepos("orbit")
	if err != nil {
		t.Fatalf("expected no error for orbit scope, got: %v", err)
	}
	if len(targets) != 3 {
		t.Errorf("expected 3 repos, got %d", len(targets))
	}

	// Invalid scope
	_, err = m.ResolveRepos("invalid-scope-12345")
	if err == nil {
		t.Error("expected error for invalid scope, got nil")
	}
}

func TestNilAndEmptyManifest(t *testing.T) {
	var m *WorkspaceManifest
	if targets := m.AllRepos(); len(targets) != 0 {
		t.Errorf("expected 0 targets for nil manifest, got %d", len(targets))
	}
	if targets := m.ResolveScope("all"); len(targets) != 0 {
		t.Errorf("expected 0 targets for nil manifest scope, got %d", len(targets))
	}

	emptyManifest := &WorkspaceManifest{}
	if targets := emptyManifest.AllRepos(); len(targets) != 0 {
		t.Errorf("expected 0 targets for empty manifest, got %d", len(targets))
	}
}

func TestCustomPathsAndDefaultsInheritance(t *testing.T) {
	manifestYAML := `
version: "1"
workspace: "manova"
remotes:
  github_colon: "git@github.com:"

groups:
  single_repo:
    remote: "ssh://git@git.dev.manova.space/custom/direct"
    default_branch: "release"

  client_group:
    path: "clients"
    defaults:
      remote: "github_colon"
      default_branch: "develop"
    clients:
      demo:
        defaults:
          default_branch: "staging"
        repositories:
          - name: "custom-path-repo"
            path: "custom/path"
            remote: "git@custom-direct.com:user"
          - name: "inherited-repo"
`
	m, err := Parse([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("unexpected error parsing manifest: %v", err)
	}

	// Single repo without path (defaults to name)
	single := m.ResolveScope("single_repo")
	if len(single) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(single))
	}
	if single[0].Path != "single_repo" {
		t.Errorf("expected path 'single_repo', got %s", single[0].Path)
	}
	if single[0].DefaultBranch != "release" {
		t.Errorf("expected default branch 'release', got %s", single[0].DefaultBranch)
	}
	if single[0].RemoteURL != "ssh://git@git.dev.manova.space/custom/direct/single_repo.git" {
		t.Errorf("expected direct ssh url, got %s", single[0].RemoteURL)
	}

	// Client repo with custom path and direct git@ remote
	clientRepos := m.ResolveScope("clients/demo")
	if len(clientRepos) != 2 {
		t.Fatalf("expected 2 client repos, got %d", len(clientRepos))
	}
	if clientRepos[0].Path != "clients/demo/custom/path" {
		t.Errorf("expected path 'clients/demo/custom/path', got %s", clientRepos[0].Path)
	}
	if clientRepos[0].RemoteURL != "git@custom-direct.com:user/custom-path-repo.git" {
		t.Errorf("expected custom remote URL, got %s", clientRepos[0].RemoteURL)
	}
	if clientRepos[0].DefaultBranch != "staging" {
		t.Errorf("expected inherited branch 'staging', got %s", clientRepos[0].DefaultBranch)
	}

	// Inherited colon baseURL: git@github.com: + inherited-repo.git
	if clientRepos[1].RemoteURL != "git@github.com:inherited-repo.git" {
		t.Errorf("expected git@github.com:inherited-repo.git, got %s", clientRepos[1].RemoteURL)
	}
}

