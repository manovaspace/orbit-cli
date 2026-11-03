package main

import (
	"bytes"
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

// prFakeClock advances on every sleep so waiting commands run instantly.
type prFakeClock struct {
	now time.Time
}

func (c *prFakeClock) Now() time.Time { return c.now }

func (c *prFakeClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

// setupPRFake swaps the pr command's runner and clock for test doubles.
func setupPRFake(t *testing.T) *runnertest.Fake {
	t.Helper()
	fake := &runnertest.Fake{}
	clock := &prFakeClock{now: time.Unix(0, 0)}

	prevRunner, prevSleep, prevNow := prRunner, prSleep, prNow
	prevPoll, prevWindow := prPollInterval, prRegisterWindow
	prRunner, prSleep, prNow = fake, clock.Sleep, clock.Now
	prPollInterval, prRegisterWindow = time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() {
		prRunner, prSleep, prNow = prevRunner, prevSleep, prevNow
		prPollInterval, prRegisterWindow = prevPoll, prevWindow
	})
	return fake
}

// expectGitAnswers registers the repository detection the pr commands perform.
func expectGitAnswers(fake *runnertest.Fake) {
	fake.On("git -C /repo config --get remote.origin.url", runner.Result{Stdout: "git@github.com:manovaspace/orbit-test.git\n"}, nil)
	fake.On("git -C /repo rev-parse --abbrev-ref HEAD", runner.Result{Stdout: "feat/x\n"}, nil)
	fake.On("git -C /repo log -1 --pretty=%s feat/x", runner.Result{Stdout: "feat: thing\n"}, nil)
	fake.On("git -C /repo log -1 --pretty=%b feat/x", runner.Result{Stdout: "body\n"}, nil)
}

func runPRCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

type prEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	OK            bool            `json:"ok"`
	Data          json.RawMessage `json:"data"`
	Error         *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// decodeEnvelope finds the JSON envelope line, which cobra may follow with its
// own "Error:" line on failure.
func decodeEnvelope(t *testing.T, out string) prEnvelope {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var env prEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("decoding envelope %q: %v", line, err)
		}
		return env
	}
	t.Fatalf("no JSON envelope in output: %s", out)
	return prEnvelope{}
}

func TestPRHelp(t *testing.T) {
	subcommands := []string{"", "create", "merge", "status", "deliver"}
	for _, sub := range subcommands {
		t.Run("Help_"+sub, func(t *testing.T) {
			buf := new(bytes.Buffer)
			cmd := newRootCmd()
			cmd.SetOut(buf)
			cmd.SetErr(buf)

			args := []string{"pr"}
			if sub != "" {
				args = append(args, sub)
			}
			args = append(args, "--help")
			cmd.SetArgs(args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error executing pr %s --help: %v", sub, err)
			}

			out := buf.String()
			if !strings.Contains(out, "Usage:") {
				t.Errorf("expected usage output, got: %s", out)
			}
		})
	}
}

func TestPRCreateJSON(t *testing.T) {
	fake := setupPRFake(t)
	expectGitAnswers(fake)
	fake.On("git -C /repo push -u origin feat/x", runner.Result{}, nil)
	fake.On("gh pr create", runner.Result{Stdout: "https://github.com/manovaspace/orbit-test/pull/7\n"}, nil)

	out, err := runPRCmd(t, "pr", "create", "--dir", "/repo", "--json")
	if err != nil {
		t.Fatalf("pr create --json: %v\nOutput: %s", err, out)
	}

	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Branch      string `json:"branch"`
		Base        string `json:"base"`
		PullRequest struct {
			Number int64  `json:"number"`
			URL    string `json:"url"`
		} `json:"pullRequest"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.Branch != "feat/x" || data.Base != "main" {
		t.Errorf("unexpected branches: %+v", data)
	}
	if data.PullRequest.Number != 7 {
		t.Errorf("pull request number = %d, want 7", data.PullRequest.Number)
	}
}

func TestPRMergeJSON(t *testing.T) {
	fake := setupPRFake(t)
	expectGitAnswers(fake)
	fake.On("gh pr checks", runner.Result{Stdout: `[{"name":"ci","bucket":"pass"}]`}, nil)
	fake.On("gh pr merge", runner.Result{}, nil)

	out, err := runPRCmd(t, "pr", "merge", "7", "--dir", "/repo", "--method", "squash", "--json")
	if err != nil {
		t.Fatalf("pr merge --json: %v\nOutput: %s", err, out)
	}

	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Ref    string `json:"ref"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.Ref != "7" || data.Method != "squash" {
		t.Errorf("unexpected merge data: %+v", data)
	}
}

