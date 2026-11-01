package application

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/workspace/domain"
)

const (
	defaultCloneConcurrency  = 4
	defaultSyncConcurrency   = 4
	defaultStatusConcurrency = 8
)

// StatusInput is one `orbit status` invocation.
type StatusInput struct {
	WorkspaceRoot string
	ManifestPath  string
	Scope         string
}

// StatusOutput is what `orbit status` produced.
type StatusOutput struct {
	Scope string              `json:"scope"`
	Repos []domain.RepoStatus `json:"repos"`
}

// SyncInput is one `orbit sync` invocation.
type SyncInput struct {
	WorkspaceRoot string
	ManifestPath  string
	Scope         string
	Concurrency   int
}

// SyncOutput is what `orbit sync` produced.
type SyncOutput struct {
	Scope   string                `json:"scope"`
	Targets []manifest.RepoTarget `json:"targets,omitempty"`
	Results []domain.SyncResult   `json:"results"`
}

// SyncAllInput is the "every repository in the manifest" sync used by
// `orbit update`.
type SyncAllInput struct {
	WorkspaceRoot string
	ManifestPath  string
	Concurrency   int
}

// SyncAllOutput is what a whole-manifest sync produced.
type SyncAllOutput struct {
	ManifestFound bool                  `json:"manifest_found"`
	Targets       []manifest.RepoTarget `json:"targets,omitempty"`
	Results       []domain.SyncResult   `json:"results"`
}

// InitInput is one `orbit init` invocation.
type InitInput struct {
	WorkspaceRoot string
	ManifestPath  string
	Scope         string
	Concurrency   int
}

// InitOutput is what `orbit init` produced.
type InitOutput struct {
	Scope          string                `json:"scope"`
	Targets        []manifest.RepoTarget `json:"targets"`
	Results        []domain.CloneResult  `json:"results"`
	Cloned         int                   `json:"cloned"`
	AlreadyPresent int                   `json:"already_present"`
	Failed         int                   `json:"failed"`
}

// RepairInput is one `orbit repair` invocation.
type RepairInput struct {
	WorkspaceRoot string
	ManifestPath  string
	Scope         string
	Concurrency   int
}

// RepairOutput is what `orbit repair` produced.
type RepairOutput struct {
	Scope    string                `json:"scope"`
	Results  []domain.RepairResult `json:"results"`
	Repaired int                   `json:"repaired"`
	Skipped  int                   `json:"skipped"`
	Failed   int                   `json:"failed"`
}

// Status inspects every repository in scope.
func (s *Service) Status(ctx context.Context, in StatusInput) (StatusOutput, error) {
	targets, err := s.resolve(in.ManifestPath, in.Scope)
	if err != nil {
		return StatusOutput{}, err
	}
	return StatusOutput{Scope: in.Scope, Repos: s.statusTargets(ctx, in.WorkspaceRoot, targets)}, nil
}

// Sync fetches and fast-forwards every repository in scope. It writes the
// per-repository progress lines through the injected writer.
func (s *Service) Sync(ctx context.Context, in SyncInput) (SyncOutput, error) {
	targets, err := s.resolve(in.ManifestPath, in.Scope)
	if err != nil {
		return SyncOutput{}, err
	}
	if len(targets) == 0 {
		return SyncOutput{Scope: in.Scope, Results: []domain.SyncResult{}}, nil
	}

	concurrency := in.Concurrency
	if concurrency <= 0 {
		concurrency = defaultSyncConcurrency
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

	fmt.Fprintf(s.out(), "%s\n\n", s.header(fmt.Sprintf("Syncing %d repositories (concurrency: %d)...", len(targets), concurrency)))

	results := s.syncTargets(ctx, in.WorkspaceRoot, targets, concurrency, nil)
	for _, res := range results {
		nameCol := s.bold(padRight(res.Name, 24))
		switch {
		case res.Success && res.FastForwarded:
			fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.OK, nameCol, s.success("Fast-forwarded to latest origin"))
		case res.Success:
			fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.OK, nameCol, s.subtle("Up to date"))
		case res.SkippedReason != "":
			fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.Warn, nameCol, s.warning("Skipped: "+res.SkippedReason))
		default:
			errMsg := res.Error
			if errMsg == "" {
				errMsg = "unknown sync error"
			}
			fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.Error, nameCol, s.errorText("Error: "+errMsg))
		}
	}

	return SyncOutput{Scope: in.Scope, Targets: targets, Results: results}, nil
}

