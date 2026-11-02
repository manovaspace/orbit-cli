package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

// fakeReleaseTester stands in for the repository's test command.
type fakeReleaseTester struct {
	env   map[string]string
	err   error
	calls int
}

func (t *fakeReleaseTester) Run(_ context.Context, _ string, env map[string]string) error {
	t.calls++
	t.env = env
	return t.err
}

const releaseTestManifest = `version: "1"
workspace: "test"
remotes:
  github_manovaspace: "git@github.com:manovaspace"
groups:
  orbit:
    path: "orbit"
    defaults:
      remote: github_manovaspace
      default_branch: main
    repositories:
      - name: "orbit-cli"
        required: true
`

// setupReleaseCommand writes a one-repository workspace and swaps the release
// runner and clock for test doubles.
func setupReleaseCommand(t *testing.T) (*runnertest.Fake, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", root)
	writeReleaseFile(t, filepath.Join(root, "workspace.yaml"), releaseTestManifest)
	writeReleaseFile(t, filepath.Join(root, "orbit", "orbit-cli", ".github", "workflows", "release.yml"), "name: Release\n")

	fake := &runnertest.Fake{}
	clock := &prFakeClock{now: time.Unix(0, 0)}

	prevRunner, prevSleep, prevNow := releaseRunner, releaseSleep, releaseNow
	prevPoll, prevWindow := releasePollInterval, releaseRegisterWindow
	prevName := releaseNewDatabaseName
	prevTester := releaseTester
	releaseRunner, releaseSleep, releaseNow = fake, clock.Sleep, clock.Now
	releasePollInterval, releaseRegisterWindow = time.Millisecond, 5*time.Millisecond
	releaseNewDatabaseName = func() string { return "test_abc123" }
	releaseTester = &fakeReleaseTester{}
	t.Cleanup(func() {
		releaseRunner, releaseSleep, releaseNow = prevRunner, prevSleep, prevNow
		releasePollInterval, releaseRegisterWindow = prevPoll, prevWindow
		releaseNewDatabaseName = prevName
		releaseTester = prevTester
	})
	return fake, root
}

func writeReleaseFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func releaseRepoDir(root string) string {
	return filepath.Join(root, "orbit", "orbit-cli")
}

func expectReleaseStatusAnswers(fake *runnertest.Fake, root string) {
	dir := releaseRepoDir(root)
	fake.On("git -C "+dir+" rev-parse origin/main", runner.Result{Stdout: "abcdef123456\n"}, nil)
	fake.On("git -C "+dir+" tag --list", runner.Result{Stdout: "v0.9.5\nv0.9.4\n"}, nil)
	fake.On("gh pr list -R manovaspace/orbit-cli", runner.Result{Stdout: `[{"number":12,"title":"chore: version packages","headRefName":"changeset-release/main","baseRefName":"main","author":{"login":"release-bot"},"url":"https://github.com/manovaspace/orbit-cli/pull/12"}]`}, nil)
	fake.On("gh pr checks 12 -R manovaspace/orbit-cli", runner.Result{Stdout: `[{"name":"release-check","bucket":"pass"}]`}, nil)
	fake.On("gh api repos/manovaspace/orbit-cli/git/ref/heads/demo", runner.Result{Stderr: "HTTP 404: Not Found\n"}, &releaseFakeError{})
	fake.On("gh run list -R manovaspace/orbit-cli", runner.Result{Stdout: `[{"name":"Publish","displayTitle":"chore: version packages","status":"completed","conclusion":"success","headBranch":"main","createdAt":"2026-01-01T00:00:00Z","url":"https://github.com/run/1"}]`}, nil)
}

// releaseFakeError is a plain nonzero-exit error for scripted gh answers.
type releaseFakeError struct{}

func (*releaseFakeError) Error() string { return "exit status 1" }

func TestReleaseHelp(t *testing.T) {
	for _, sub := range []string{"", "status", "check", "demo", "prod"} {
		t.Run("Help_"+sub, func(t *testing.T) {
			args := []string{"release"}
			if sub != "" {
				args = append(args, sub)
			}
			args = append(args, "--help")
			out, err := runRootForTest(t, args...)
			if err != nil {
				t.Fatalf("release %s --help: %v\n%s", sub, err, out)
			}
			if !strings.Contains(out, "Usage:") {
				t.Fatalf("expected usage output, got: %s", out)
			}
		})
	}
}

