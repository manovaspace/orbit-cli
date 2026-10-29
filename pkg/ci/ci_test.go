package ci

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

type call struct {
	stdin string
	argv  string
}

func recorder(result map[string]string, failure map[string]error) (*[]call, Exec, ExecStdin) {
	calls := &[]call{}
	invoke := func(stdin, name string, args ...string) (string, error) {
		argv := name + " " + strings.Join(args, " ")
		*calls = append(*calls, call{stdin: stdin, argv: argv})
		if err, ok := failure[argv]; ok {
			return "", err
		}
		return result[argv], nil
	}
	return calls, func(name string, args ...string) (string, error) {
		return invoke("", name, args...)
	}, invoke
}

func TestSetRunnerVPSOrder(t *testing.T) {
	calls, run, _ := recorder(nil, nil)

	if err := SetRunner("vps", []string{"manovaspace/a", "manovaspace/b"}, run); err != nil {
		t.Fatalf("SetRunner: %v", err)
	}

	want := []string{
		"sudo systemctl start gh-ci",
		"gh variable set CI_RUNNER --body vps -R manovaspace/a",
		"gh variable set CI_RUNNER --body vps -R manovaspace/b",
	}
	if len(*calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(*calls), len(want), *calls)
	}
	for i := range want {
		if (*calls)[i].argv != want[i] {
			t.Errorf("call %d = %q, want %q", i, (*calls)[i].argv, want[i])
		}
	}
}

func TestSetRunnerGithubIgnoresMissingVar(t *testing.T) {
	calls, run, _ := recorder(nil, map[string]error{
		"gh variable delete CI_RUNNER -R manovaspace/b": errors.New("variable not found"),
	})

	if err := SetRunner("github", []string{"manovaspace/a", "manovaspace/b"}, run); err != nil {
		t.Fatalf("SetRunner: %v", err)
	}
	if last := (*calls)[len(*calls)-1].argv; last != "sudo systemctl stop gh-ci" {
		t.Errorf("last call = %q, want the runner stop", last)
	}
}

func TestSetRunnerRejectsUnknownMode(t *testing.T) {
	_, run, _ := recorder(nil, nil)
	if err := SetRunner("carrier-pigeon", nil, run); err == nil {
		t.Fatal("unknown mode must fail")
	}
}

func TestPrivateReposFiltersPublic(t *testing.T) {
	_, run, _ := recorder(map[string]string{
		"gh repo view manovaspace/private --json visibility --jq .visibility": "PRIVATE",
		"gh repo view manovaspace/public --json visibility --jq .visibility":  "PUBLIC",
	}, nil)

	repos := []manifest.RepoTarget{
		{Name: "private", RemoteURL: "git@github.com:manovaspace/private.git"},
		{Name: "public", RemoteURL: "https://github.com/manovaspace/public"},
		{Name: "self-hosted", RemoteURL: "git@git.internal.manova.space:manova/toolkit.git"},
	}
	private, err := PrivateRepos(repos, run)
	if err != nil {
		t.Fatalf("PrivateRepos: %v", err)
	}
	if len(private) != 1 || private[0] != "manovaspace/private" {
		t.Fatalf("private repos = %v, want [manovaspace/private]", private)
	}
}

func TestSyncSecretsUsesStdin(t *testing.T) {
	keysDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(keysDir, "orbit-tg"), []byte("KEYDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := manifest.RepoTarget{
		Name:      "orbit-tg",
		RemoteURL: "git@github.com:manovaspace/orbit-tg.git",
		CISecrets: []string{"NPM_TOKEN", "DEPLOY_SSH_KEY"},
	}
	calls, _, run := recorder(nil, nil)

	if err := SyncSecrets(repo, map[string]string{"NPM_TOKEN": "s3cret"}, keysDir, run); err != nil {
		t.Fatalf("SyncSecrets: %v", err)
	}

	wantStdin := map[string]string{
		"gh secret set NPM_TOKEN -R manovaspace/orbit-tg":      "s3cret",
		"gh secret set DEPLOY_SSH_KEY -R manovaspace/orbit-tg": "KEYDATA",
	}
	if len(*calls) != len(wantStdin) {
		t.Fatalf("got %d calls, want %d", len(*calls), len(wantStdin))
	}
	for _, c := range *calls {
		if c.stdin != wantStdin[c.argv] {
			t.Errorf("%q stdin = %q, want %q", c.argv, c.stdin, wantStdin[c.argv])
		}
		if strings.Contains(c.argv, "s3cret") || strings.Contains(c.argv, "KEYDATA") {
			t.Errorf("secret value leaked into argv: %q", c.argv)
		}
	}
}

func TestSyncSecretsMissingValueSetsNothing(t *testing.T) {
	repo := manifest.RepoTarget{
		Name:      "orbit-tg",
		RemoteURL: "git@github.com:manovaspace/orbit-tg.git",
		CISecrets: []string{"NPM_TOKEN", "MISSING"},
	}
	calls, _, run := recorder(nil, nil)

	err := SyncSecrets(repo, map[string]string{"NPM_TOKEN": "s3cret"}, t.TempDir(), run)
	if err == nil {
		t.Fatal("a missing secret must fail")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("error should name the missing secret: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("nothing may be set when validation fails: %+v", *calls)
	}
}

func TestLoadSourceRejectsWideMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ci-secrets.env")
	if err := os.WriteFile(path, []byte("NPM_TOKEN=abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSource(path); err == nil {
		t.Fatal("0644 source must be refused")
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := LoadSource(path)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if source["NPM_TOKEN"] != "abc" {
		t.Errorf("NPM_TOKEN = %q, want abc", source["NPM_TOKEN"])
	}
}