func TestPRMergeJSONFailure(t *testing.T) {
	fake := setupPRFake(t)
	expectGitAnswers(fake)
	fake.On("gh pr checks", runner.Result{Stdout: `[{"name":"ci","bucket":"fail"}]`}, nil)

	out, err := runPRCmd(t, "pr", "merge", "7", "--dir", "/repo", "--json")
	if err == nil {
		t.Fatal("expected merge to be refused, got nil")
	}

	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if env.Error == nil || env.Error.Code != "failed" {
		t.Fatalf("unexpected error body: %+v", env.Error)
	}
	if !strings.Contains(env.Error.Message, "ci") {
		t.Errorf("expected the failing check in the message, got: %s", env.Error.Message)
	}
}

func TestPRStatusJSON(t *testing.T) {
	fake := setupPRFake(t)
	expectGitAnswers(fake)
	fake.On("gh pr list", runner.Result{Stdout: `[{"number":12,"title":"feat: x","headRefName":"feat/x","baseRefName":"main","author":{"login":"opmc"},"url":"https://github.com/manovaspace/orbit-test/pull/12"}]`}, nil)

	out, err := runPRCmd(t, "pr", "status", "--dir", "/repo", "--json")
	if err != nil {
		t.Fatalf("pr status --json: %v\nOutput: %s", err, out)
	}

	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Repo         string `json:"repo"`
		PullRequests []struct {
			Number int64  `json:"number"`
			Title  string `json:"title"`
			Head   string `json:"head"`
			Base   string `json:"base"`
			Author string `json:"author"`
			URL    string `json:"url"`
		} `json:"pullRequests"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.Repo != "manovaspace/orbit-test" {
		t.Errorf("repo = %q", data.Repo)
	}
	if len(data.PullRequests) != 1 {
		t.Fatalf("got %d pull requests, want 1", len(data.PullRequests))
	}
	got := data.PullRequests[0]
	if got.Number != 12 || got.Title != "feat: x" || got.Head != "feat/x" || got.Base != "main" || got.Author != "opmc" || got.URL == "" {
		t.Errorf("unexpected pull request: %+v", got)
	}
}

func TestPRStatusHumanOutputHasAuthorColumn(t *testing.T) {
	fake := setupPRFake(t)
	expectGitAnswers(fake)
	fake.On("gh pr list", runner.Result{Stdout: `[{"number":12,"title":"feat: x","headRefName":"feat/x","baseRefName":"main","author":{"login":"opmc"},"url":"https://github.com/manovaspace/orbit-test/pull/12"}]`}, nil)

	out, err := runPRCmd(t, "pr", "status", "--dir", "/repo")
	if err != nil {
		t.Fatalf("pr status: %v\nOutput: %s", err, out)
	}
	for _, want := range []string{"AUTHOR", "opmc"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the status table, got: %s", want, out)
		}
	}
	if strings.Contains(out, "schemaVersion") {
		t.Errorf("human output must not be a JSON envelope: %s", out)
	}
}

// setupPRWorkspace writes a three-repository manifest: repo-a has one open PR,
// repo-b has none, and repo-c is gitless and must be skipped.
func setupPRWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", root)

	for _, name := range []string{"repo-a", "repo-b"} {
		if err := os.MkdirAll(filepath.Join(root, name, ".git"), 0o755); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "repo-c"), 0o755); err != nil {
		t.Fatalf("create repo-c: %v", err)
	}

	manifestContent := `version: "1"
workspace: "test"
groups:
  test:
    path: ""
    repositories:
      - name: "repo-a"
        path: "repo-a"
      - name: "repo-b"
        path: "repo-b"
      - name: "repo-c"
        path: "repo-c"
`
	if err := os.WriteFile(filepath.Join(root, "workspace.yaml"), []byte(manifestContent), 0o600); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}
	return root
}

func expectWorkspacePRAnswers(t *testing.T, fake *runnertest.Fake) {
	t.Helper()
	root := os.Getenv("ORBIT_WORKSPACE")
	repoA := filepath.Join(root, "repo-a")
	repoB := filepath.Join(root, "repo-b")

	fake.On("git -C "+repoA+" config --get remote.origin.url", runner.Result{Stdout: "git@github.com:manovaspace/repo-a.git\n"}, nil)
	fake.On("git -C "+repoA+" rev-parse --abbrev-ref HEAD", runner.Result{Stdout: "main\n"}, nil)
	fake.On("gh pr list -R manovaspace/repo-a", runner.Result{Stdout: `[{"number":21,"title":"feat: a","headRefName":"feat/a","baseRefName":"main","author":{"login":"opmc"},"url":"https://github.com/manovaspace/repo-a/pull/21"}]`}, nil)

	fake.On("git -C "+repoB+" config --get remote.origin.url", runner.Result{Stdout: "git@github.com:manovaspace/repo-b.git\n"}, nil)
	fake.On("git -C "+repoB+" rev-parse --abbrev-ref HEAD", runner.Result{Stdout: "main\n"}, nil)
	fake.On("gh pr list -R manovaspace/repo-b", runner.Result{Stdout: `[]`}, nil)
}

func TestPRStatusPatternJSON(t *testing.T) {
	setupPRWorkspace(t)
	fake := setupPRFake(t)
	expectWorkspacePRAnswers(t, fake)

	out, err := runPRCmd(t, "pr", "status", "repo-*", "--json")
	if err != nil {
		t.Fatalf("pr status repo-* --json: %v\nOutput: %s", err, out)
	}

	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Repos []struct {
			Repo         string `json:"repo"`
			PullRequests []struct {
				Number int64 `json:"number"`
			} `json:"pullRequests"`
			Skipped string `json:"skipped"`
			Error   string `json:"error"`
		} `json:"repos"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if len(data.Repos) != 3 {
		t.Fatalf("got %d repositories, want 3: %+v", len(data.Repos), data.Repos)
	}
	if data.Repos[0].Repo != "repo-a" || len(data.Repos[0].PullRequests) != 1 || data.Repos[0].PullRequests[0].Number != 21 {
		t.Errorf("unexpected repo-a result: %+v", data.Repos[0])
	}
	if data.Repos[1].Repo != "repo-b" || len(data.Repos[1].PullRequests) != 0 {
		t.Errorf("unexpected repo-b result: %+v", data.Repos[1])
	}
	if data.Repos[2].Repo != "repo-c" || data.Repos[2].Skipped == "" || data.Repos[2].Error != "" {
		t.Errorf("unexpected repo-c result: %+v", data.Repos[2])
	}
	if data.Total != 1 {
		t.Errorf("total = %d, want 1", data.Total)
	}
}