func TestReleaseStatusJSON(t *testing.T) {
	fake, root := setupReleaseCommand(t)
	expectReleaseStatusAnswers(fake, root)

	out, err := runRootForTest(t, "release", "status", "orbit-cli", "--json")
	if err != nil {
		t.Fatalf("release status --json: %v\n%s", err, out)
	}
	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Repos []struct {
			Name      string `json:"name"`
			HeadSHA   string `json:"headSha"`
			LatestTag string `json:"latestTag"`
			ReleasePR struct {
				Number     int64  `json:"number"`
				CheckState string `json:"checkState"`
			} `json:"releasePr"`
			Demo struct {
				Exists bool `json:"exists"`
			} `json:"demo"`
			LastRun struct {
				Name string `json:"name"`
			} `json:"lastRun"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if len(data.Repos) != 1 {
		t.Fatalf("repos = %+v", data.Repos)
	}
	got := data.Repos[0]
	if got.Name != "orbit-cli" || got.HeadSHA != "abcdef123456" || got.LatestTag != "v0.9.5" {
		t.Errorf("status = %+v", got)
	}
	if got.ReleasePR.Number != 12 || got.ReleasePR.CheckState != "pass" {
		t.Errorf("release PR = %+v", got.ReleasePR)
	}
	if got.Demo.Exists {
		t.Errorf("demo = %+v", got.Demo)
	}
	if got.LastRun.Name != "Publish" {
		t.Errorf("last run = %+v", got.LastRun)
	}
}

func TestReleaseStatusUnreadableIsUnverified(t *testing.T) {
	fake, root := setupReleaseCommand(t)
	dir := releaseRepoDir(root)
	fake.On("git -C "+dir+" rev-parse origin/main", runner.Result{Stdout: "abcdef123456\n"}, nil)
	fake.On("git -C "+dir+" tag --list", runner.Result{}, nil)
	fake.On("gh pr list -R manovaspace/orbit-cli", runner.Result{}, &releaseFakeError{})

	out, err := runRootForTest(t, "release", "status", "orbit-cli", "--json")
	if err == nil {
		t.Fatal("expected an unverified error")
	}
	if exitCodeFor(err) != 2 {
		t.Errorf("exit code = %d, want 2", exitCodeFor(err))
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "unverified" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if len(env.Data) == 0 {
		t.Error("expected the readable status to stay in the envelope")
	}
}

func TestReleaseCheckJSONFailureExitOne(t *testing.T) {
	fake, root := setupReleaseCommand(t)
	_ = root
	fake.On("gh pr list -R manovaspace/orbit-cli", runner.Result{Stdout: `[{"number":12,"title":"chore: version packages","headRefName":"changeset-release/main","baseRefName":"main","author":{"login":"release-bot"},"url":"https://github.com/manovaspace/orbit-cli/pull/12"}]`}, nil)
	fake.On("gh pr checks 12 -R manovaspace/orbit-cli", runner.Result{Stdout: `[{"name":"release-check","bucket":"fail"}]`}, nil)

	out, err := runRootForTest(t, "release", "check", "orbit-cli", "--json")
	if err == nil {
		t.Fatal("expected the check to fail")
	}
	if exitCodeFor(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeFor(err))
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "failed" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestReleaseCheckNoPRExitTwo(t *testing.T) {
	fake, _ := setupReleaseCommand(t)
	fake.On("gh pr list -R manovaspace/orbit-cli", runner.Result{Stdout: `[]`}, nil)

	out, err := runRootForTest(t, "release", "check", "orbit-cli", "--json")
	if err == nil {
		t.Fatal("expected an unverified error")
	}
	if exitCodeFor(err) != 2 {
		t.Errorf("exit code = %d, want 2", exitCodeFor(err))
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "unverified" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestReleaseDemoJSON(t *testing.T) {
	fake, _ := setupReleaseCommand(t)
	fake.On("gh api repos/manovaspace/orbit-cli/git/ref/heads/main --jq .object.sha", runner.Result{Stdout: "abc123456789\n"}, nil)
	fake.On("gh api repos/manovaspace/orbit-cli/git/ref/heads/demo --jq .object.sha", runner.Result{Stdout: "def456789012\n"}, nil)
	fake.On("gh api repos/manovaspace/orbit-cli/commits/abc123456789/check-runs", runner.Result{Stdout: `{"name":"ci","status":"completed","conclusion":"success"}` + "\n"}, nil)
	fake.On("gh pr create -R manovaspace/orbit-cli", runner.Result{Stdout: "https://github.com/manovaspace/orbit-cli/pull/7\n"}, nil)
	fake.On("gh pr merge 7 -R manovaspace/orbit-cli --merge", runner.Result{}, nil)

	out, err := runRootForTest(t, "release", "demo", "orbit-cli", "--json")
	if err != nil {
		t.Fatalf("release demo --json: %v\n%s", err, out)
	}
	env := decodeEnvelope(t, out)
	if !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		MainSHA     string `json:"mainSha"`
		PullRequest struct {
			Number int64 `json:"number"`
		} `json:"pullRequest"`
		Merged bool `json:"merged"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.MainSHA != "abc123456789" || data.PullRequest.Number != 7 || !data.Merged {
		t.Errorf("demo = %+v", data)
	}
}

func TestReleaseDemoRefusesPendingChecks(t *testing.T) {
	fake, _ := setupReleaseCommand(t)
	fake.On("gh api repos/manovaspace/orbit-cli/git/ref/heads/main --jq .object.sha", runner.Result{Stdout: "abc123456789\n"}, nil)
	fake.On("gh api repos/manovaspace/orbit-cli/git/ref/heads/demo --jq .object.sha", runner.Result{Stdout: "def456789012\n"}, nil)
	fake.On("gh api repos/manovaspace/orbit-cli/commits/abc123456789/check-runs", runner.Result{Stdout: `{"name":"ci","status":"in_progress","conclusion":""}` + "\n"}, nil)

	out, err := runRootForTest(t, "release", "demo", "orbit-cli", "--json")
	if err == nil {
		t.Fatal("expected the demo gate to refuse pending checks")
	}
	if exitCodeFor(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeFor(err))
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "failed" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestReleaseProdLibraryDispatchesPublish(t *testing.T) {
	fake, root := setupReleaseCommand(t)
	writeReleaseFile(t, filepath.Join(releaseRepoDir(root), ".changeset", "config.json"), "{}\n")
	fake.On("gh pr list -R manovaspace/orbit-cli", runner.Result{Stdout: `[{"number":12,"title":"chore: version packages","headRefName":"changeset-release/main","baseRefName":"main","author":{"login":"release-bot"},"url":"https://github.com/manovaspace/orbit-cli/pull/12"}]`}, nil)
	fake.On("gh pr checks 12 -R manovaspace/orbit-cli", runner.Result{Stdout: `[{"name":"release-check","bucket":"pass"}]`}, nil)
	fake.On("gh pr merge 12 -R manovaspace/orbit-cli --merge", runner.Result{}, nil)
	fake.On("gh workflow run publish.yml -R manovaspace/orbit-cli --ref main", runner.Result{}, nil)

	out, err := runRootForTest(t, "release", "prod", "orbit-cli", "--json")
	if err != nil {
		t.Fatalf("release prod --json: %v\n%s", err, out)
	}
	env := decodeEnvelope(t, out)
	if !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Library       bool   `json:"library"`
		Merged        bool   `json:"merged"`
		Dispatched    string `json:"dispatched"`
		OwnerApproval string `json:"ownerApproval"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if !data.Library || !data.Merged || data.Dispatched != "publish.yml" {
		t.Errorf("prod = %+v", data)
	}
	if !strings.Contains(data.OwnerApproval, "npm-publish") {
		t.Errorf("owner approval = %q", data.OwnerApproval)
	}
}

func TestReleaseProdServiceDoesNotDispatch(t *testing.T) {
	fake, _ := setupReleaseCommand(t)
	fake.On("gh pr list -R manovaspace/orbit-cli", runner.Result{Stdout: `[{"number":12,"title":"chore: version packages","headRefName":"changeset-release/main","baseRefName":"main","author":{"login":"release-bot"},"url":"https://github.com/manovaspace/orbit-cli/pull/12"}]`}, nil)
	fake.On("gh pr checks 12 -R manovaspace/orbit-cli", runner.Result{Stdout: `[{"name":"release-check","bucket":"pass"}]`}, nil)
	fake.On("gh pr merge 12 -R manovaspace/orbit-cli --merge", runner.Result{}, nil)

	out, err := runRootForTest(t, "release", "prod", "orbit-cli", "--json")
	if err != nil {
		t.Fatalf("release prod --json: %v\n%s", err, out)
	}
	env := decodeEnvelope(t, out)
	var data struct {
		Library    bool   `json:"library"`
		Dispatched string `json:"dispatched"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.Library || data.Dispatched != "" {
		t.Errorf("service prod = %+v", data)
	}
}

func TestReleaseProdWithoutReleasePRRefused(t *testing.T) {
	fake, _ := setupReleaseCommand(t)
	fake.On("gh pr list -R manovaspace/orbit-cli", runner.Result{Stdout: `[]`}, nil)

	out, err := runRootForTest(t, "release", "prod", "orbit-cli", "--json")
	if err == nil {
		t.Fatal("expected prod to refuse without a release PR")
	}
	if exitCodeFor(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeFor(err))
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "failed" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestReleaseCheckLocalDropsDatabaseOnTestFailure(t *testing.T) {
	fake, root := setupReleaseCommand(t)
	dir := releaseRepoDir(root)
	writeReleaseFile(t, filepath.Join(dir, ".env"), "DATABASE_URL=postgres://orbit:secret@localhost:5432/dev?sslmode=disable\n")
	writeReleaseFile(t, filepath.Join(dir, "go.mod"), "module example.test\n")

	fake.On("psql postgres://orbit:secret@localhost:5432/dev?sslmode=disable -tAc SELECT 1", runner.Result{Stdout: "1\n"}, nil)
	fake.On("psql postgres://orbit:secret@localhost:5432/dev?sslmode=disable -v ON_ERROR_STOP=1 -c CREATE DATABASE test_abc123", runner.Result{}, nil)
	fake.On("psql postgres://orbit:secret@localhost:5432/dev?sslmode=disable -v ON_ERROR_STOP=1 -c DROP DATABASE IF EXISTS test_abc123", runner.Result{}, nil)
	tested := releaseTester.(*fakeReleaseTester)
	tested.err = errors.New("tests failed")

	out, err := runRootForTest(t, "release", "check", "orbit-cli", "--local", "--json")
	if err == nil {
		t.Fatal("expected the local tier to fail")
	}
	if exitCodeFor(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeFor(err))
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "failed" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if tested.calls != 1 {
		t.Errorf("test command calls = %d, want 1", tested.calls)
	}
	if tested.env["CI_TIER"] != "release" || tested.env["TEST_DATABASE_URL"] == "" {
		t.Errorf("test environment = %v", tested.env)
	}
	dropped := false
	for _, call := range fake.Calls() {
		if strings.Contains(strings.Join(call.Args, " "), "DROP DATABASE IF EXISTS test_abc123") {
			dropped = true
		}
	}
	if !dropped {
		t.Errorf("throwaway database was not dropped after the test failure: %v", fake.Calls())
	}
}

func TestReleaseCheckLocalUnreachableExitTwo(t *testing.T) {
	fake, root := setupReleaseCommand(t)
	dir := releaseRepoDir(root)
	writeReleaseFile(t, filepath.Join(dir, ".env"), "DATABASE_URL=postgres://orbit:secret@localhost:5432/dev?sslmode=disable\n")
	fake.On("psql postgres://orbit:secret@localhost:5432/dev?sslmode=disable -tAc SELECT 1", runner.Result{Stderr: "no route to host\n"}, &releaseFakeError{})

	out, err := runRootForTest(t, "release", "check", "orbit-cli", "--local", "--json")
	if err == nil {
		t.Fatal("expected an unverified error")
	}
	if exitCodeFor(err) != 2 {
		t.Errorf("exit code = %d, want 2", exitCodeFor(err))
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "unverified" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}
