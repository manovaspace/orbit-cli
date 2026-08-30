package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

// InspectRepo inspects a single local Git repository and returns its status.
func InspectRepo(repoPath string) (*RepoStatus, error) {
	name := filepath.Base(repoPath)

	info, err := os.Stat(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &RepoStatus{
				Name:  name,
				Path:  repoPath,
				Error: ErrMissing,
			}, fmt.Errorf("%s", ErrMissing)
		}
		return &RepoStatus{
			Name:  name,
			Path:  repoPath,
			Error: fmt.Sprintf("repository path not accessible: %v", err),
		}, fmt.Errorf("repository path not accessible: %w", err)
	}
	if !info.IsDir() {
		return &RepoStatus{
			Name:  name,
			Path:  repoPath,
			Error: "path is not a directory",
		}, fmt.Errorf("path is not a directory: %s", repoPath)
	}

	// Verify it is inside a git working tree
	cmdCheck := exec.Command("git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	if out, err := cmdCheck.CombinedOutput(); err != nil {
		return &RepoStatus{
			Name:  name,
			Path:  repoPath,
			Error: ErrGitless,
		}, fmt.Errorf("%s (%s): %v", ErrGitless, strings.TrimSpace(string(out)), err)
	}

	// 1. Get current branch name
	currentBranch := ""
	cmdBranch := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if out, err := cmdBranch.Output(); err == nil {
		currentBranch = strings.TrimSpace(string(out))
	} else {
		// Fallback for unborn branch or detached HEAD
		cmdSym := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "HEAD")
		if symOut, symErr := cmdSym.Output(); symErr == nil {
			currentBranch = strings.TrimSpace(string(symOut))
		} else {
			currentBranch = "HEAD"
		}
	}

	// 2. Check working tree cleanliness (modified, staged, untracked)
	cmdStatus := exec.Command("git", "-C", repoPath, "status", "--porcelain")
	statusOut, err := cmdStatus.Output()
	if err != nil {
		return &RepoStatus{
			Name:          name,
			Path:          repoPath,
			CurrentBranch: currentBranch,
			Error:         fmt.Sprintf("git status failed: %v", err),
		}, fmt.Errorf("git status failed: %w", err)
	}

	trimmedStatus := strings.TrimSpace(string(statusOut))
	modifiedCount := 0
	if trimmedStatus != "" {
		lines := strings.Split(trimmedStatus, "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				modifiedCount++
			}
		}
	}
	isClean := (modifiedCount == 0)

	// 3. Check ahead / behind counts relative to upstream tracking branch
	aheadCount := 0
	behindCount := 0
	cmdRevList := exec.Command("git", "-C", repoPath, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if revOut, err := cmdRevList.Output(); err == nil {
		fields := strings.Fields(strings.TrimSpace(string(revOut)))
		if len(fields) >= 2 {
			if a, err := strconv.Atoi(fields[0]); err == nil {
				aheadCount = a
			}
			if b, err := strconv.Atoi(fields[1]); err == nil {
				behindCount = b
			}
		}
	}
	// If rev-list fails (e.g. no upstream configured), ahead/behind stay 0 without erroring out.

	return &RepoStatus{
		Name:          name,
		Path:          repoPath,
		CurrentBranch: currentBranch,
		IsClean:       isClean,
		AheadCount:    aheadCount,
		BehindCount:   behindCount,
		ModifiedCount: modifiedCount,
	}, nil
}

// GetWorkspaceStatus runs InspectRepo across all target repositories concurrently.
func GetWorkspaceStatus(workspaceRoot string, targets []manifest.RepoTarget) []RepoStatus {
	if len(targets) == 0 {
		return []RepoStatus{}
	}

	results := make([]RepoStatus, len(targets))

	concurrency := 8
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

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
				targetPath := filepath.Join(workspaceRoot, j.target.Path)
				status, err := InspectRepo(targetPath)
				if status == nil {
					msg := ErrMissing
					if err != nil {
						msg = err.Error()
					}
					results[j.index] = RepoStatus{
						Name:  j.target.Name,
						Path:  j.target.Path,
						Error: msg,
					}
					continue
				}
				status.Name = j.target.Name
				status.Path = j.target.Path
				results[j.index] = *status
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
