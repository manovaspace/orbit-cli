package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/pr/domain"
)

// fakeForge answers the forge port from scripted checks and records calls.
type fakeForge struct {
	checks     [][]domain.Check
	checksErr  error
	created    domain.PullRequest
	createErr  error
	mergeErr   error
	commentErr error
	open       []domain.PullRequest
	listErr    error

	calls []string

	lastCreateTitle string
	lastCreateBody  string
}

func (f *fakeForge) CreatePR(_ context.Context, _ domain.Repo, title, body, head, base string) (domain.PullRequest, error) {
	f.calls = append(f.calls, "create")
	if f.createErr != nil {
		return domain.PullRequest{}, f.createErr
	}
	f.lastCreateTitle, f.lastCreateBody = title, body
	created := f.created
	if created.Number == 0 {
		created.Number = 7
	}
	if created.URL == "" {
		created.URL = "https://github.com/manovaspace/orbit-test/pull/7"
	}
	created.Title, created.Head, created.Base = title, head, base
	return created, nil
}

func (f *fakeForge) MergePR(_ context.Context, _ domain.Repo, ref string, method domain.Method) error {
	f.calls = append(f.calls, "merge")
	return f.mergeErr
}

func (f *fakeForge) Checks(_ context.Context, _ domain.Repo, _ string) ([]domain.Check, error) {
	f.calls = append(f.calls, "checks")
	if f.checksErr != nil {
		return nil, f.checksErr
	}
	if len(f.checks) == 0 {
		return []domain.Check{}, nil
	}
	next := f.checks[0]
	if len(f.checks) > 1 {
		f.checks = f.checks[1:]
	}
	return next, nil
}

func (f *fakeForge) Comment(_ context.Context, _ domain.Repo, _ string, body string) error {
	f.calls = append(f.calls, "comment:"+body)
	return f.commentErr
}

func (f *fakeForge) ListOpen(_ context.Context, _ domain.Repo) ([]domain.PullRequest, error) {
	f.calls = append(f.calls, "list")
	return f.open, f.listErr
}

func (f *fakeForge) count(prefix string) int {
	n := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

// fakeGit answers the git port; the zero value is a clean GitHub checkout of
// feat/gate-test on main.
type fakeGit struct {
	info     domain.RepoInfo
	repoErr  error
	dirty    bool
	cleanErr error

	lastSubject string
	lastBody    string
	lastErr     error

	pushErr     error
	checkoutErr error
	pullErr     error
	deleteErr   error

	calls []string
}

func newFakeGit() *fakeGit {
	return &fakeGit{
		info: domain.RepoInfo{
			Repo:          domain.Repo{Owner: "manovaspace", Name: "orbit-test"},
			CurrentBranch: "feat/gate-test",
			RemoteURL:     "git@github.com:manovaspace/orbit-test.git",
			IsGitHub:      true,
		},
		lastSubject: "feat: gate test change",
	}
}

func (g *fakeGit) Repo(_ context.Context, _ string) (domain.RepoInfo, error) {
	g.calls = append(g.calls, "repo")
	return g.info, g.repoErr
}

func (g *fakeGit) IsClean(_ context.Context, _ string) (bool, error) {
	g.calls = append(g.calls, "clean")
	return !g.dirty, g.cleanErr
}

func (g *fakeGit) LastCommit(_ context.Context, _, _ string) (string, string, error) {
	g.calls = append(g.calls, "lastcommit")
	if g.lastErr != nil {
		return "", "", g.lastErr
	}
	return g.lastSubject, g.lastBody, nil
}

func (g *fakeGit) Push(_ context.Context, _, _ string) error {
	g.calls = append(g.calls, "push")
	return g.pushErr
}

func (g *fakeGit) Checkout(_ context.Context, _, _ string) error {
	g.calls = append(g.calls, "checkout")
	return g.checkoutErr
}

func (g *fakeGit) PullFFOnly(_ context.Context, _ string) error {
	g.calls = append(g.calls, "pull")
	return g.pullErr
}

func (g *fakeGit) DeleteBranch(_ context.Context, _, _ string) error {
	g.calls = append(g.calls, "delete")
	return g.deleteErr
}

// fakeClock advances on every sleep so waiting tests run instantly.
type fakeClock struct {
	now    time.Time
	sleeps int
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) {
	c.sleeps++
	c.now = c.now.Add(d)
}

func newTestService(forge *fakeForge, git *fakeGit) (*Service, *bytes.Buffer) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	out := new(bytes.Buffer)
	return &Service{
		Forge:          forge,
		Git:            git,
		Out:            out,
		Sleep:          clock.Sleep,
		Now:            clock.Now,
		PollInterval:   time.Second,
		RegisterWindow: 5 * time.Second,
	}, out
}

