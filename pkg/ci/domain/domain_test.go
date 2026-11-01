package domain

import (
	"errors"
	"testing"
)

func TestParseRunnerMode(t *testing.T) {
	for _, value := range []string{"vps", "github", "status"} {
		if _, err := ParseRunnerMode(value); err != nil {
			t.Errorf("ParseRunnerMode(%q): %v", value, err)
		}
	}
	if _, err := ParseRunnerMode("carrier-pigeon"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestGitHubSlug(t *testing.T) {
	tests := map[string]string{
		"git@github.com:manovaspace/orbit-tg.git":          "manovaspace/orbit-tg",
		"https://github.com/manovaspace/orbit-tg":          "manovaspace/orbit-tg",
		"ssh://git@github.com/manovaspace/orbit-tg.git":    "manovaspace/orbit-tg",
		"git@git.internal.manova.space:manova/toolkit.git": "",
	}
	for remote, want := range tests {
		slug, ok := GitHubSlug(remote)
		if want == "" {
			if ok {
				t.Errorf("GitHubSlug(%q) accepted a non-GitHub remote", remote)
			}
			continue
		}
		if !ok || slug != want {
			t.Errorf("GitHubSlug(%q) = %q, %v, want %q", remote, slug, ok, want)
		}
	}
}

func TestParseSource(t *testing.T) {
	source, err := ParseSource("# comment\n\nNPM_TOKEN=abc\nDEPLOY_SSH_KEY=\"key\"\n", "secrets.env")
	if err != nil {
		t.Fatal(err)
	}
	if source["NPM_TOKEN"] != "abc" || source["DEPLOY_SSH_KEY"] != `"key"` {
		t.Fatalf("source = %+v", source)
	}
	if _, err := ParseSource("not-a-pair\n", "secrets.env"); err == nil {
		t.Fatal("invalid line accepted")
	}
}

func TestIsMissingVariable(t *testing.T) {
	if !IsMissingVariable(errors.New("variable not found")) {
		t.Error("not found not recognised")
	}
	if !IsMissingVariable(errors.New("failed to delete variable: HTTP 404")) {
		t.Error("HTTP 404 not recognised")
	}
	if IsMissingVariable(errors.New("permission denied")) {
		t.Error("permission denied treated as missing")
	}
}
