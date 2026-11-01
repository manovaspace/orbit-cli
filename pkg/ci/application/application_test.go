package application

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/ci/domain"
	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

type call struct {
	stdin string
	argv  string
}

// fakeCommand records calls and answers from scripted results.
type fakeCommand struct {
	calls   []call
	result  map[string]string
	failure map[string]error
}

func newFakeCommand() *fakeCommand {
	return &fakeCommand{result: map[string]string{}, failure: map[string]error{}}
}

func (f *fakeCommand) Run(_ context.Context, stdin, _ string, name string, args ...string) (string, error) {
	argv := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call{stdin: stdin, argv: argv})
	if err, ok := f.failure[argv]; ok {
		return "", err
	}
	return f.result[argv], nil
}

// fakeFiles is an in-memory secret source.
type fakeFiles struct {
	data  map[string][]byte
	modes map[string]uint32
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{data: map[string][]byte{}, modes: map[string]uint32{}}
}

func (f *fakeFiles) ReadFile(path string) ([]byte, error) {
	data, ok := f.data[path]
	if !ok {
		return nil, errors.New("no such file: " + path)
	}
	return data, nil
}

func (f *fakeFiles) Perm(path string) (uint32, error) {
	mode, ok := f.modes[path]
	if !ok {
		return 0, errors.New("no such file: " + path)
	}
	return mode, nil
}

func newService(commands *fakeCommand, files *fakeFiles) *Service {
	return &Service{Commands: commands, Files: files, Out: io.Discard}
}

func TestSetRunnerVPSOrder(t *testing.T) {
	commands := newFakeCommand()
	if err := newService(commands, newFakeFiles()).SetRunner(context.Background(), domain.ModeVPS, []string{"manovaspace/a", "manovaspace/b"}); err != nil {
		t.Fatalf("SetRunner: %v", err)
	}

	want := []string{
		"sudo systemctl start gh-ci",
		"gh variable set CI_RUNNER --body vps -R manovaspace/a",
		"gh variable set CI_RUNNER --body vps -R manovaspace/b",
	}
	if len(commands.calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(commands.calls), len(want), commands.calls)
	}
	for i := range want {
		if commands.calls[i].argv != want[i] {
			t.Errorf("call %d = %q, want %q", i, commands.calls[i].argv, want[i])
		}
	}
}

func TestSetRunnerGithubIgnoresMissingVar(t *testing.T) {
	commands := newFakeCommand()
	commands.failure["gh variable delete CI_RUNNER -R manovaspace/b"] = errors.New("variable not found")
	commands.failure["gh variable delete CI_RUNNER -R manovaspace/c"] = errors.New("failed to delete variable CI_RUNNER: HTTP 404")

	if err := newService(commands, newFakeFiles()).SetRunner(context.Background(), domain.ModeGitHub, []string{"manovaspace/a", "manovaspace/b", "manovaspace/c"}); err != nil {
		t.Fatalf("SetRunner: %v", err)
	}
	if last := commands.calls[len(commands.calls)-1].argv; last != "sudo systemctl stop gh-ci" {
		t.Errorf("last call = %q, want the runner stop", last)
	}
}

func TestSetRunnerRejectsUnknownMode(t *testing.T) {
	err := newService(newFakeCommand(), newFakeFiles()).SetRunner(context.Background(), domain.RunnerMode("carrier-pigeon"), nil)
	if err == nil {
		t.Fatal("unknown mode must fail")
	}
	if got := clierr.ExitCode(err); got != clierr.ExitUsage {
		t.Errorf("exit = %d, want usage", got)
	}
}

func TestPrivateReposFiltersPublic(t *testing.T) {
	commands := newFakeCommand()
	commands.result["gh repo view manovaspace/private --json visibility --jq .visibility"] = "PRIVATE"
	commands.result["gh repo view manovaspace/public --json visibility --jq .visibility"] = "PUBLIC"

	repos := []manifest.RepoTarget{
		{Name: "private", RemoteURL: "git@github.com:manovaspace/private.git"},
		{Name: "public", RemoteURL: "https://github.com/manovaspace/public"},
		{Name: "self-hosted", RemoteURL: "git@git.internal.manova.space:manova/toolkit.git"},
	}
	private, err := newService(commands, newFakeFiles()).PrivateRepos(context.Background(), repos)
	if err != nil {
		t.Fatalf("PrivateRepos: %v", err)
	}
	if len(private) != 1 || private[0] != "manovaspace/private" {
		t.Fatalf("private repos = %v, want [manovaspace/private]", private)
	}
}