func mergeOptions() domain.MergeOptions {
	return domain.MergeOptions{Wait: false, WaitTimeout: time.Minute}
}

func TestMergeAllowsGreen(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{{{Name: "ci", Bucket: "pass"}, {Name: "docs", Bucket: "skipping"}}}}
	git := newFakeGit()
	svc, _ := newTestService(forge, git)

	if _, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, mergeOptions()); err != nil {
		t.Fatalf("expected merge to succeed, got: %v", err)
	}
	if forge.count("merge") != 1 {
		t.Errorf("expected one merge call, got: %v", forge.calls)
	}
	if forge.count("comment") != 0 {
		t.Errorf("expected no comment call, got: %v", forge.calls)
	}
}

func TestMergeRefusesRed(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{{{Name: "ci", Bucket: "fail"}}}}
	svc, _ := newTestService(forge, newFakeGit())

	_, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, mergeOptions())
	if err == nil {
		t.Fatal("expected merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the failing check, got: %v", err)
	}
	if got := clierr.Code(err); got != clierr.CodeFailed {
		t.Errorf("error code = %q, want %q", got, clierr.CodeFailed)
	}
	if forge.count("merge") != 0 {
		t.Errorf("expected no merge call, got: %v", forge.calls)
	}
}

func TestMergeRefusesPending(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{{{Name: "ci", Bucket: "pending"}}}}
	svc, _ := newTestService(forge, newFakeGit())

	_, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, mergeOptions())
	if err == nil {
		t.Fatal("expected merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the pending check, got: %v", err)
	}
	if forge.count("merge") != 0 {
		t.Errorf("expected no merge call, got: %v", forge.calls)
	}
	if got := forge.count("checks"); got != 1 {
		t.Errorf("expected exactly one checks poll, got %d", got)
	}
}

func TestMergeRefusesNoChecks(t *testing.T) {
	forge := &fakeForge{}
	svc, _ := newTestService(forge, newFakeGit())

	_, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, mergeOptions())
	if err == nil {
		t.Fatal("expected merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "no checks reported") {
		t.Errorf("expected error to mention 'no checks reported', got: %v", err)
	}
	if forge.count("merge") != 0 {
		t.Errorf("expected no merge call, got: %v", forge.calls)
	}
}

func TestMergeAllowsNoChecksWithFlag(t *testing.T) {
	forge := &fakeForge{}
	svc, _ := newTestService(forge, newFakeGit())
	opts := mergeOptions()
	opts.AllowNoChecks = true

	if _, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, opts); err != nil {
		t.Fatalf("expected merge to succeed with --allow-no-checks, got: %v", err)
	}
	if forge.count("merge") != 1 {
		t.Errorf("expected one merge call, got: %v", forge.calls)
	}
}

func TestMergeRefusesWhenChecksCannotBeRead(t *testing.T) {
	forge := &fakeForge{checksErr: errors.New("boom: unexpected failure")}
	svc, _ := newTestService(forge, newFakeGit())

	_, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, mergeOptions())
	if err == nil {
		t.Fatal("expected merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "could not read checks") {
		t.Errorf("expected error to mention 'could not read checks', got: %v", err)
	}
	if forge.count("merge") != 0 {
		t.Errorf("expected no merge call, got: %v", forge.calls)
	}
}

func TestMergeOverrideCommentsThenMerges(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{{{Name: "ci", Bucket: "fail"}}}}
	svc, _ := newTestService(forge, newFakeGit())
	opts := mergeOptions()
	opts.Override = "hotfix for outage"
	opts.OverrideSet = true

	if _, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, opts); err != nil {
		t.Fatalf("expected override merge to succeed, got: %v", err)
	}

	commentIdx, mergeIdx := -1, -1
	for i, call := range forge.calls {
		if strings.HasPrefix(call, "comment") && commentIdx == -1 {
			commentIdx = i
		}
		if call == "merge" && mergeIdx == -1 {
			mergeIdx = i
		}
	}
	if commentIdx == -1 {
		t.Fatalf("expected a comment call, got: %v", forge.calls)
	}
	if mergeIdx == -1 {
		t.Fatalf("expected a merge call, got: %v", forge.calls)
	}
	if commentIdx > mergeIdx {
		t.Errorf("expected comment before merge, got: %v", forge.calls)
	}
	if got := forge.count("comment"); got != 1 {
		t.Errorf("expected exactly one comment, got %d", got)
	}
	if !strings.Contains(forge.calls[commentIdx], "override: hotfix for outage") {
		t.Errorf("expected comment body to record the override, got: %s", forge.calls[commentIdx])
	}
}

