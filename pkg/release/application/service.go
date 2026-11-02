package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	prdomain "github.com/manovaspace/orbit-cli/pkg/pr/domain"
	releasedomain "github.com/manovaspace/orbit-cli/pkg/release/domain"
)

// Timings used when the caller leaves the gate defaults unset.
const (
	DefaultPollInterval   = 10 * time.Second
	DefaultRegisterWindow = 60 * time.Second
	DefaultWaitTimeout    = 20 * time.Minute
)

// Status reports the release state of every selected repository. It is
// read-only and never fetches.
func (s *Service) Status(ctx context.Context, selector string) (StatusResult, error) {
	repos, err := s.selectRepos(selector, true)
	if err != nil {
		return StatusResult{}, err
	}

	result := StatusResult{Repos: make([]RepoStatus, 0, len(repos))}
	unverified := 0
	for _, repo := range repos {
		status := s.statusOne(ctx, repo)
		unverified += len(status.Unverified)
		result.Repos = append(result.Repos, status)
	}
	if unverified > 0 {
		return result, clierr.Unverified(fmt.Sprintf("release status could not verify %d item(s)", unverified))
	}
	return result, nil
}

// Check watches the release-tier check of every selected repository, or runs
// the tier locally against the existing dev services with --local.
func (s *Service) Check(ctx context.Context, selector string, local bool, opts prdomain.MergeOptions) (CheckOutput, error) {
	repos, err := s.selectRepos(selector, true)
	if err != nil {
		return CheckOutput{}, err
	}
	if len(repos) == 0 {
		return CheckOutput{}, clierr.Unverified("no repository with a release setup matches the selection")
	}

	result := CheckOutput{Results: make([]CheckResult, 0, len(repos))}
	var failed, unverified int
	for _, repo := range repos {
		var (
			checked CheckResult
			err     error
		)
		if local {
			checked, err = s.checkLocal(ctx, repo)
		} else {
			checked, err = s.checkRemote(ctx, repo, opts)
		}
		result.Results = append(result.Results, checked)
		switch clierr.ExitCode(err) {
		case clierr.ExitOK:
		case clierr.ExitFailed:
			failed++
		default:
			unverified++
		}
	}
	if failed > 0 {
		return result, clierr.Failed(fmt.Sprintf("release check failed for %d repository(ies)", failed))
	}
	if unverified > 0 {
		return result, clierr.Unverified(fmt.Sprintf("release check could not verify %d repository(ies)", unverified))
	}
	return result, nil
}