func TestPRStatusPatternHumanOutput(t *testing.T) {
	setupPRWorkspace(t)
	fake := setupPRFake(t)
	expectWorkspacePRAnswers(t, fake)

	out, err := runPRCmd(t, "pr", "status", "repo-*")
	if err != nil {
		t.Fatalf("pr status repo-*: %v\nOutput: %s", err, out)
	}
	for _, want := range []string{
		"Open pull requests",
		"repo-a",
		"No open PRs in 1 repository",
		"Skipped: repo-c",
		"1 open pull request across 2 repositories",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
	lines := strings.Split(out, "\n")
	urlIndex := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == "https://github.com/manovaspace/repo-a/pull/21" {
			urlIndex = index
			break
		}
	}
	if urlIndex < 0 {
		t.Fatalf("expected the pull request URL on its own line, got:\n%s", out)
	}
	if urlIndex == 0 || !strings.Contains(lines[urlIndex-1], "#21") {
		t.Errorf("expected the URL directly below its row, got:\n%s", out)
	}
	if strings.Contains(out, "schemaVersion") {
		t.Errorf("human output must not be a JSON envelope: %s", out)
	}
}

func TestPRStatusPatternNoURL(t *testing.T) {
	setupPRWorkspace(t)
	fake := setupPRFake(t)
	expectWorkspacePRAnswers(t, fake)

	out, err := runPRCmd(t, "pr", "status", "repo-*", "--no-url")
	if err != nil {
		t.Fatalf("pr status repo-* --no-url: %v\nOutput: %s", err, out)
	}
	if strings.Contains(out, "https://github.com/manovaspace/repo-a/pull/21") {
		t.Errorf("--no-url must hide pull request URLs, got:\n%s", out)
	}
	if !strings.Contains(out, "repo-a") || !strings.Contains(out, "#21") {
		t.Errorf("--no-url must keep the table rows, got:\n%s", out)
	}
}