func TestMergeOverrideCommentFailureBlocksMerge(t *testing.T) {
	forge := &fakeForge{
		checks:     [][]domain.Check{{{Name: "ci", Bucket: "fail"}}},
		commentErr: errors.New("comment failed"),
	}
	svc, _ := newTestService(forge, newFakeGit())
	opts := mergeOptions()
	opts.Override = "hotfix for outage"
	opts.OverrideSet = true

	_, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, opts)
	if err == nil {
		t.Fatal("expected error when override comment fails, got nil")
	}
	if forge.count("merge") != 0 {
		t.Errorf("expected no merge call, got: %v", forge.calls)
	}
}

func TestMergeOverrideBlankRejected(t *testing.T) {
	forge := &fakeForge{}
	svc, _ := newTestService(forge, newFakeGit())
	opts := mergeOptions()
	opts.Override = "  "
	opts.OverrideSet = true

	_, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, opts)
	if err == nil {
		t.Fatal("expected error for blank override, got nil")
	}
	if !strings.Contains(err.Error(), "--override requires a non-empty reason") {
		t.Errorf("unexpected error message: %v", err)
	}
	if got := clierr.Code(err); got != clierr.CodeUsage {
		t.Errorf("error code = %q, want %q", got, clierr.CodeUsage)
	}
	if len(forge.calls) != 0 {
		t.Errorf("expected no forge calls, got: %v", forge.calls)
	}
}

func TestAllowNoChecksDoesNotOverrideFailure(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{{{Name: "ci", Bucket: "fail"}}}}
	svc, _ := newTestService(forge, newFakeGit())
	opts := mergeOptions()
	opts.AllowNoChecks = true

	_, err := svc.Merge(context.Background(), ".", "7", domain.MethodMerge, opts)
	if err == nil {
		t.Fatal("expected merge to be refused despite --allow-no-checks, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the failing check, got: %v", err)
	}
	if forge.count("merge") != 0 {
		t.Errorf("expected no merge call, got: %v", forge.calls)
	}
}

func TestMergeUsesCurrentBranchWhenRefBlank(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{{{Name: "ci", Bucket: "pass"}}}}
	git := newFakeGit()
	svc, out := newTestService(forge, git)

	res, err := svc.Merge(context.Background(), ".", "", domain.MethodSquash, mergeOptions())
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if res.Ref != "feat/gate-test" {
		t.Errorf("ref = %q, want the current branch", res.Ref)
	}
	if !strings.Contains(out.String(), "Merging pull request feat/gate-test in manovaspace/orbit-test on GitHub (squash)...") {
		t.Errorf("unexpected output: %s", out.String())
	}
}

func TestMergeRefusesBlankRefOnDetachedHead(t *testing.T) {
	git := newFakeGit()
	git.info.CurrentBranch = "HEAD"
	svc, _ := newTestService(&fakeForge{}, git)

	_, err := svc.Merge(context.Background(), ".", "", domain.MethodMerge, mergeOptions())
	if err == nil {
		t.Fatal("expected error on detached HEAD, got nil")
	}
	if !strings.Contains(err.Error(), "cannot determine current branch") {
		t.Errorf("unexpected error: %v", err)
	}
}

func deliverOptions() domain.MergeOptions {
	return domain.MergeOptions{Wait: true, WaitTimeout: 40 * time.Second}
}

func TestDeliverWaitsThenRefusesRed(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{
		{{Name: "ci", Bucket: "pending"}},
		{{Name: "ci", Bucket: "fail"}},
	}}
	svc, _ := newTestService(forge, newFakeGit())

	_, err := svc.Deliver(context.Background(), ".", DeliverInput{Method: domain.MethodMerge, Gate: deliverOptions()})
	if err == nil {
		t.Fatal("expected deliver to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the failing check, got: %v", err)
	}
	if forge.count("merge") != 0 {
		t.Errorf("expected no merge call, got: %v", forge.calls)
	}
	if got := forge.count("checks"); got < 2 {
		t.Errorf("expected deliver to poll checks at least twice, got %d", got)
	}
}