// Demo opens a pull request from main to demo, creates demo from main when it
// is missing, and merges it after the main commit's checks pass.
func (s *Service) Demo(ctx context.Context, selector string, method prdomain.Method, opts prdomain.MergeOptions) (DemoResult, error) {
	repo, err := s.selectOne(selector)
	if err != nil {
		return DemoResult{}, err
	}
	if repo.Slug == "" {
		return DemoResult{}, clierr.Failed(fmt.Sprintf("%s has no GitHub remote", repo.Name))
	}
	if opts.OverrideSet && strings.TrimSpace(opts.Override) == "" {
		return DemoResult{}, clierr.Usage("--override requires a non-empty reason")
	}

	mainSHA, exists, err := s.Forge.BranchSHA(ctx, repo.Slug, releasedomain.MainBranch)
	if err != nil {
		return DemoResult{}, withMessage(err, "demo refused: could not read the main branch")
	}
	if !exists {
		return DemoResult{}, clierr.Failed(fmt.Sprintf("demo refused: %s has no %s branch", repo.Slug, releasedomain.MainBranch))
	}

	result := DemoResult{Repo: repo.Name, Slug: repo.Slug, MainSHA: mainSHA}

	demoSHA, demoExists, err := s.Forge.BranchSHA(ctx, repo.Slug, releasedomain.DemoBranch)
	if err != nil {
		return result, withMessage(err, "demo refused: could not read the demo branch")
	}
	if !demoExists {
		if err := s.Forge.CreateBranch(ctx, repo.Slug, releasedomain.DemoBranch, mainSHA); err != nil {
			return result, withMessage(err, "demo refused: could not create the demo branch")
		}
		fmt.Fprintf(s.out(), "%s Created %s from %s at %s\n",
			s.icon(s.UI.OK), s.bold(releasedomain.DemoBranch), s.bold(releasedomain.MainBranch), shortSHA(mainSHA))
		result.CreatedBranch = true
		result.UpToDate = true
		return result, nil
	}
	if demoSHA == mainSHA {
		fmt.Fprintf(s.out(), "%s %s is already at %s\n", s.icon(s.UI.OK), s.bold(releasedomain.DemoBranch), shortSHA(mainSHA))
		result.UpToDate = true
		return result, nil
	}

	checks, err := s.Forge.CommitChecks(ctx, repo.Slug, mainSHA)
	if err != nil {
		return result, clierr.Unverified("demo refused: could not read the checks of " + shortSHA(mainSHA) + ": " + err.Error())
	}
	evaluation := prdomain.EvaluateChecks(checks)
	override := strings.TrimSpace(opts.Override)
	if !evaluation.Allowed && override == "" {
		return result, clierr.Failed("demo refused: " + evaluation.Reason + " (use --override \"reason\" to merge anyway)")
	}

	fmt.Fprintf(s.out(), "%s Opening a pull request from %s to %s...\n",
		s.icon(s.UI.Arrow), s.bold(releasedomain.MainBranch), s.bold(releasedomain.DemoBranch))
	created, err := s.Forge.CreatePR(ctx, repo.Slug,
		fmt.Sprintf("chore: promote %s to %s", releasedomain.MainBranch, releasedomain.DemoBranch),
		fmt.Sprintf("Promote %s (%s) to %s. The %s commit already passed CI.\n", releasedomain.MainBranch, shortSHA(mainSHA), releasedomain.DemoBranch, releasedomain.MainBranch),
		releasedomain.MainBranch, releasedomain.DemoBranch)
	if err != nil {
		return result, withMessage(err, "demo refused: could not open the pull request")
	}
	result.PullRequest = &created
	fmt.Fprintf(s.out(), "  %s Pull request #%d: %s\n", s.icon(s.UI.OK), created.Number, s.bold(created.URL))

	if !evaluation.Allowed {
		if err := s.Forge.Comment(ctx, repo.Slug, strconv.FormatInt(created.Number, 10), "override: "+override); err != nil {
			return result, withMessage(err, "recording override comment")
		}
		fmt.Fprintf(s.out(), "%s override recorded: %s\n", s.icon(s.UI.Warn), override)
	}

	if method == "" {
		method = prdomain.MethodMerge
	}
	if err := s.Forge.MergePR(ctx, repo.Slug, strconv.FormatInt(created.Number, 10), method); err != nil {
		return result, withMessage(err, "demo refused: could not merge the pull request")
	}
	result.Merged = true
	fmt.Fprintf(s.out(), "%s Merged %s into %s\n", s.icon(s.UI.OK), s.bold(releasedomain.MainBranch), s.bold(releasedomain.DemoBranch))
	return result, nil
}

