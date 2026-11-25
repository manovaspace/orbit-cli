package spec

import (
	"strings"
	"testing"
)

func TestParseRootSpecRejectsEscapes(t *testing.T) {
	good := []byte(`
version: 1
links:
  AGENTS.md: handbook/agents/AGENTS.md
files:
  .gitignore: templates/gitignore
generated:
  go.work: go-work
generators: [go-work]
`)
	rs, err := ParseRootSpec(good)
	if err != nil {
		t.Fatalf("valid root: %v", err)
	}
	if rs.Links["AGENTS.md"] != "handbook/agents/AGENTS.md" || rs.Generated["go.work"] != "go-work" {
		t.Fatalf("fields not loaded: %+v", rs)
	}

	cases := map[string]string{
		"link source ..":   "links:\n  a: ../x\n",
		"link source abs":  "links:\n  a: /etc/x\n",
		"link key ..":      "links:\n  ../x: y\n",
		"file key abs":     "files:\n  /etc/x: y\n",
		"generated key ..": "generated:\n  ../go.work: go-work\n",
		"generated kind":   "generated:\n  go.work: unknown\n",
	}
	for name, body := range cases {
		if _, err := ParseRootSpec([]byte(body)); err == nil {
			t.Fatalf("%s: want error", name)
		}
	}
	content, err := ParseRootSpec([]byte("version: 1\nfiles:\n  CLAUDE.md: \"see ../docs and /etc/x\\n\"\n"))
	if err != nil {
		t.Fatalf("file content with / and ..: %v", err)
	}
	if content.Files["CLAUDE.md"] != "see ../docs and /etc/x\n" {
		t.Fatalf("file content = %q", content.Files["CLAUDE.md"])
	}
	if _, err := ParseRootSpec([]byte("links: [\n")); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("malformed yaml: want error, got %v", err)
	}
}