func TestDeliverWaitsThenMergesGreen(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{
		{{Name: "ci", Bucket: "pending"}},
		{{Name: "ci", Bucket: "pass"}},
	}}
	svc, out := newTestService(forge, newFakeGit())

	if _, err := svc.Deliver(context.Background(), ".", DeliverInput{Method: domain.MethodMerge, Gate: deliverOptions()}); err != nil {
		t.Fatalf("expected deliver to succeed, got: %v\nOutput: %s", err, out.String())
	}
	if !strings.Contains(out.String(), "Successfully delivered feat/gate-test into main!") {
		t.Errorf("expected success message, got: %s", out.String())
	}
	if forge.count("merge") != 1 {
		t.Errorf("expected a merge call, got: %v", forge.calls)
	}
}

func TestDeliverWaitsForLateRegisteringCheck(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{
		{{Name: "ci", Bucket: "pass"}},
		{{Name: "ci", Bucket: "pass"}, {Name: "lint", Bucket: "fail"}},
	}}
	svc, _ := newTestService(forge, newFakeGit())

	_, err := svc.Deliver(context.Background(), ".", DeliverInput{Method: domain.MethodMerge, Gate: deliverOptions()})
	if err == nil {
		t.Fatal("expected deliver to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "lint") {
		t.Errorf("expected error to name the late-registering check, got: %v", err)
	}
	if forge.count("merge") != 0 {
		t.Errorf("expected no merge call, got: %v", forge.calls)
	}
	if got := forge.count("checks"); got < 3 {
		t.Errorf("expected deliver to poll checks at least three times, got %d", got)
	}
}

func TestDeliverMergesWhenChecksStable(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{
		{{Name: "ci", Bucket: "pass"}},
		{{Name: "ci", Bucket: "pass"}, {Name: "lint", Bucket: "pass"}},
	}}
	svc, _ := newTestService(forge, newFakeGit())

	if _, err := svc.Deliver(context.Background(), ".", DeliverInput{Method: domain.MethodMerge, Gate: deliverOptions()}); err != nil {
		t.Fatalf("expected deliver to succeed, got: %v", err)
	}
	if forge.count("merge") != 1 {
		t.Errorf("expected a merge call, got: %v", forge.calls)
	}
	if got := forge.count("checks"); got < 3 {
		t.Errorf("expected deliver to poll checks at least three times, got %d", got)
	}
}