// Prod merges the open release pull request through the merge gate and, for a
// library, dispatches the owner-gated publish workflow.
func (s *Service) Prod(ctx context.Context, selector string, method prdomain.Method, opts prdomain.MergeOptions) (ProdResult, error) {
	repo, err := s.selectOne(selector)
	if err != nil {
		return ProdResult{}, err
	}
	if repo.Slug == "" {
		return ProdResult{}, clierr.Failed(fmt.Sprintf("%s has no GitHub remote", repo.Name))
	}
	if opts.OverrideSet && strings.TrimSpace(opts.Override) == "" {
		return ProdResult{}, clierr.Usage("--override requires a non-empty reason")
	}

	result := ProdResult{Repo: repo.Name, Slug: repo.Slug, Library: s.library(repo)}
	if method == "" {
		method = prdomain.MethodMerge
	}
	open, err := s.Forge.ListOpen(ctx, repo.Slug)
	if err != nil {
		return result, withMessage(err, "could not list open pull requests")
	}
	release := findReleasePR(open)
	if release == nil {
		return result, clierr.Failed(fmt.Sprintf("%s has no open release pull request", repo.Slug))
	}
	result.ReleasePR = release

	if err := s.enforceGate(ctx, repo, release, opts); err != nil {
		return result, err
	}

	fmt.Fprintf(s.out(), "%s Merging release pull request #%d (%s)...\n",
		s.icon(s.UI.Arrow), release.Number, method)
	if err := s.Forge.MergePR(ctx, repo.Slug, strconv.FormatInt(release.Number, 10), method); err != nil {
		return result, withMessage(err, "failed to merge the release pull request")
	}
	result.Merged = true
	fmt.Fprintf(s.out(), "%s Release pull request #%d merged\n", s.icon(s.UI.OK), release.Number)

	if result.Library {
		if err := s.Forge.Dispatch(ctx, repo.Slug, releasedomain.PublishWorkflow, releasedomain.MainBranch); err != nil {
			return result, withMessage(err, "failed to dispatch "+releasedomain.PublishWorkflow)
		}
		result.Dispatched = releasedomain.PublishWorkflow
		result.OwnerApproval = fmt.Sprintf("The owner must approve the %s environment before packages publish.", releasedomain.NpmPublishEnvironment)
		fmt.Fprintf(s.out(), "%s Dispatched %s\n", s.icon(s.UI.OK), s.bold(releasedomain.PublishWorkflow))
		fmt.Fprintf(s.out(), "%s %s\n", s.icon(s.UI.Warn), result.OwnerApproval)
	}
	return result, nil
}

func (s *Service) statusOne(ctx context.Context, repo Repo) RepoStatus {
	setup, library := s.releaseSetup(repo)
	status := RepoStatus{
		Name:          repo.Name,
		Path:          repo.Path,
		Slug:          repo.Slug,
		ReleaseSetup:  setup,
		Library:       library,
		DefaultBranch: repo.DefaultBranch,
	}
	if status.DefaultBranch == "" {
		status.DefaultBranch = releasedomain.MainBranch
	}
	if repo.Slug == "" {
		status.Unverified = append(status.Unverified, repo.Name+": no GitHub remote")
	}

	if sha, err := s.headSHA(ctx, repo); err != nil {
		status.Unverified = append(status.Unverified, repo.Name+": default branch SHA: "+err.Error())
	} else {
		status.HeadSHA = sha
	}
	if tag, err := s.Git.LatestTag(ctx, repo.Dir); err != nil {
		status.Unverified = append(status.Unverified, repo.Name+": latest tag: "+err.Error())
	} else {
		status.LatestTag = tag
	}
	if repo.Slug == "" {
		return status
	}

	if release, err := s.releasePR(ctx, repo); err != nil {
		status.Unverified = append(status.Unverified, repo.Name+": release pull request: "+err.Error())
	} else if release != nil {
		status.ReleasePR = release
	}

	if demo, err := s.demoStatus(ctx, repo); err != nil {
		status.Unverified = append(status.Unverified, repo.Name+": demo branch: "+err.Error())
	} else {
		status.Demo = demo
	}

	if run, err := s.lastRun(ctx, repo); err != nil {
		status.Unverified = append(status.Unverified, repo.Name+": last publish/deploy run: "+err.Error())
	} else {
		status.LastRun = run
	}
	return status
}

func (s *Service) headSHA(ctx context.Context, repo Repo) (string, error) {
	branch := repo.DefaultBranch
	if branch == "" {
		branch = releasedomain.MainBranch
	}
	sha, err := s.Git.RevParse(ctx, repo.Dir, "origin/"+branch)
	if err == nil {
		return sha, nil
	}
	return s.Git.RevParse(ctx, repo.Dir, branch)
}

func (s *Service) releasePR(ctx context.Context, repo Repo) (*ReleasePR, error) {
	found, err := s.findOpenReleasePR(ctx, repo)
	if err != nil || found == nil {
		return nil, err
	}
	release := &ReleasePR{
		Number: found.Number,
		URL:    found.URL,
		Title:  found.Title,
		Head:   found.Head,
		Base:   found.Base,
	}
	checks, err := s.Forge.Checks(ctx, repo.Slug, strconv.FormatInt(found.Number, 10))
	if err != nil {
		return release, err
	}
	release.Checks = checks
	release.CheckState = releasedomain.CheckState(checks)
	return release, nil
}