func TestPRStatusPatternContinuesAfterRepoError(t *testing.T) {
	setupPRWorkspace(t)
	fake := setupPRFake(t)
	root := os.Getenv("ORBIT_WORKSPACE")
	repoA := filepath.Join(root, "repo-a")
	repoB := filepath.Join(root, "repo-b")

	fake.On("git -C "+repoA+" config --get remote.origin.url", runner.Result{Stdout: "git@github.com:manovaspace/repo-a.git\n"}, nil)
	fake.On("git -C "+repoA+" rev-parse --abbrev-ref HEAD", runner.Result{Stdout: "main\n"}, nil)
	fake.On("gh pr list -R manovaspace/repo-a", runner.Result{Stdout: `[{"number":21,"title":"feat: a","headRefName":"feat/a","baseRefName":"main","author":{"login":"opmc"},"url":"https://github.com/manovaspace/repo-a/pull/21"}]`}, nil)

	fake.On("git -C "+repoB+" config --get remote.origin.url", runner.Result{Stdout: "git@github.com:manovaspace/repo-b.git\n"}, nil)
	fake.On("git -C "+repoB+" rev-parse --abbrev-ref HEAD", runner.Result{Stdout: "main\n"}, nil)
	fake.On("gh pr list -R manovaspace/repo-b", runner.Result{Stderr: "gh: could not reach GitHub\n"}, errors.New("exit status 1"))

	out, err := runPRCmd(t, "pr", "status", "repo-*", "--json")
	if err == nil {
		t.Fatalf("expected a per-repository failure, got nil\nOutput: %s", out)
	}

	env := decodeEnvelope(t, out)
	if env.OK {
		t.Fatalf("unexpected success envelope: %+v", env)
	}
	var data struct {
		Repos []struct {
			Repo         string `json:"repo"`
			PullRequests []struct {
				Number int64 `json:"number"`
			} `json:"pullRequests"`
			Skipped string `json:"skipped"`
			Error   string `json:"error"`
		} `json:"repos"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if len(data.Repos) != 3 {
		t.Fatalf("got %d repositories, want 3: %+v", len(data.Repos), data.Repos)
	}
	if len(data.Repos[0].PullRequests) != 1 {
		t.Errorf("repo-a result was lost: %+v", data.Repos[0])
	}
	if data.Repos[1].Error == "" {
		t.Errorf("expected repo-b error, got: %+v", data.Repos[1])
	}
	if data.Repos[2].Skipped == "" {
		t.Errorf("expected repo-c skipped, got: %+v", data.Repos[2])
	}
	if data.Total != 1 {
		t.Errorf("total = %d, want 1", data.Total)
	}
}

func TestPRStatusPatternNoMatchIsUsageError(t *testing.T) {
	setupPRWorkspace(t)

	out, err := runPRCmd(t, "pr", "status", "nope*", "--json")
	if err == nil {
		t.Fatalf("expected a usage error, got nil\nOutput: %s", out)
	}
	if got := exitCodeFor(err); got != 3 {
		t.Errorf("exit code = %d, want 3\nOutput: %s", got, out)
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if !strings.Contains(env.Error.Message, "no repositories match") {
		t.Errorf("unexpected error message: %s", env.Error.Message)
	}
}

func TestPRStatusPatternRejectsDir(t *testing.T) {
	setupPRWorkspace(t)

	out, err := runPRCmd(t, "pr", "status", "repo-*", "--dir", "/repo", "--json")
	if err == nil {
		t.Fatalf("expected a usage error, got nil\nOutput: %s", out)
	}
	if got := exitCodeFor(err); got != 3 {
		t.Errorf("exit code = %d, want 3\nOutput: %s", got, out)
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if !strings.Contains(env.Error.Message, "--dir") {
		t.Errorf("unexpected error message: %s", env.Error.Message)
	}
}

func TestPRStatusEmptyManifestWildcardJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ORBIT_WORKSPACE", root)
	if err := os.WriteFile(filepath.Join(root, "workspace.yaml"), []byte("version: \"1\"\nworkspace: test\ngroups: {}\n"), 0o600); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}

	out, err := runPRCmd(t, "pr", "status", "*", "--json")
	if err != nil {
		t.Fatalf("pr status '*' on an empty manifest: %v\nOutput: %s", err, out)
	}
	env := decodeEnvelope(t, out)
	if !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Repos []struct{} `json:"repos"`
		Total int        `json:"total"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if len(data.Repos) != 0 || data.Total != 0 {
		t.Errorf("unexpected empty result: %+v", data)
	}
}

func TestPRDeliverJSON(t *testing.T) {
	fake := setupPRFake(t)
	expectGitAnswers(fake)
	fake.On("git -C /repo status --porcelain", runner.Result{}, nil)
	fake.On("git -C /repo push -u origin feat/x", runner.Result{}, nil)
	fake.On("git -C /repo checkout main", runner.Result{}, nil)
	fake.On("git -C /repo pull --ff-only", runner.Result{}, nil)
	fake.On("git -C /repo branch -d feat/x", runner.Result{}, nil)
	fake.On("gh pr create", runner.Result{Stdout: "https://github.com/manovaspace/orbit-test/pull/7\n"}, nil)
	fake.On("gh pr checks", runner.Result{Stdout: `[{"name":"ci","bucket":"pass"}]`}, nil)
	fake.On("gh pr merge", runner.Result{}, nil)

	out, err := runPRCmd(t, "pr", "deliver", "--dir", "/repo", "--json")
	if err != nil {
		t.Fatalf("pr deliver --json: %v\nOutput: %s", err, out)
	}

	env := decodeEnvelope(t, out)
	if env.SchemaVersion != 1 || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	var data struct {
		Branch      string `json:"branch"`
		Base        string `json:"base"`
		PullRequest struct {
			Number int64 `json:"number"`
		} `json:"pullRequest"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	if data.Branch != "feat/x" || data.Base != "main" || data.PullRequest.Number != 7 {
		t.Errorf("unexpected deliver data: %+v", data)
	}
}

func TestPRMergeHumanOutput(t *testing.T) {
	fake := setupPRFake(t)
	expectGitAnswers(fake)
	fake.On("gh pr checks", runner.Result{Stdout: `[{"name":"ci","bucket":"pass"}]`}, nil)
	fake.On("gh pr merge", runner.Result{}, nil)

	out, err := runPRCmd(t, "pr", "merge", "7", "--dir", "/repo")
	if err != nil {
		t.Fatalf("pr merge: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "Merging pull request 7 in manovaspace/orbit-test on GitHub (merge)...") {
		t.Errorf("expected the merge progress line, got: %s", out)
	}
	if !strings.Contains(out, `Pull request 7 merged using method "merge"`) {
		t.Errorf("expected the merge result line, got: %s", out)
	}
	if strings.Contains(out, "schemaVersion") {
		t.Errorf("human output must not be a JSON envelope: %s", out)
	}
}
