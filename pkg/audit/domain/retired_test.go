package domain

import "testing"

func TestActiveDocPath(t *testing.T) {
	cases := map[string]bool{
		"handbook/docs/orbit/guides/development-workflow.md":        true,
		"handbook/agents/rules/rtk.mdc":                             false,
		"handbook/agents/skills/manova-delivery.md":                 true,
		"handbook/docs/superpowers/plans/2026-11-02-plan.md":        false,
		"handbook/docs/superpowers/specs/2026-11-02-spec.md":        false,
		"handbook/docs/archive/root-docs-2026-10/README.md":         false,
		"handbook/docs/orbit/migrations/2026-10-10-keycloak.md":     false,
		"handbook/docs/orbit/reviews/2026-10-manova/audit.md":       false,
		"handbook/docs/orbit/history/2026-01-handbook.md":           false,
		"handbook/agents/CHANGELOG.md":                              false,
		"handbook/docs/orbit/guides/CHANGELOG.md":                   false,
		"handbook/docs/orbit/guides/diagram.mdx":                    true,
		"handbook/docs/clients/demo/fixtures/seed.md":               false,
		"handbook/docs/orbit/guides/testdata/example.md":            false,
		"handbook/agents/scripts/fixtures/notes.md":                 false,
		"clients/demo/README.md":                                    false,
		"handbook/docs/orbit/guides/development-workflow.md.backup": false,
	}
	for path, want := range cases {
		if got := ActiveDocPath(path); got != want {
			t.Errorf("ActiveDocPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRetiredHitsFlagsCursorPathInGuide(t *testing.T) {
	hits := RetiredHits("handbook/docs/orbit/guides/development-workflow.md",
		[]byte("first line\nSee handbook/cursor/workspace for routing.\n"))

	if len(hits) != 1 {
		t.Fatalf("hits = %+v, want one", hits)
	}
	hit := hits[0]
	if hit.File != "handbook/docs/orbit/guides/development-workflow.md" || hit.Line != 2 {
		t.Errorf("hit = %+v, want the file and line 2", hit)
	}
	if hit.Term != "handbook/cursor" {
		t.Errorf("term = %q, want handbook/cursor", hit.Term)
	}
	if hit.Fix == "" {
		t.Error("hit carries no fix hint")
	}
}

func TestRetiredHitsAllowsRetiredNamesInADRs(t *testing.T) {
	content := []byte("Forgejo was retired in favour of GitHub.\nThe handbook/cursor symlink is retired.\n")
	for _, path := range []string{
		"handbook/docs/orbit/decisions/008-developer-workspace-shell.md",
		"handbook/docs/orbit/decisions/020-manova-handbook.md",
		"handbook/docs/orbit/decisions/031-agent-configuration-standard.md",
		"handbook/docs/orbit/decisions/036-github-sole-forge.md",
	} {
		if hits := RetiredHits(path, content); len(hits) != 0 {
			t.Errorf("RetiredHits(%q) = %+v, want none inside an ADR", path, hits)
		}
	}
}

func TestRetiredHitsFlagsForgejoInGuide(t *testing.T) {
	hits := RetiredHits("handbook/docs/orbit/architecture/module-catalog.md",
		[]byte("Hosted on Forgejo until 2026.\n"))

	if len(hits) != 1 {
		t.Fatalf("hits = %+v, want the Forgejo mention", hits)
	}
	if hits[0].Expected != "Forgejo only in an ADR under handbook/docs/orbit/decisions/" {
		t.Errorf("expected = %q, want the ADR-only hint", hits[0].Expected)
	}
}

func TestRetiredHitsIgnoresInactiveDocs(t *testing.T) {
	content := []byte("handbook/cursor and Forgejo\n")
	for _, path := range []string{
		"handbook/docs/superpowers/plans/2026-11-02-plan.md",
		"handbook/docs/archive/root-docs-2026-10/README.md",
		"handbook/docs/clients/demo/fixtures/seed.md",
		"handbook/agents/scripts/fixtures/notes.md",
		"handbook/docs/orbit/guides/testdata/example.md",
	} {
		if hits := RetiredHits(path, content); len(hits) != 0 {
			t.Errorf("RetiredHits(%q) = %+v, want none", path, hits)
		}
	}
}