func TestDeliverRefusesBaseBranchDetachedAndDirty(t *testing.T) {
	t.Run("BaseBranch", func(t *testing.T) {
		git := newFakeGit()
		git.info.CurrentBranch = "main"
		svc, _ := newTestService(&fakeForge{}, git)

		_, err := svc.Deliver(context.Background(), ".", DeliverInput{Base: "main", Method: domain.MethodMerge})
		if err == nil || !strings.Contains(err.Error(), `cannot deliver from "main" branch`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("DetachedHead", func(t *testing.T) {
		git := newFakeGit()
		git.info.CurrentBranch = "HEAD"
		svc, _ := newTestService(&fakeForge{}, git)

		_, err := svc.Deliver(context.Background(), ".", DeliverInput{Base: "main", Method: domain.MethodMerge})
		if err == nil || !strings.Contains(err.Error(), "detached HEAD") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("DirtyTree", func(t *testing.T) {
		git := newFakeGit()
		git.dirty = true
		svc, _ := newTestService(&fakeForge{}, git)

		_, err := svc.Deliver(context.Background(), ".", DeliverInput{Base: "main", Method: domain.MethodMerge})
		if err == nil || !strings.Contains(err.Error(), "working tree has uncommitted changes") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestDeliverUsesLastCommitMessages(t *testing.T) {
	git := newFakeGit()
	git.lastSubject = "feat(core): implement shiny feature"
	git.lastBody = "Detailed body"
	forge := &fakeForge{checks: [][]domain.Check{{{Name: "ci", Bucket: "pass"}}}}
	svc, _ := newTestService(forge, git)

	if _, err := svc.Deliver(context.Background(), ".", DeliverInput{Method: domain.MethodMerge, Gate: deliverOptions()}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if forge.lastCreateTitle != "feat(core): implement shiny feature" {
		t.Errorf("create title = %q, want the commit subject", forge.lastCreateTitle)
	}
	if forge.lastCreateBody != "Detailed body" {
		t.Errorf("create body = %q, want the commit body", forge.lastCreateBody)
	}
}

func TestDeliverPrunesBranchAndFastForwards(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{{{Name: "ci", Bucket: "pass"}}}}
	git := newFakeGit()
	svc, out := newTestService(forge, git)

	if _, err := svc.Deliver(context.Background(), ".", DeliverInput{Method: domain.MethodMerge, Gate: deliverOptions()}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	for _, want := range []string{"push", "checkout", "pull", "delete"} {
		found := false
		for _, call := range git.calls {
			if call == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a %s call, got: %v", want, git.calls)
		}
	}
	if !strings.Contains(out.String(), "Successfully delivered feat/gate-test into main!") {
		t.Errorf("expected success message, got: %s", out.String())
	}
}

func TestCreateMergeUsesGate(t *testing.T) {
	forge := &fakeForge{checks: [][]domain.Check{{{Name: "ci", Bucket: "fail"}}}}
	git := newFakeGit()
	git.lastSubject = "feat: gate create"
	svc, _ := newTestService(forge, git)

	_, err := svc.Create(context.Background(), ".", CreateInput{
		Title:  "feat: gate create",
		Merge:  true,
		Method: domain.MethodMerge,
		Gate:   domain.MergeOptions{Wait: true, WaitTimeout: 40 * time.Second},
	})
	if err == nil {
		t.Fatal("expected create --merge to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "ci") {
		t.Errorf("expected error to name the failing check, got: %v", err)
	}
	if forge.count("merge") != 0 {
		t.Errorf("expected no merge call, got: %v", forge.calls)
	}
	if forge.count("create") != 1 {
		t.Errorf("expected a create call, got: %v", forge.calls)
	}
}

func TestCreatePushesThenCreates(t *testing.T) {
	forge := &fakeForge{}
	git := newFakeGit()
	git.lastSubject = "feat: add thing"
	svc, out := newTestService(forge, git)

	res, err := svc.Create(context.Background(), ".", CreateInput{Method: domain.MethodMerge})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Branch != "feat/gate-test" || res.Base != "main" {
		t.Errorf("unexpected result: %+v", res)
	}
	if res.PullRequest.Title != "feat: add thing" {
		t.Errorf("title = %q, want the commit subject", res.PullRequest.Title)
	}
	if got := strings.Join(git.calls, ","); got != "repo,lastcommit,push" {
		t.Errorf("git calls = %s, want repo,lastcommit,push", got)
	}
	if forge.count("create") != 1 {
		t.Errorf("expected a create call, got: %v", forge.calls)
	}
	if !strings.Contains(out.String(), "Pull request created: https://github.com/manovaspace/orbit-test/pull/7") {
		t.Errorf("unexpected output: %s", out.String())
	}
}

func TestCreateRefusesSameHeadAndBase(t *testing.T) {
	git := newFakeGit()
	svc, _ := newTestService(&fakeForge{}, git)

	_, err := svc.Create(context.Background(), ".", CreateInput{Base: "feat/gate-test", Method: domain.MethodMerge})
	if err == nil || !strings.Contains(err.Error(), "cannot be the same as base branch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	git := newFakeGit()
	git.lastSubject = ""
	svc, _ := newTestService(&fakeForge{}, git)

	_, err := svc.Create(context.Background(), ".", CreateInput{Method: domain.MethodMerge})
	if err == nil || !strings.Contains(err.Error(), "PR title is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusListsOpenPullRequests(t *testing.T) {
	forge := &fakeForge{open: []domain.PullRequest{{Number: 12, Title: "feat: x", Head: "feat/x", Base: "main", Author: "opmc", URL: "https://github.com/manovaspace/orbit-test/pull/12"}}}
	svc, _ := newTestService(forge, newFakeGit())

	res, err := svc.Status(context.Background(), ".")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Repo.String() != "manovaspace/orbit-test" {
		t.Errorf("repo = %s", res.Repo)
	}
	if len(res.PullRequests) != 1 || res.PullRequests[0].Number != 12 {
		t.Errorf("unexpected pull requests: %+v", res.PullRequests)
	}
	if res.PullRequests[0].Author != "opmc" {
		t.Errorf("author = %q, want opmc", res.PullRequests[0].Author)
	}
}
