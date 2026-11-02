package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	prdomain "github.com/manovaspace/orbit-cli/pkg/pr/domain"
)

// fakeWorkspace returns a fixed repository list.
type fakeWorkspace struct {
	repos []Repo
	err   error
}

func (w *fakeWorkspace) Repos(_, _ string) ([]Repo, error) { return w.repos, w.err }

// fakeFiles serves files from maps.
type fakeFiles struct {
	files map[string]string
	dirs  map[string][]string
}

func (f *fakeFiles) ReadFile(path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return []byte(data), nil
}

func (f *fakeFiles) Exists(path string) bool {
	_, ok := f.files[path]
	return ok
}

func (f *fakeFiles) ReadDir(path string) ([]string, error) { return f.dirs[path], nil }

// fakeGit answers local ref reads.
type fakeGit struct {
	refs map[string]string
	tags string
	err  error
}

func (g *fakeGit) RevParse(_ context.Context, _ string, ref string) (string, error) {
	if g.err != nil {
		return "", g.err
	}
	if sha, ok := g.refs[ref]; ok {
		return sha, nil
	}
	return "", errors.New("unknown ref " + ref)
}

func (g *fakeGit) LatestTag(_ context.Context, _ string) (string, error) {
	if g.err != nil {
		return "", g.err
	}
	return g.tags, nil
}

// fakeForge records the release forge calls.
type fakeForge struct {
	open     []PullRequest
	listErr  error
	checkSeq [][]prdomain.Check
	checks   []prdomain.Check
	checkErr error

	commitChecks []prdomain.Check
	commitErr    error

	branches    map[string]string
	branchErr   error
	created     PullRequest
	createErr   error
	mergeErr    error
	commentErr  error
	runs        []WorkflowRun
	runsErr     error
	dispatchErr error
	compare     map[string]int
	compareErr  error

	calls []string
}

func (f *fakeForge) ListOpen(_ context.Context, _ string) ([]PullRequest, error) {
	f.calls = append(f.calls, "list")
	return f.open, f.listErr
}

func (f *fakeForge) Checks(_ context.Context, _, ref string) ([]prdomain.Check, error) {
	f.calls = append(f.calls, "checks:"+ref)
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	if len(f.checkSeq) > 0 {
		next := f.checkSeq[0]
		if len(f.checkSeq) > 1 {
			f.checkSeq = f.checkSeq[1:]
		}
		return next, nil
	}
	return f.checks, nil
}

func (f *fakeForge) CommitChecks(_ context.Context, _, sha string) ([]prdomain.Check, error) {
	f.calls = append(f.calls, "commit-checks:"+sha)
	return f.commitChecks, f.commitErr
}

func (f *fakeForge) MergePR(_ context.Context, _, ref string, method prdomain.Method) error {
	f.calls = append(f.calls, "merge:"+ref+":"+string(method))
	return f.mergeErr
}

func (f *fakeForge) Comment(_ context.Context, _, ref, body string) error {
	f.calls = append(f.calls, "comment:"+ref+":"+body)
	return f.commentErr
}

func (f *fakeForge) BranchSHA(_ context.Context, _, branch string) (string, bool, error) {
	f.calls = append(f.calls, "branch:"+branch)
	if f.branchErr != nil {
		return "", false, f.branchErr
	}
	sha, ok := f.branches[branch]
	return sha, ok, nil
}

func (f *fakeForge) CreateBranch(_ context.Context, _, branch, sha string) error {
	f.calls = append(f.calls, "create-branch:"+branch+":"+sha)
	return nil
}

