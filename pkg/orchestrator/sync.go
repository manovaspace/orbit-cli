package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"git.dev.manova.space/manova/orbit-cli/pkg/manifest"
)

const defaultSyncConcurrency = 4

// SyncRepo synchronizes a local Git repository with its remote origin.
// It fetches origin, verifies the working tree is clean, verifies the current branch is the default branch,
// and executes a fast-forward only merge.
func SyncRepo(repoPath string, defaultBranch string) SyncResult {
	name := filepath.Base(repoPath)

	info, err := os.Stat(repoPath)
	if err != nil {
		return SyncResult{
			Name:    name,
			Path:    repoPath,
			Success: false,
			Error:   fmt.Sprintf("repository path not accessible: %v", err),
		}
	}
	if !info.IsDir() {
		return SyncResult{
			Name:    name,
			Path:    repoPath,
			Success: false,
			Error:   "path is not a directory",
		}
	}

	// Verify it is a git repository
	cmdCheck := exec.Command("git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	if out, err := cmdCheck.CombinedOutput(); err != nil {
		return SyncResult{
			Name:    name,
			Path:    repoPath,
			Success: false,
			Error:   fmt.Sprintf("not a git repository (%s)", strings.TrimSpace(string(out))),
		}
	}

	if defaultBranch == "" {
		defaultBranch = "main"
	}

	// 1. Fetch remote origin
	cmdFetch := exec.Command("git", "-C", repoPath, "fetch", "origin")
	if fetchOut, err := cmdFetch.CombinedOutput(); err != nil {
		return SyncResult{
			Name:    name,
			Path:    repoPath,
			Success: false,
			Error:   fmt.Sprintf("git fetch origin failed: %s", strings.TrimSpace(string(fetchOut))),
		}
	}

	// 2. Check if working tree is clean
	cmdStatus := exec.Command("git", "-C", repoPath, "status", "--porcelain")
	statusOut, err := cmdStatus.Output()
	if err != nil {
		return SyncResult{
			Name:    name,
			Path:    repoPath,
			Success: false,
			Error:   fmt.Sprintf("git status failed: %v", err),
		}
	}

	if strings.TrimSpace(string(statusOut)) != "" {
		return SyncResult{
			Name:          name,
			Path:          repoPath,
			Success:       false,
			SkippedReason: "working tree has uncommitted changes",
		}
	}

	// 3. Check current branch
	cmdBranch := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := cmdBranch.Output()
	if err != nil {
		return SyncResult{
			Name:    name,
			Path:    repoPath,
			Success: false,
			Error:   fmt.Sprintf("failed to get current branch: %v", err),
		}
	}

	currentBranch := strings.TrimSpace(string(branchOut))
	if currentBranch != defaultBranch {
		return SyncResult{
			Name:          name,
			Path:          repoPath,
			Success:       false,
			SkippedReason: fmt.Sprintf("current branch '%s' is not default branch '%s'", currentBranch, defaultBranch),
		}
	}

	// 4. Fast-forward merge origin/<defaultBranch>
	targetRef := fmt.Sprintf("origin/%s", defaultBranch)
	cmdMerge := exec.Command("git", "-C", repoPath, "merge", "--ff-only", targetRef)
	mergeOut, err := cmdMerge.CombinedOutput()
	mergeOutputStr := strings.TrimSpace(string(mergeOut))

	if err != nil {
		return SyncResult{
			Name:          name,
			Path:          repoPath,
			Success:       false,
			SkippedReason: "branch cannot be fast-forwarded",
			Error:         mergeOutputStr,
		}
	}

	fastForwarded := strings.Contains(mergeOutputStr, "Fast-forward") || strings.Contains(mergeOutputStr, "Updating")

	return SyncResult{
		Name:          name,
		Path:          repoPath,
		Success:       true,
		FastForwarded: fastForwarded,
	}
}

// SyncTargets executes SyncRepo across multiple repository targets in parallel using a worker pool.
func SyncTargets(workspaceRoot string, targets []manifest.RepoTarget, concurrency int) []SyncResult {
	if len(targets) == 0 {
		return []SyncResult{}
	}

	if concurrency <= 0 {
		concurrency = defaultSyncConcurrency
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

	results := make([]SyncResult, len(targets))

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
				fullPath := filepath.Join(workspaceRoot, j.target.Path)
				gitPath := filepath.Join(fullPath, ".git")
				if _, err := os.Stat(gitPath); err != nil {
					results[j.index] = SyncResult{
						Name:    j.target.Name,
						Path:    j.target.Path,
						Success: false,
						Error:   "repository not cloned",
					}
					continue
				}

				branch := j.target.DefaultBranch
				if branch == "" {
					branch = "main"
				}

				res := SyncRepo(fullPath, branch)
				res.Name = j.target.Name
				res.Path = j.target.Path
				results[j.index] = res
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
