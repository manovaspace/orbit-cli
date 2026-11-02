package domain

import "testing"

func TestParseBunLockAll(t *testing.T) {
	text := `{
  "packages": {
    "lodash": ["lodash@4.17.20", "", {}],
    "@scope/pkg": ["@scope/pkg@1.2.3", "", {}],
    "legacy/@scope/pkg": ["@scope/pkg@1.0.0", "", {}],
  }
}`
	got := ParseBunLockAll(text)
	if len(got) != 3 {
		t.Fatalf("resolved = %d, want 3: %+v", len(got), got)
	}
	byName := map[string]string{}
	for _, entry := range got {
		byName[entry.Key] = entry.Version
	}
	if byName["lodash"] != "4.17.20" {
		t.Errorf("lodash version = %q", byName["lodash"])
	}
	if byName["@scope/pkg"] != "1.2.3" {
		t.Errorf("@scope/pkg version = %q", byName["@scope/pkg"])
	}
	if byName["legacy/@scope/pkg"] != "1.0.0" {
		t.Errorf("nested @scope/pkg version = %q", byName["legacy/@scope/pkg"])
	}
}

func TestParseGoSum(t *testing.T) {
	text := `github.com/example/vulnerable v1.2.3 h1:abc=
github.com/example/vulnerable v1.2.3/go.mod h1:def=
golang.org/x/text v0.3.0 h1:ghi=
golang.org/x/text v0.3.0/go.mod h1:jkl=
`
	got := ParseGoSum(text)
	if len(got) != 2 {
		t.Fatalf("modules = %d, want 2: %+v", len(got), got)
	}
	if got[0].Name != "github.com/example/vulnerable" || got[0].Version != "v1.2.3" {
		t.Errorf("first module = %+v", got[0])
	}
	if got[1].Name != "golang.org/x/text" || got[1].Version != "v0.3.0" {
		t.Errorf("second module = %+v", got[1])
	}
}