func (f *fakeForge) CreatePR(_ context.Context, _, title, body, head, base string) (PullRequest, error) {
	f.calls = append(f.calls, "create-pr:"+head+"->"+base)
	if f.createErr != nil {
		return PullRequest{}, f.createErr
	}
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

func (f *fakeForge) Runs(_ context.Context, _ string) ([]WorkflowRun, error) {
	f.calls = append(f.calls, "runs")
	return f.runs, f.runsErr
}

func (f *fakeForge) Dispatch(_ context.Context, _, workflow, ref string) error {
	f.calls = append(f.calls, "dispatch:"+workflow+":"+ref)
	return f.dispatchErr
}

func (f *fakeForge) CompareAhead(_ context.Context, _, base, head string) (int, error) {
	f.calls = append(f.calls, "compare:"+base+"..."+head)
	if f.compareErr != nil {
		return 0, f.compareErr
	}
	return f.compare[base+"..."+head], nil
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

// fakeLocal prepares the local release tier.
type fakeLocal struct {
	env          map[string]string
	reachableErr error
	createErr    error
	dropErr      error
	created      []string
	dropped      []string
}

func (l *fakeLocal) Reachable(context.Context, string) error { return l.reachableErr }

func (l *fakeLocal) CreateDatabase(_ context.Context, _, name string) error {
	if l.createErr != nil {
		return l.createErr
	}
	l.created = append(l.created, name)
	return nil
}

func (l *fakeLocal) DropDatabase(_ context.Context, _, name string) error {
	l.dropped = append(l.dropped, name)
	return l.dropErr
}

func (l *fakeLocal) Env(_, _ string) (map[string]string, error) {
	if l.env == nil {
		return map[string]string{"TEST_DATABASE_URL": "postgres://dev/secret", "CI_TIER": "release"}, nil
	}
	return l.env, nil
}

// fakeTester records the environment and returns a scripted error.
type fakeTester struct {
	err error
	env map[string]string
	dir string
}

func (t *fakeTester) Run(_ context.Context, dir string, env map[string]string) error {
	t.dir = dir
	t.env = env
	return t.err
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time        { return c.now }
func (c *fakeClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

func testRepo() Repo {
	return Repo{
		Name:          "orbit-cli",
		Path:          "orbit/orbit-cli",
		Dir:           "/ws/orbit/orbit-cli",
		DefaultBranch: "main",
		Scope:         "orbit",
		Slug:          "manovaspace/orbit-cli",
	}
}

func newTestService(forge *fakeForge) (*Service, *bytes.Buffer, *fakeLocal, *fakeTester) {
	out := new(bytes.Buffer)
	local := &fakeLocal{}
	tester := &fakeTester{}
	clock := &fakeClock{now: time.Unix(0, 0)}
	files := &fakeFiles{files: map[string]string{
		"/ws/orbit/orbit-cli/.github/workflows/release.yml": "",
	}}
	return &Service{
		Workspace:       &fakeWorkspace{repos: []Repo{testRepo()}},
		Files:           files,
		Git:             &fakeGit{refs: map[string]string{"origin/main": "abcdef123456"}, tags: "v1.2.3"},
		Forge:           forge,
		Local:           local,
		Tester:          tester,
		Out:             out,
		Root:            "/ws",
		ManifestPath:    "/ws/workspace.yaml",
		Sleep:           clock.Sleep,
		Now:             clock.Now,
		PollInterval:    time.Millisecond,
		RegisterWindow:  5 * time.Millisecond,
		WaitTimeout:     time.Minute,
		NewDatabaseName: func() string { return "test_abc123" },
	}, out, local, tester
}

func releasePR() PullRequest {
	return PullRequest{Number: 12, URL: "https://github.com/manovaspace/orbit-cli/pull/12", Title: "chore: version packages", Head: "changeset-release/main", Base: "main"}
}

func TestStatusReportsReleasePRDemoAndRun(t *testing.T) {
	forge := &fakeForge{
		open:     []PullRequest{releasePR()},
		checks:   []prdomain.Check{{Name: "release-check", Bucket: "pass"}},
		branches: map[string]string{"demo": "def4567890"},
		compare:  map[string]int{"demo...main": 3},
		runs:     []WorkflowRun{{Name: "CI", Conclusion: "success"}, {Name: "Publish", Status: "completed", Conclusion: "success", URL: "https://github.com/run/1"}},
	}
	svc, _, _, _ := newTestService(forge)

	result, err := svc.Status(context.Background(), "orbit-cli")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(result.Repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(result.Repos))
	}
	got := result.Repos[0]
	if got.HeadSHA != "abcdef123456" || got.LatestTag != "v1.2.3" {
		t.Errorf("branch data = %+v", got)
	}
	if got.ReleasePR == nil || got.ReleasePR.CheckState != "pass" {
		t.Errorf("release PR = %+v", got.ReleasePR)
	}
	if !got.Demo.Exists || got.Demo.Behind != 3 {
		t.Errorf("demo = %+v", got.Demo)
	}
	if got.LastRun == nil || got.LastRun.Name != "Publish" {
		t.Errorf("last run = %+v", got.LastRun)
	}
	if len(got.Unverified) != 0 {
		t.Errorf("unverified = %v", got.Unverified)
	}
}

func TestStatusUnverified(t *testing.T) {
	forge := &fakeForge{listErr: errors.New("boom")}
	svc, _, _, _ := newTestService(forge)

	_, err := svc.Status(context.Background(), "orbit-cli")
	if err == nil {
		t.Fatal("expected an unverified error")
	}
	if clierr.Code(err) != clierr.CodeUnverified {
		t.Errorf("code = %q", clierr.Code(err))
	}
}

func TestCheckRemoteWaitsForStableGreen(t *testing.T) {
	forge := &fakeForge{
		open:     []PullRequest{releasePR()},
		checkSeq: [][]prdomain.Check{{{Name: "release-check", Bucket: "pass"}}, {{Name: "release-check", Bucket: "pass"}}},
	}
	svc, _, _, _ := newTestService(forge)

	result, err := svc.Check(context.Background(), "orbit-cli", false, prdomain.MergeOptions{Wait: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].State != "pass" {
		t.Fatalf("result = %+v", result)
	}
	if forge.count("checks:12") < 2 {
		t.Errorf("expected two polls, got %v", forge.calls)
	}
}

func TestCheckRemoteRefusesFailingPendingAndMissing(t *testing.T) {
	t.Run("Failing", func(t *testing.T) {
		forge := &fakeForge{open: []PullRequest{releasePR()}, checks: []prdomain.Check{{Name: "release-check", Bucket: "fail"}}}
		svc, _, _, _ := newTestService(forge)
		_, err := svc.Check(context.Background(), "orbit-cli", false, prdomain.MergeOptions{})
		if err == nil || clierr.Code(err) != clierr.CodeFailed {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("Pending", func(t *testing.T) {
		forge := &fakeForge{open: []PullRequest{releasePR()}, checks: []prdomain.Check{{Name: "release-check", Bucket: "pending"}}}
		svc, _, _, _ := newTestService(forge)
		_, err := svc.Check(context.Background(), "orbit-cli", false, prdomain.MergeOptions{})
		if err == nil || clierr.Code(err) != clierr.CodeFailed {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("NoPR", func(t *testing.T) {
		forge := &fakeForge{}
		svc, _, _, _ := newTestService(forge)
		_, err := svc.Check(context.Background(), "orbit-cli", false, prdomain.MergeOptions{})
		if err == nil || clierr.Code(err) != clierr.CodeUnverified {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("NoChecks", func(t *testing.T) {
		forge := &fakeForge{open: []PullRequest{releasePR()}}
		svc, _, _, _ := newTestService(forge)
		_, err := svc.Check(context.Background(), "orbit-cli", false, prdomain.MergeOptions{Wait: true})
		if err == nil || clierr.Code(err) != clierr.CodeUnverified {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("Unreadable", func(t *testing.T) {
		forge := &fakeForge{open: []PullRequest{releasePR()}, checkErr: errors.New("boom")}
		svc, _, _, _ := newTestService(forge)
		_, err := svc.Check(context.Background(), "orbit-cli", false, prdomain.MergeOptions{})
		if err == nil || clierr.Code(err) != clierr.CodeFailed {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCheckLocalDropsDatabaseWhenTestFails(t *testing.T) {
	forge := &fakeForge{}
	svc, _, local, tester := newTestService(forge)
	tester.err = errors.New("tests failed")

	result, err := svc.Check(context.Background(), "orbit-cli", true, prdomain.MergeOptions{})
	if err == nil || clierr.Code(err) != clierr.CodeFailed {
		t.Fatalf("err = %v", err)
	}
	if len(local.created) != 1 || len(local.dropped) != 1 {
		t.Fatalf("created = %v, dropped = %v", local.created, local.dropped)
	}
	if local.created[0] != local.dropped[0] {
		t.Errorf("drop %q did not match create %q", local.dropped[0], local.created[0])
	}
	if tester.env["CI_TIER"] != "release" || tester.env["TEST_DATABASE_URL"] == "" {
		t.Errorf("env = %v", tester.env)
	}
	if result.Results[0].Database != "test_abc123" {
		t.Errorf("database = %q", result.Results[0].Database)
	}
}

func TestCheckLocalUnreachableIsUnverified(t *testing.T) {
	svc, _, local, _ := newTestService(&fakeForge{})
	local.reachableErr = clierr.Unverified("dev Postgres is unreachable")

	_, err := svc.Check(context.Background(), "orbit-cli", true, prdomain.MergeOptions{})
	if err == nil || clierr.Code(err) != clierr.CodeUnverified {
		t.Fatalf("err = %v", err)
	}
	if len(local.created) != 0 || len(local.dropped) != 0 {
		t.Errorf("created = %v, dropped = %v", local.created, local.dropped)
	}
}

func TestDemoPromotesMainOnly(t *testing.T) {
	forge := &fakeForge{
		branches:     map[string]string{"main": "abc123456789", "demo": "def456789012"},
		commitChecks: []prdomain.Check{{Name: "ci", Bucket: "pass"}},
	}
	svc, _, _, _ := newTestService(forge)

	result, err := svc.Demo(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{})
	if err != nil {
		t.Fatalf("Demo: %v", err)
	}
	if result.MainSHA != "abc123456789" || result.PullRequest == nil || !result.Merged {
		t.Fatalf("result = %+v", result)
	}
	if forge.count("commit-checks:abc123456789") != 1 {
		t.Errorf("expected the gate to read the main commit, got %v", forge.calls)
	}
	found := false
	for _, call := range forge.calls {
		if call == "create-pr:main->demo" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a main -> demo pull request, got %v", forge.calls)
	}
}

func TestDemoCreatesMissingBranchFromMain(t *testing.T) {
	forge := &fakeForge{branches: map[string]string{"main": "abc123456789"}}
	svc, _, _, _ := newTestService(forge)

	result, err := svc.Demo(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{})
	if err != nil {
		t.Fatalf("Demo: %v", err)
	}
	if !result.CreatedBranch || !result.UpToDate || result.PullRequest != nil {
		t.Fatalf("result = %+v", result)
	}
	if forge.count("create-branch:demo:abc123456789") != 1 {
		t.Errorf("expected demo created from the main SHA, got %v", forge.calls)
	}
}

func TestDemoRefusesFailingPendingAndUnreadableChecks(t *testing.T) {
	t.Run("Failing", func(t *testing.T) {
		forge := &fakeForge{branches: map[string]string{"main": "abc", "demo": "def"}, commitChecks: []prdomain.Check{{Name: "ci", Bucket: "fail"}}}
		svc, _, _, _ := newTestService(forge)
		_, err := svc.Demo(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{})
		if err == nil || clierr.Code(err) != clierr.CodeFailed {
			t.Fatalf("err = %v", err)
		}
		if forge.count("create-pr") != 0 {
			t.Errorf("must refuse before opening a PR: %v", forge.calls)
		}
	})
	t.Run("Pending", func(t *testing.T) {
		forge := &fakeForge{branches: map[string]string{"main": "abc", "demo": "def"}, commitChecks: []prdomain.Check{{Name: "ci", Bucket: "pending"}}}
		svc, _, _, _ := newTestService(forge)
		_, err := svc.Demo(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{})
		if err == nil || clierr.Code(err) != clierr.CodeFailed {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("Unreadable", func(t *testing.T) {
		forge := &fakeForge{branches: map[string]string{"main": "abc", "demo": "def"}, commitErr: errors.New("boom")}
		svc, _, _, _ := newTestService(forge)
		_, err := svc.Demo(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{})
		if err == nil || clierr.Code(err) != clierr.CodeUnverified {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDemoOverrideRecordsComment(t *testing.T) {
	forge := &fakeForge{
		branches:     map[string]string{"main": "abc", "demo": "def"},
		commitChecks: []prdomain.Check{{Name: "ci", Bucket: "fail"}},
	}
	svc, _, _, _ := newTestService(forge)

	_, err := svc.Demo(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{Override: "hotfix", OverrideSet: true})
	if err != nil {
		t.Fatalf("Demo: %v", err)
	}
	if forge.count("comment:7:override: hotfix") != 1 {
		t.Errorf("expected the override comment, got %v", forge.calls)
	}
	if forge.count("merge:7:merge") != 1 {
		t.Errorf("expected the merge, got %v", forge.calls)
	}
}

func TestProdMergesReleasePRAndDispatchesOnlyForLibraries(t *testing.T) {
	t.Run("Library", func(t *testing.T) {
		forge := &fakeForge{open: []PullRequest{releasePR()}, checks: []prdomain.Check{{Name: "release-check", Bucket: "pass"}}}
		svc, _, _, _ := newTestService(forge)
		svc.Files = &fakeFiles{files: map[string]string{
			"/ws/orbit/orbit-cli/.changeset/config.json": "",
		}}
		result, err := svc.Prod(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{})
		if err != nil {
			t.Fatalf("Prod: %v", err)
		}
		if !result.Library || !result.Merged || result.Dispatched != "publish.yml" {
			t.Fatalf("result = %+v", result)
		}
		if forge.count("dispatch:publish.yml:main") != 1 {
			t.Errorf("expected publish dispatch, got %v", forge.calls)
		}
		if !strings.Contains(result.OwnerApproval, "npm-publish") {
			t.Errorf("owner approval = %q", result.OwnerApproval)
		}
	})
	t.Run("Service", func(t *testing.T) {
		forge := &fakeForge{open: []PullRequest{releasePR()}, checks: []prdomain.Check{{Name: "release-check", Bucket: "pass"}}}
		svc, _, _, _ := newTestService(forge)
		result, err := svc.Prod(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{})
		if err != nil {
			t.Fatalf("Prod: %v", err)
		}
		if result.Library || result.Dispatched != "" {
			t.Fatalf("result = %+v", result)
		}
		if forge.count("dispatch") != 0 {
			t.Errorf("service must not dispatch publish: %v", forge.calls)
		}
	})
}

func TestProdRefusesWithoutReleasePRAndFailedChecks(t *testing.T) {
	t.Run("NoPR", func(t *testing.T) {
		svc, _, _, _ := newTestService(&fakeForge{})
		_, err := svc.Prod(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{})
		if err == nil || clierr.Code(err) != clierr.CodeFailed {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("FailedChecks", func(t *testing.T) {
		forge := &fakeForge{open: []PullRequest{releasePR()}, checks: []prdomain.Check{{Name: "release-check", Bucket: "fail"}}}
		svc, _, _, _ := newTestService(forge)
		_, err := svc.Prod(context.Background(), "orbit-cli", prdomain.MethodMerge, prdomain.MergeOptions{})
		if err == nil || clierr.Code(err) != clierr.CodeFailed {
			t.Fatalf("err = %v", err)
		}
		if forge.count("merge") != 0 {
			t.Errorf("must not merge: %v", forge.calls)
		}
	})
}