// findOpenReleasePR lists the open pull requests and returns the bot release
// one without reading its checks. `release check` reads the checks through the
// wait loop instead.
func (s *Service) findOpenReleasePR(ctx context.Context, repo Repo) (*PullRequest, error) {
	open, err := s.Forge.ListOpen(ctx, repo.Slug)
	if err != nil {
		return nil, err
	}
	return findReleasePR(open), nil
}

func (s *Service) demoStatus(ctx context.Context, repo Repo) (DemoStatus, error) {
	sha, exists, err := s.Forge.BranchSHA(ctx, repo.Slug, releasedomain.DemoBranch)
	if err != nil {
		return DemoStatus{}, err
	}
	status := DemoStatus{Exists: exists, SHA: sha}
	if !exists {
		return status, nil
	}
	behind, err := s.Forge.CompareAhead(ctx, repo.Slug, releasedomain.DemoBranch, releasedomain.MainBranch)
	if err != nil {
		return status, err
	}
	status.Behind = behind
	return status, nil
}

func (s *Service) lastRun(ctx context.Context, repo Repo) (*WorkflowRun, error) {
	runs, err := s.Forge.Runs(ctx, repo.Slug)
	if err != nil {
		return nil, err
	}
	for i := range runs {
		if releasedomain.IsPublishOrDeployWorkflow(runs[i].Name) || releasedomain.IsPublishOrDeployWorkflow(runs[i].DisplayTitle) {
			return &runs[i], nil
		}
	}
	return nil, nil
}

func (s *Service) checkRemote(ctx context.Context, repo Repo, opts prdomain.MergeOptions) (CheckResult, error) {
	result := CheckResult{Repo: repo.Name, Slug: repo.Slug}
	if repo.Slug == "" {
		return result, clierr.Unverified(repo.Name + ": no GitHub remote")
	}
	release, err := s.findOpenReleasePR(ctx, repo)
	if err != nil {
		return result, withMessage(err, "could not read the release pull request")
	}
	if release == nil {
		return result, clierr.Unverified(repo.Name + ": no open release pull request")
	}
	result.ReleasePR = &ReleasePR{
		Number: release.Number,
		URL:    release.URL,
		Title:  release.Title,
		Head:   release.Head,
		Base:   release.Base,
	}

	checks, err := s.waitForChecks(ctx, repo.Slug, strconv.FormatInt(release.Number, 10), opts)
	if err != nil {
		return result, withMessage(err, "could not read release checks")
	}
	if len(checks) == 0 {
		result.State = "none"
		return result, clierr.Unverified(repo.Name + ": no release checks reported")
	}
	result.Checks = checks
	result.State = releasedomain.CheckState(checks)
	result.ReleasePR.Checks = checks
	result.ReleasePR.CheckState = result.State
	evaluation := prdomain.EvaluateChecks(checks)
	if !evaluation.Allowed {
		return result, clierr.Failed(repo.Name + ": " + evaluation.Reason)
	}
	fmt.Fprintf(s.out(), "%s %s release checks passed\n", s.icon(s.UI.OK), s.bold(repo.Name))
	return result, nil
}

func (s *Service) checkLocal(ctx context.Context, repo Repo) (CheckResult, error) {
	result := CheckResult{Repo: repo.Name, Slug: repo.Slug, Local: true}
	if s.Local == nil || s.Tester == nil {
		return result, clierr.Unverified(repo.Name + ": local release tier is not wired")
	}
	database := s.databaseName()
	if !releasedomain.ValidDatabaseName(database) {
		return result, clierr.Unverified(repo.Name + ": could not generate a throwaway database name")
	}
	result.Database = database

	env, err := s.Local.Env(repo.Dir, database)
	if err != nil {
		return result, clierr.Unverified(repo.Name + ": " + err.Error())
	}
	if err := s.Local.Reachable(ctx, repo.Dir); err != nil {
		return result, withMessage(err, repo.Name+": dev services are unreachable")
	}
	if err := s.Local.CreateDatabase(ctx, repo.Dir, database); err != nil {
		return result, clierr.Unverified(repo.Name + ": could not create the throwaway database: " + err.Error())
	}

	fmt.Fprintf(s.out(), "%s Running the release tier for %s against %s...\n",
		s.icon(s.UI.Arrow), s.bold(repo.Name), s.bold(database))
	testErr := s.Tester.Run(ctx, repo.Dir, env)

	dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dropErr := s.Local.DropDatabase(dropCtx, repo.Dir, database)
	if dropErr != nil {
		fmt.Fprintf(s.out(), "%s Could not drop %s: %s\n", s.icon(s.UI.Warn), s.bold(database), dropErr.Error())
	}
	if testErr != nil {
		return result, clierr.Failed(repo.Name + ": release tier failed: " + testErr.Error())
	}
	if dropErr != nil {
		return result, clierr.Unverified(repo.Name + ": release tier passed but " + database + " could not be dropped")
	}
	result.State = "pass"
	fmt.Fprintf(s.out(), "%s %s release tier passed; dropped %s\n", s.icon(s.UI.OK), s.bold(repo.Name), s.bold(database))
	return result, nil
}

