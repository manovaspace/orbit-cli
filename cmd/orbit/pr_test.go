package main

import (
	"bytes"
	"encoding/json"
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