func TestRunnerStatus(t *testing.T) {
	commands := newFakeCommand()
	commands.result["systemctl is-active gh-ci"] = "active\n"
	commands.result["gh variable get CI_RUNNER -R manovaspace/a --json value --jq .value"] = "vps\n"
	commands.failure["gh variable get CI_RUNNER -R manovaspace/b --json value --jq .value"] = errors.New("not found")

	status, err := newService(commands, newFakeFiles()).RunnerStatus(context.Background(), []string{"manovaspace/a", "manovaspace/b"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Service != "active" || status.Vars["manovaspace/a"] != "vps" {
		t.Fatalf("status = %+v", status)
	}
	if _, ok := status.Vars["manovaspace/b"]; ok {
		t.Error("unset variable recorded")
	}
}

func TestSyncSecretsUsesStdin(t *testing.T) {
	keysDir := t.TempDir()
	files := newFakeFiles()
	keyPath := filepath.Join(keysDir, "orbit-tg")
	files.data[keyPath] = []byte("KEYDATA")
	files.modes[keyPath] = 0o600
	repo := manifest.RepoTarget{
		Name:      "orbit-tg",
		RemoteURL: "git@github.com:manovaspace/orbit-tg.git",
		CISecrets: []string{"NPM_TOKEN", "DEPLOY_SSH_KEY"},
	}
	commands := newFakeCommand()

	count, err := newService(commands, files).SyncSecrets(context.Background(), repo, map[string]string{"NPM_TOKEN": "s3cret"}, keysDir)
	if err != nil {
		t.Fatalf("SyncSecrets: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	wantStdin := map[string]string{
		"gh secret set NPM_TOKEN -R manovaspace/orbit-tg":      "s3cret",
		"gh secret set DEPLOY_SSH_KEY -R manovaspace/orbit-tg": "KEYDATA",
	}
	if len(commands.calls) != len(wantStdin) {
		t.Fatalf("got %d calls, want %d", len(commands.calls), len(wantStdin))
	}
	for _, c := range commands.calls {
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
	commands := newFakeCommand()

	_, err := newService(commands, newFakeFiles()).SyncSecrets(context.Background(), repo, map[string]string{"NPM_TOKEN": "s3cret"}, t.TempDir())
	if err == nil {
		t.Fatal("a missing secret must fail")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("error should name the missing secret: %v", err)
	}
	if len(commands.calls) != 0 {
		t.Errorf("nothing may be set when validation fails: %+v", commands.calls)
	}
}

func TestSyncSelectsRepositoriesAndPrintsSummary(t *testing.T) {
	commands := newFakeCommand()
	repo := manifest.RepoTarget{
		Name:      "orbit-tg",
		RemoteURL: "git@github.com:manovaspace/orbit-tg.git",
		CISecrets: []string{"NPM_TOKEN"},
	}
	out := new(strings.Builder)
	svc := &Service{Commands: commands, Files: newFakeFiles(), Out: out}

	res, err := svc.Sync(context.Background(), []manifest.RepoTarget{repo}, "orbit-tg", map[string]string{"NPM_TOKEN": "s3cret"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Repositories) != 1 || res.Repositories[0].Secrets != 1 {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(out.String(), "orbit-tg: set 1 secret(s)") {
		t.Errorf("output = %q", out.String())
	}

	_, err = svc.Sync(context.Background(), []manifest.RepoTarget{repo}, "nope", nil, t.TempDir())
	if err == nil {
		t.Fatal("expected a selection error")
	}
	if got := clierr.ExitCode(err); got != clierr.ExitUsage {
		t.Errorf("exit = %d, want usage", got)
	}
}

func TestLoadSourceRejectsWideMode(t *testing.T) {
	path := "/secrets/ci-secrets.env"
	files := newFakeFiles()
	files.data[path] = []byte("NPM_TOKEN=abc\n")
	files.modes[path] = 0o644

	if _, err := newService(newFakeCommand(), files).LoadSource(path); err == nil {
		t.Fatal("0644 source must be refused")
	}

	files.modes[path] = 0o600
	source, err := newService(newFakeCommand(), files).LoadSource(path)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if source["NPM_TOKEN"] != "abc" {
		t.Errorf("NPM_TOKEN = %q, want abc", source["NPM_TOKEN"])
	}
}