func (s *Service) enforceGate(ctx context.Context, repo Repo, pr *PullRequest, opts prdomain.MergeOptions) error {
	checks, err := s.waitForChecks(ctx, repo.Slug, strconv.FormatInt(pr.Number, 10), opts)
	if err != nil {
		return withMessage(err, "merge refused: could not read checks")
	}
	if len(checks) == 0 && opts.AllowNoChecks {
		fmt.Fprintf(s.out(), "%s No checks reported; merging because %s is set\n",
			s.icon(s.UI.Warn), s.bold("--allow-no-checks"))
		return nil
	}
	evaluation := prdomain.EvaluateChecks(checks)
	if evaluation.Allowed {
		return nil
	}
	if reason := strings.TrimSpace(opts.Override); reason != "" {
		if err := s.Forge.Comment(ctx, repo.Slug, strconv.FormatInt(pr.Number, 10), "override: "+reason); err != nil {
			return withMessage(err, "recording override comment")
		}
		fmt.Fprintf(s.out(), "%s override recorded: %s\n", s.icon(s.UI.Warn), reason)
		return nil
	}
	return clierr.Failed("merge refused: " + evaluation.Reason + " (use --override \"reason\" to merge anyway)")
}

// waitForChecks polls the pull request checks until the check names are stable
// across two polls and none is pending. It mirrors `pr merge` exactly.
func (s *Service) waitForChecks(ctx context.Context, slug, ref string, opts prdomain.MergeOptions) ([]prdomain.Check, error) {
	waitTimeout := opts.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = s.waitTimeout()
	}
	pollInterval := s.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	registerWindow := s.RegisterWindow
	if registerWindow <= 0 {
		registerWindow = DefaultRegisterWindow
	}

	start := s.now()
	var last []prdomain.Check
	var lastNames map[string]struct{}
	polled := false

	for {
		checks, err := s.Forge.Checks(ctx, slug, ref)
		if err != nil {
			return nil, err
		}
		last = checks
		if !opts.Wait {
			return last, nil
		}

		names := checkNameSet(last)
		elapsed := s.now().Sub(start)
		complete := countPending(last) == 0 && (len(last) > 0 || elapsed >= registerWindow)
		stable := polled && sameCheckNameSet(lastNames, names)
		firstEmpty := !polled && len(last) == 0 && elapsed >= registerWindow
		if complete && (stable || firstEmpty) {
			return last, nil
		}
		lastNames, polled = names, true

		if elapsed >= waitTimeout {
			fmt.Fprintf(s.out(), "%s Check wait timed out after %s\n", s.icon(s.UI.Warn), waitTimeout)
			return last, nil
		}
		fmt.Fprintf(s.out(), "%s Waiting for release checks on %s (%d pending, %s elapsed)...\n",
			s.icon(s.UI.Arrow), s.bold(ref), countPending(last), elapsed.Round(time.Second))
		s.sleep(pollInterval)
	}
}