// SyncAll syncs every repository in the manifest without writing progress
// lines; `orbit update` renders its own phase output.
func (s *Service) SyncAll(ctx context.Context, in SyncAllInput) (SyncAllOutput, error) {
	exists, err := s.manifestExists(in.ManifestPath)
	if err != nil {
		return SyncAllOutput{}, err
	}
	if !exists {
		return SyncAllOutput{}, nil
	}
	m, err := s.Manifest.Load(in.ManifestPath)
	if err != nil {
		return SyncAllOutput{ManifestFound: true}, err
	}
	targets := m.AllRepos()
	return SyncAllOutput{
		ManifestFound: true,
		Targets:       targets,
		Results:       s.syncTargets(ctx, in.WorkspaceRoot, targets, normalizedConcurrency(in.Concurrency, len(targets), defaultSyncConcurrency), nil),
	}, nil
}

// Init clones every repository in scope. It writes the per-repository progress
// lines through the injected writer.
func (s *Service) Init(ctx context.Context, in InitInput) (InitOutput, error) {
	m, err := s.loadManifest(in.ManifestPath)
	if err != nil {
		return InitOutput{}, err
	}
	targets, err := m.ResolveRepos(in.Scope)
	if err != nil {
		return InitOutput{}, clierr.Failed(fmt.Sprintf("failed to resolve repositories for scope %q: %v", in.Scope, err))
	}
	out := InitOutput{Scope: in.Scope, Targets: targets, Results: []domain.CloneResult{}}
	if len(targets) == 0 {
		out.Targets = []manifest.RepoTarget{}
		return out, nil
	}

	concurrency := normalizedConcurrency(in.Concurrency, len(targets), defaultCloneConcurrency)
	fmt.Fprintf(s.out(), "%s\n", s.header(fmt.Sprintf("Cloning %d repositories (concurrency: %d)...", len(targets), concurrency)))

	out.Results = s.cloneTargets(ctx, in.WorkspaceRoot, targets, concurrency, func(res domain.CloneResult) {
		if res.Success {
			if res.AlreadyExists {
				fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.OK, s.bold(res.Name), s.subtle("(already exists)"))
			} else {
				fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.OK, s.bold(res.Name), s.success("(cloned)"))
			}
			return
		}
		fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.Error, s.bold(res.Name), s.errorText(fmt.Sprintf("(failed: %s)", res.Error)))
	})
	for _, res := range out.Results {
		switch {
		case !res.Success:
			out.Failed++
		case res.AlreadyExists:
			out.AlreadyPresent++
		default:
			out.Cloned++
		}
	}
	return out, nil
}

// Repair attaches .git to every gitless tree in scope. It writes the
// per-repository progress lines through the injected writer.
func (s *Service) Repair(ctx context.Context, in RepairInput) (RepairOutput, error) {
	targets, err := s.resolve(in.ManifestPath, in.Scope)
	if err != nil {
		return RepairOutput{}, err
	}
	out := RepairOutput{Scope: in.Scope, Results: []domain.RepairResult{}}
	if len(targets) == 0 {
		return out, nil
	}

	concurrency := normalizedConcurrency(in.Concurrency, len(targets), defaultCloneConcurrency)
	fmt.Fprintf(s.out(), "%s\n\n", s.header(fmt.Sprintf("Repairing %d repositories (concurrency: %d)...", len(targets), concurrency)))

	out.Results = s.repairTargets(ctx, in.WorkspaceRoot, targets, concurrency, func(res domain.RepairResult) {
		nameCol := s.bold(padRight(res.Name, 24))
		switch {
		case !res.Success:
			fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.Error, nameCol, s.errorText(res.Error))
		case res.Skipped != "":
			fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.OK, nameCol, s.subtle(res.Skipped))
		default:
			fmt.Fprintf(s.out(), "  %s  %s %s\n", s.UI.OK, nameCol, s.success("attached .git"))
		}
	})
	for _, res := range out.Results {
		switch {
		case !res.Success:
			out.Failed++
		case res.Skipped != "":
			out.Skipped++
		default:
			out.Repaired++
		}
	}
	return out, nil
}

