package manifest

import (
	"testing"

	"gopkg.in/yaml.v3"
)

const projectManifestYAML = `
version: "1"
workspace: "manova"
groups:
  falak:
    path: "clients/manova/falak"
    repo: "falak.git"
    remote: "forgejo"
  orbit:
    path: "orbit"
    repositories:
      - name: "orbit-cli"
        repo: "orbit-cli.git"
  handbook:
    path: "handbook"
    repo: "handbook.git"
    remote: "forgejo"
projects:
  falak:
    description: "Falak platform"
    repos: ["clients/manova/falak", "orbit/orbit-cli"]
  everything: ["*"]
  bare: ["handbook"]
`

func TestProjectReposWildcardAndUnknown(t *testing.T) {
	var m WorkspaceManifest
	if err := yaml.Unmarshal([]byte(projectManifestYAML), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	all := m.AllRepos()
	got, err := m.ProjectRepos("everything")
	if err != nil {
		t.Fatalf("everything: %v", err)
	}
	if len(got) != len(all) {
		t.Fatalf("everything: got %d repos, want %d", len(got), len(all))
	}

	wild, err := m.ProjectRepos("*")
	if err != nil || len(wild) != len(all) {
		t.Fatalf("name * should return all repos, got %d err %v", len(wild), err)
	}

	bare, err := m.ProjectRepos("bare")
	if err != nil || len(bare) != 1 || bare[0].Path != "handbook" {
		t.Fatalf("bare: got %+v err %v", bare, err)
	}

	falak, err := m.ProjectRepos("falak")
	if err != nil {
		t.Fatalf("falak: %v", err)
	}
	if len(falak) != 2 || falak[0].Path != "clients/manova/falak" || falak[1].Path != "orbit/orbit-cli" {
		t.Fatalf("falak: want sorted by path [clients/manova/falak orbit/orbit-cli], got %+v", falak)
	}

	if _, err := m.ProjectRepos("nope"); err == nil {
		t.Fatal("unknown project: want error")
	}
	var bad WorkspaceManifest
	if err := yaml.Unmarshal([]byte(projectManifestYAML), &bad); err != nil {
		t.Fatal(err)
	}
	bad.Projects["broken"] = ProjectConfig{Repos: []string{"no/such/path"}}
	if _, err := bad.ProjectRepos("broken"); err == nil {
		t.Fatal("unknown path: want error")
	}
}

func TestProjectConfigShorthandAndMapping(t *testing.T) {
	var m WorkspaceManifest
	if err := yaml.Unmarshal([]byte(projectManifestYAML), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	short := m.Projects["bare"]
	if short.Description != "" || len(short.Repos) != 1 || short.Repos[0] != "handbook" {
		t.Fatalf("sequence form: got %+v", short)
	}
	wild := m.Projects["everything"]
	if wild.Description != "" || len(wild.Repos) != 1 || wild.Repos[0] != "*" {
		t.Fatalf("sequence form with *: got %+v", wild)
	}

	mapped := m.Projects["falak"]
	if mapped.Description != "Falak platform" || len(mapped.Repos) != 2 {
		t.Fatalf("mapping form: got %+v", mapped)
	}
}

func TestProjectNamesSorted(t *testing.T) {
	var m WorkspaceManifest
	if err := yaml.Unmarshal([]byte(projectManifestYAML), &m); err != nil {
		t.Fatal(err)
	}
	names := m.ProjectNames()
	want := []string{"bare", "everything", "falak"}
	if len(names) != len(want) {
		t.Fatalf("names: got %v want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names: got %v want %v", names, want)
		}
	}
}