func (s *Service) selectRepos(selector string, setupOnly bool) ([]Repo, error) {
	repos, err := s.Workspace.Repos(s.Root, s.ManifestPath)
	if err != nil {
		return nil, clierr.Unverified("workspace manifest: " + err.Error())
	}
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return s.filterSetup(repos), nil
	}
	var matched []Repo
	for _, repo := range repos {
		if repoMatches(repo, selector) {
			matched = append(matched, repo)
		}
	}
	if len(matched) == 0 {
		for _, repo := range repos {
			if repo.Scope == selector || strings.HasPrefix(repo.Scope, selector+"/") {
				matched = append(matched, repo)
			}
		}
	}
	if len(matched) == 0 {
		return nil, clierr.Usage(fmt.Sprintf("no workspace repository or scope matches %q", selector))
	}
	if setupOnly && len(matched) > 1 {
		matched = s.filterSetup(matched)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Path < matched[j].Path })
	return matched, nil
}

func (s *Service) selectOne(selector string) (Repo, error) {
	if strings.TrimSpace(selector) == "" {
		return Repo{}, clierr.Usage("select one repository")
	}
	repos, err := s.selectRepos(selector, false)
	if err != nil {
		return Repo{}, err
	}
	if len(repos) != 1 {
		return Repo{}, clierr.Usage(fmt.Sprintf("%q matches %d repositories; select one", selector, len(repos)))
	}
	return repos[0], nil
}

func (s *Service) filterSetup(repos []Repo) []Repo {
	filtered := make([]Repo, 0, len(repos))
	for _, repo := range repos {
		if setup, _ := s.releaseSetup(repo); setup {
			filtered = append(filtered, repo)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Path < filtered[j].Path })
	return filtered
}

func (s *Service) releaseSetup(repo Repo) (setup, library bool) {
	if s.Files == nil {
		return false, false
	}
	for _, name := range releasedomain.ReleaseSetupFiles {
		if s.Files.Exists(filepath.Join(repo.Dir, filepath.FromSlash(name))) {
			setup = true
		}
	}
	return setup, s.library(repo)
}

func (s *Service) library(repo Repo) bool {
	if s.Files == nil {
		return false
	}
	return s.Files.Exists(filepath.Join(repo.Dir, filepath.FromSlash(releasedomain.ChangesetConfig)))
}

func (s *Service) databaseName() string {
	if s.NewDatabaseName != nil {
		return s.NewDatabaseName()
	}
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return releasedomain.DatabaseNamePrefix + hex.EncodeToString(buf)
}

func (s *Service) waitTimeout() time.Duration {
	if s.WaitTimeout > 0 {
		return s.WaitTimeout
	}
	return DefaultWaitTimeout
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) icon(glyph string) string {
	if glyph == "" {
		return ""
	}
	return glyph + " "
}

func (s *Service) bold(text string) string {
	if s.UI.Bold == nil {
		return text
	}
	return s.UI.Bold(text)
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *Service) sleep(d time.Duration) {
	if s.Sleep == nil {
		time.Sleep(d)
		return
	}
	s.Sleep(d)
}

func repoMatches(repo Repo, selector string) bool {
	if strings.EqualFold(repo.Name, selector) || repo.Path == selector || repo.Scope == selector {
		return true
	}
	cleaned := filepath.Clean(selector)
	if repo.Path == cleaned {
		return true
	}
	if abs, err := filepath.Abs(selector); err == nil && repo.Dir == abs {
		return true
	}
	return false
}

func findReleasePR(open []PullRequest) *PullRequest {
	for i := range open {
		if releasedomain.IsReleaseHead(open[i].Head) {
			return &open[i]
		}
	}
	return nil
}

func countPending(checks []prdomain.Check) int {
	n := 0
	for _, check := range checks {
		if check.Bucket == "pending" {
			n++
		}
	}
	return n
}

func checkNameSet(checks []prdomain.Check) map[string]struct{} {
	names := make(map[string]struct{}, len(checks))
	for _, check := range checks {
		names[check.Name] = struct{}{}
	}
	return names
}

func sameCheckNameSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if _, ok := b[name]; !ok {
			return false
		}
	}
	return true
}

func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

func withMessage(err error, msg string) error {
	var c *clierr.Error
	if errors.As(err, &c) {
		return &clierr.Error{Code: c.Code, Exit: c.Exit, Message: msg, Hint: c.Hint, Err: err}
	}
	return clierr.Wrap(err, clierr.Failed(msg))
}