func (s *Service) resolve(manifestPath, scope string) ([]manifest.RepoTarget, error) {
	m, err := s.loadManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	targets, err := m.ResolveRepos(scope)
	if err != nil {
		return nil, clierr.Failed(err.Error())
	}
	return targets, nil
}

func (s *Service) loadManifest(manifestPath string) (*manifest.WorkspaceManifest, error) {
	exists, err := s.manifestExists(manifestPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, clierr.Failed(fmt.Sprintf("manifest file not found at %s. Ensure workspace.yaml exists or pass --manifest", manifestPath))
	}
	m, err := s.Manifest.Load(manifestPath)
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Failed("failed to load workspace manifest"))
	}
	return m, nil
}

func (s *Service) manifestExists(path string) (bool, error) {
	_, exists, err := s.FS.Stat(path)
	if err != nil {
		return false, clierr.Wrap(err, clierr.Failed(fmt.Sprintf("failed to read manifest at %s", path)))
	}
	return exists, nil
}

func (s *Service) statusTargets(ctx context.Context, workspaceRoot string, targets []manifest.RepoTarget) []domain.RepoStatus {
	if len(targets) == 0 {
		return []domain.RepoStatus{}
	}
	results := make([]domain.RepoStatus, len(targets))
	concurrency := normalizedConcurrency(defaultStatusConcurrency, len(targets), defaultStatusConcurrency)

	type job struct {
		index  int
		target manifest.RepoTarget
	}
	jobs := make(chan job, len(targets))
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				status := s.inspect(ctx, filepath.Join(workspaceRoot, j.target.Path), j.target)
				results[j.index] = status
			}
		}()
	}
	for i, target := range targets {
		jobs <- job{index: i, target: target}
	}
	close(jobs)
	wg.Wait()
	return results
}

func (s *Service) inspect(ctx context.Context, repoPath string, target manifest.RepoTarget) domain.RepoStatus {
	name := filepath.Base(repoPath)
	status := domain.RepoStatus{Name: name, Path: repoPath}

	info, exists, err := s.FS.Stat(repoPath)
	if err != nil {
		status.Error = fmt.Sprintf("repository path not accessible: %v", err)
		return replaceTarget(status, target)
	}
	if !exists {
		status.Error = domain.ErrMissing
		return replaceTarget(status, target)
	}
	if !info.IsDir {
		status.Error = "path is not a directory"
		return replaceTarget(status, target)
	}

	if inside, gitErr := s.Git.IsWorkTree(ctx, repoPath); gitErr != nil || !inside {
		status.Error = domain.ErrGitless
		return replaceTarget(status, target)
	}

	if branch, branchErr := s.Git.Branch(ctx, repoPath); branchErr == nil {
		status.CurrentBranch = branch
	}

	porcelain, statusErr := s.Git.Status(ctx, repoPath)
	if statusErr != nil {
		status.Error = fmt.Sprintf("git status failed: %v", statusErr)
		return replaceTarget(status, target)
	}
	status.ModifiedCount = domain.ModifiedCount(porcelain)
	status.IsClean = status.ModifiedCount == 0
	if ahead, behind, aheadErr := s.Git.AheadBehind(ctx, repoPath); aheadErr == nil {
		status.AheadCount, status.BehindCount = ahead, behind
	}
	return replaceTarget(status, target)
}

func replaceTarget(status domain.RepoStatus, target manifest.RepoTarget) domain.RepoStatus {
	status.Name = target.Name
	status.Path = target.Path
	return status
}

