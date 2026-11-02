package domain

import (
	"testing"
	"time"
)

func TestStaticOnlyReposAreExempt(t *testing.T) {
	for _, path := range []string{
		"manovaspace/docs",
		"clients/kohan_kherad/website",
		"clients/jtash/frontend",
		"clients/manova/manova-frontend",
		"clients/meow/pishidolfini",
		"handbook",
	} {
		if !StaticOnlyRepo(path) {
			t.Errorf("%s is not static-only", path)
		}
	}
	if StaticOnlyRepo("orbit/orbit-cli") {
		t.Error("orbit/orbit-cli reported as static-only")
	}
}

func TestIsTestFile(t *testing.T) {
	cases := map[string]bool{
		"internal/service_test.go":          true,
		"src/components/button.test.tsx":    true,
		"src/lib/api.spec.ts":               true,
		"test/helpers.test.mjs":             true,
		"tests/test_workspace.py":           true,
		"tests/workspace_test.py":           true,
		"scripts/test-quality-local.sh":     true,
		"scripts/quality_test.sh":           true,
		"src/components/button.tsx":         false,
		"README.md":                         false,
		"testdata/fixtures/example.test.ts": true,
	}
	for path, want := range cases {
		if got := IsTestFile(path); got != want {
			t.Errorf("IsTestFile(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestGitHubSlug(t *testing.T) {
	cases := map[string]string{
		"git@github.com:manovaspace/orbit-cli.git":          "manovaspace/orbit-cli",
		"https://github.com/manovaspace/orbit-cli.git":      "manovaspace/orbit-cli",
		"https://github.com/manovaspace/orbit-cli":          "manovaspace/orbit-cli",
		"ssh://git@github.com/manovaspace/orbit-cli.git":    "manovaspace/orbit-cli",
		"git@forgejo.example.com:manovaspace/orbit-cli.git": "",
	}
	for remote, want := range cases {
		got, ok := GitHubSlug(remote)
		if got != want {
			t.Errorf("GitHubSlug(%q) = %q (ok %v), want %q", remote, got, ok, want)
		}
		if want != "" && !ok {
			t.Errorf("GitHubSlug(%q) reported no slug", remote)
		}
	}
}

func TestMergedPullRequestOverride(t *testing.T) {
	pr := MergedPullRequest{Comments: []string{"lgtm", " override: hotfix"}}
	if !pr.HasOverride() {
		t.Error("override comment not recognised")
	}
	if (MergedPullRequest{Comments: []string{"lgtm"}}).HasOverride() {
		t.Error("plain comment treated as an override")
	}
}

func TestMergedPullRequestFailingChecks(t *testing.T) {
	pr := MergedPullRequest{Checks: []MergedCheck{
		{Name: "build", Bucket: "pass"},
		{Name: "lint", Bucket: "fail"},
		{Name: "deploy", Bucket: "pending"},
		{Name: "docs", Bucket: "skipping"},
	}}
	got := pr.FailingChecks()
	if len(got) != 2 || got[0] != "deploy" || got[1] != "lint" {
		t.Errorf("FailingChecks() = %v, want [deploy lint]", got)
	}
	if (MergedPullRequest{Checks: []MergedCheck{{Name: "build", Bucket: "pass"}}}).FailingChecks() != nil {
		t.Error("passing checks reported as failing")
	}
	if !(MergedPullRequest{}).NoChecks() {
		t.Error("empty check list not reported as no checks")
	}
}

func TestParseEvalSummary(t *testing.T) {
	recall, mrr, ok := ParseEvalSummary("eval: recall@5=0.93 mrr=0.91 (14/15)\neval: OK")
	if !ok || recall != "0.93" || mrr != "0.91" {
		t.Errorf("ParseEvalSummary = %q, %q, %v", recall, mrr, ok)
	}
	if _, _, ok := ParseEvalSummary("eval: OK"); ok {
		t.Error("summary without numbers parsed")
	}
}

func TestMergedSince(t *testing.T) {
	since := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if got := MergedSearch(since); got != "merged:>=2026-11-01" {
		t.Errorf("MergedSearch = %q", got)
	}
}
