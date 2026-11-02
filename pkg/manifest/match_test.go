package manifest

import (
	"reflect"
	"testing"
)

const matchManifestYAML = `
version: "1"
workspace: "test"
groups:
  alpha:
    repositories:
      - name: "orbit-auth"
      - name: "orbit-infra"
      - name: "fryto-ops"
      - name: "fryto-web"
      - name: "fryvoice"
      - name: "a.b"
      - name: "axb"
      - name: "Some-Thing"
`

func repoNames(targets []RepoTarget) []string {
	names := make([]string, len(targets))
	for i, target := range targets {
		names[i] = target.Name
	}
	return names
}

func TestMatchRepos(t *testing.T) {
	m, err := Parse([]byte(matchManifestYAML))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{name: "exact", pattern: "fryto-ops", want: []string{"fryto-ops"}},
		{name: "prefix", pattern: "fryto*", want: []string{"fryto-ops", "fryto-web"}},
		{name: "suffix", pattern: "*ops", want: []string{"fryto-ops"}},
		{name: "infix", pattern: "fry*voice", want: []string{"fryvoice"}},
		{name: "wildcard only", pattern: "*", want: []string{"orbit-auth", "orbit-infra", "fryto-ops", "fryto-web", "fryvoice", "a.b", "axb", "Some-Thing"}},
		{name: "case insensitive", pattern: "ORBIT-*", want: []string{"orbit-auth", "orbit-infra"}},
		{name: "no match", pattern: "nope", want: []string{}},
		{name: "regex dot stays literal", pattern: "a.b", want: []string{"a.b"}},
		{name: "regex metacharacters stay literal", pattern: "a+b", want: []string{}},
		{name: "scopes are not names", pattern: "alpha", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoNames(m.MatchRepos(tt.pattern))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MatchRepos(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestMatchReposNilAndEmptyManifest(t *testing.T) {
	var m *WorkspaceManifest
	if got := m.MatchRepos("*"); len(got) != 0 {
		t.Errorf("nil manifest matched %d repositories, want 0", len(got))
	}

	empty := &WorkspaceManifest{}
	if got := empty.MatchRepos("*"); len(got) != 0 {
		t.Errorf("empty manifest matched %d repositories, want 0", len(got))
	}
}