func (s *Service) syncTargets(ctx context.Context, workspaceRoot string, targets []manifest.RepoTarget, concurrency int, callback func(domain.SyncResult)) []domain.SyncResult {
	if len(targets) == 0 {
		return []domain.SyncResult{}
	}
	if concurrency <= 0 {
		concurrency = defaultSyncConcurrency
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

	results := make([]domain.SyncResult, len(targets))
	type job struct {
		index  int
		target manifest.RepoTarget
	}
	jobs := make(chan job, len(targets))
	var wg sync.WaitGroup
	var cbMutex sync.Mutex

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				fullPath := filepath.Join(workspaceRoot, j.target.Path)
				gitPath := filepath.Join(fullPath, ".git")
				if _, exists, err := s.FS.Stat(gitPath); err != nil || !exists {
					results[j.index] = domain.SyncResult{
						Name:    j.target.Name,
						Path:    j.target.Path,
						Success: false,
						Error:   domain.NotCloned,
					}
					continue
				}
				branch := j.target.DefaultBranch
				if branch == "" {
					branch = "main"
				}
				res := s.syncRepo(ctx, fullPath, branch)
				res.Name = j.target.Name
				res.Path = j.target.Path
				results[j.index] = res
				if callback != nil {
					cbMutex.Lock()
					callback(res)
					cbMutex.Unlock()
				}
			}
		}()
	}
	for i, target := range targets {
		jobs <- job{index: i, target: target}
	}
	close(jobs)
	wg.Wait()
	return results
}

func (s *Service) syncRepo(ctx context.Context, repoPath, defaultBranch string) domain.SyncResult {
	name := filepath.Base(repoPath)

	info, exists, err := s.FS.Stat(repoPath)
	if err != nil {
		return domain.SyncResult{Name: name, Path: repoPath, Error: fmt.Sprintf("repository path not accessible: %v", err)}
	}
	if !exists {
		return domain.SyncResult{Name: name, Path: repoPath, Error: fmt.Sprintf("repository path not accessible: %s does not exist", repoPath)}
	}
	if !info.IsDir {
		return domain.SyncResult{Name: name, Path: repoPath, Error: "path is not a directory"}
	}

	if inside, gitErr := s.Git.IsWorkTree(ctx, repoPath); gitErr != nil || !inside {
		detail := ""
		if gitErr != nil {
			detail = gitErr.Error()
		}
		return domain.SyncResult{Name: name, Path: repoPath, Error: fmt.Sprintf("not a git repository (%s)", detail)}
	}

	if defaultBranch == "" {
		defaultBranch = "main"
	}

	if fetchErr := s.Git.Fetch(ctx, repoPath); fetchErr != nil {
		return domain.SyncResult{Name: name, Path: repoPath, Error: fmt.Sprintf("git fetch origin failed: %s", fetchErr.Error())}
	}

	porcelain, statusErr := s.Git.Status(ctx, repoPath)
	if statusErr != nil {
		return domain.SyncResult{Name: name, Path: repoPath, Error: fmt.Sprintf("git status failed: %v", statusErr)}
	}
	if strings.TrimSpace(porcelain) != "" {
		return domain.SyncResult{Name: name, Path: repoPath, SkippedReason: domain.SkipUncommitted}
	}

	currentBranch, branchErr := s.Git.Branch(ctx, repoPath)
	if branchErr != nil {
		return domain.SyncResult{Name: name, Path: repoPath, Error: fmt.Sprintf("failed to get current branch: %v", branchErr)}
	}
	if currentBranch != defaultBranch {
		return domain.SyncResult{Name: name, Path: repoPath, SkippedReason: domain.NotDefaultBranch(currentBranch, defaultBranch)}
	}

	output, mergeErr := s.Git.MergeFFOnly(ctx, repoPath, "origin/"+defaultBranch)
	if mergeErr != nil {
		return domain.SyncResult{
			Name:          name,
			Path:          repoPath,
			SkippedReason: domain.SkipNotFastForward,
			Error:         strings.TrimSpace(output),
		}
	}
	return domain.SyncResult{
		Name:          name,
		Path:          repoPath,
		Success:       true,
		FastForwarded: domain.IsFastForward(output),
	}
}

// CloneTargets clones targets with the configured ports. The onboarding flow
// uses it directly; the progress callback may be nil.
func (s *Service) CloneTargets(ctx context.Context, workspaceRoot string, targets []manifest.RepoTarget, concurrency int, callback func(domain.CloneResult)) []domain.CloneResult {
	return s.cloneTargets(ctx, workspaceRoot, targets, concurrency, callback)
}

func (s *Service) cloneTargets(ctx context.Context, workspaceRoot string, targets []manifest.RepoTarget, concurrency int, callback func(domain.CloneResult)) []domain.CloneResult {
	if len(targets) == 0 {
		return []domain.CloneResult{}
	}
	if concurrency <= 0 {
		concurrency = defaultCloneConcurrency
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

	results := make([]domain.CloneResult, len(targets))
	type job struct {
		index  int
		target manifest.RepoTarget
	}
	jobs := make(chan job, len(targets))
	var wg sync.WaitGroup
	var cbMutex sync.Mutex

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				res := s.cloneTarget(ctx, workspaceRoot, j.target)
				results[j.index] = res
				if callback != nil {
					cbMutex.Lock()
					callback(res)
					cbMutex.Unlock()
				}
			}
		}()
	}
	for i, target := range targets {
		jobs <- job{index: i, target: target}
	}
	close(jobs)
	wg.Wait()
	return results
}

func (s *Service) cloneTarget(ctx context.Context, workspaceRoot string, target manifest.RepoTarget) domain.CloneResult {
	if target.Path == "" {
		return domain.CloneResult{Name: target.Name, Path: target.Path, Error: "target path is empty"}
	}

	destPath := filepath.Join(workspaceRoot, target.Path)
	gitPath := filepath.Join(destPath, ".git")

	if info, exists, err := s.FS.Stat(destPath); err == nil && exists && info.IsDir {
		if _, gitExists, gitErr := s.FS.Stat(gitPath); gitErr == nil && gitExists {
			return domain.CloneResult{Name: target.Name, Path: target.Path, Success: true, AlreadyExists: true}
		}
		entries, readErr := s.FS.ReadDir(destPath)
		if readErr != nil {
			return domain.CloneResult{Name: target.Name, Path: target.Path, Error: readErr.Error()}
		}
		if len(entries) > 0 {
			return domain.CloneResult{Name: target.Name, Path: target.Path, Error: "exists but is not a git repository; run orbit repair"}
		}
	}

	if target.RemoteURL == "" {
		return domain.CloneResult{Name: target.Name, Path: target.Path, Error: "remote URL is empty"}
	}

	if err := s.FS.MkdirAll(filepath.Dir(destPath)); err != nil {
		return domain.CloneResult{Name: target.Name, Path: target.Path, Error: fmt.Sprintf("failed to create parent directory: %v", err)}
	}

	output, err := s.Git.Clone(ctx, workspaceRoot, target.RemoteURL, target.Path)
	if err != nil {
		errMsg := strings.TrimSpace(output)
		if strings.Contains(errMsg, "already exists and is not an empty directory") {
			return domain.CloneResult{Name: target.Name, Path: target.Path, Error: "exists but is not a git repository; run orbit repair"}
		}
		if errMsg == "" {
			errMsg = err.Error()
		}
		return domain.CloneResult{Name: target.Name, Path: target.Path, Error: errMsg}
	}
	return domain.CloneResult{Name: target.Name, Path: target.Path, Success: true}
}

func (s *Service) repairTargets(ctx context.Context, workspaceRoot string, targets []manifest.RepoTarget, concurrency int, callback func(domain.RepairResult)) []domain.RepairResult {
	if len(targets) == 0 {
		return []domain.RepairResult{}
	}
	if concurrency <= 0 {
		concurrency = defaultCloneConcurrency
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

	results := make([]domain.RepairResult, len(targets))
	type job struct {
		index  int
		target manifest.RepoTarget
	}
	jobs := make(chan job, len(targets))
	var wg sync.WaitGroup
	var cbMutex sync.Mutex

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				res := s.repairTarget(ctx, workspaceRoot, j.target)
				results[j.index] = res
				if callback != nil {
					cbMutex.Lock()
					callback(res)
					cbMutex.Unlock()
				}
			}
		}()
	}
	for i, target := range targets {
		jobs <- job{index: i, target: target}
	}
	close(jobs)
	wg.Wait()
	return results
}

// repairTarget copies .git from a temporary clone onto the existing tree. It
// never checks out, resets or deletes a working file.
func (s *Service) repairTarget(ctx context.Context, workspaceRoot string, target manifest.RepoTarget) domain.RepairResult {
	if target.Path == "" {
		return domain.RepairResult{Name: target.Name, Success: false, Error: "target path is empty"}
	}
	if target.RemoteURL == "" {
		return domain.RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: "remote URL is empty"}
	}

	destPath := filepath.Join(workspaceRoot, target.Path)
	gitPath := filepath.Join(destPath, ".git")

	if _, exists, err := s.FS.Stat(gitPath); err == nil && exists {
		return domain.RepairResult{Name: target.Name, Path: target.Path, Success: true, Skipped: "already a git repository"}
	}

	info, exists, err := s.FS.Stat(destPath)
	if err != nil {
		return domain.RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: err.Error()}
	}
	if !exists {
		clone := s.cloneTarget(ctx, workspaceRoot, target)
		return domain.RepairResult{Name: target.Name, Path: target.Path, Success: clone.Success, Error: clone.Error}
	}
	if !info.IsDir {
		return domain.RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: "path is not a directory"}
	}

	tmp, err := s.FS.TempDir("orbit-repair-*")
	if err != nil {
		return domain.RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: err.Error()}
	}
	defer func() { _ = s.FS.RemoveAll(tmp) }()

	cloneDir := filepath.Join(tmp, "clone")
	output, cloneErr := s.Git.Clone(ctx, "", target.RemoteURL, cloneDir)
	if cloneErr != nil {
		msg := strings.TrimSpace(output)
		if msg == "" {
			msg = cloneErr.Error()
		}
		return domain.RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: fmt.Sprintf("git clone failed: %s", msg)}
	}

	srcGit := filepath.Join(cloneDir, ".git")
	if _, exists, statErr := s.FS.Stat(srcGit); statErr != nil || !exists {
		return domain.RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: "clone produced no .git directory"}
	}
	if copyErr := s.FS.CopyTree(srcGit, gitPath); copyErr != nil {
		return domain.RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: fmt.Sprintf("copy .git failed: %s", copyErr.Error())}
	}
	return domain.RepairResult{Name: target.Name, Path: target.Path, Success: true}
}

func normalizedConcurrency(want, targets, fallback int) int {
	if want <= 0 {
		want = fallback
	}
	if want > targets {
		want = targets
	}
	if want < 1 {
		want = 1
	}
	return want
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) header(text string) string {
	if s.UI.Header == nil {
		return text
	}
	return s.UI.Header(text)
}

func (s *Service) bold(text string) string {
	if s.UI.Bold == nil {
		return text
	}
	return s.UI.Bold(text)
}

func (s *Service) success(text string) string {
	if s.UI.Success == nil {
		return text
	}
	return s.UI.Success(text)
}

func (s *Service) warning(text string) string {
	if s.UI.Warning == nil {
		return text
	}
	return s.UI.Warning(text)
}

func (s *Service) errorText(text string) string {
	if s.UI.ErrorText == nil {
		return text
	}
	return s.UI.ErrorText(text)
}

func (s *Service) subtle(text string) string {
	if s.UI.Subtle == nil {
		return text
	}
	return s.UI.Subtle(text)
}

func padRight(s string, width int) string {
	length := len([]rune(s))
	if length >= width {
		return s
	}
	return s + strings.Repeat(" ", width-length)
}
