package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

const defaultCloneConcurrency = 4

// CloneTarget clones a single repository target into the workspace.
// If the destination directory already contains a .git directory/file, it skips cloning
// and returns AlreadyExists: true.
func CloneTarget(workspaceRoot string, target manifest.RepoTarget) CloneResult {
	if target.Path == "" {
		return CloneResult{
			Name:    target.Name,
			Path:    target.Path,
			Success: false,
			Error:   "target path is empty",
		}
	}

	destPath := filepath.Join(workspaceRoot, target.Path)
	gitPath := filepath.Join(destPath, ".git")

	// Check if destination directory already exists
	if fi, err := os.Stat(destPath); err == nil && fi.IsDir() {
		// If .git exists, it's an existing repo
		if _, err := os.Stat(gitPath); err == nil {
			return CloneResult{
				Name:          target.Name,
				Path:          target.Path,
				Success:       true,
				AlreadyExists: true,
			}
		}

		// If directory has files, treat as already present
		entries, err := os.ReadDir(destPath)
		if err == nil && len(entries) > 0 {
			return CloneResult{
				Name:          target.Name,
				Path:          target.Path,
				Success:       true,
				AlreadyExists: true,
			}
		}
	}

	if target.RemoteURL == "" {
		return CloneResult{
			Name:    target.Name,
			Path:    target.Path,
			Success: false,
			Error:   "remote URL is empty",
		}
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(destPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return CloneResult{
			Name:    target.Name,
			Path:    target.Path,
			Success: false,
			Error:   fmt.Sprintf("failed to create parent directory: %v", err),
		}
	}

	cmd := exec.Command("git", "clone", target.RemoteURL, target.Path)
	cmd.Dir = workspaceRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(out))
		if strings.Contains(errMsg, "already exists and is not an empty directory") {
			return CloneResult{
				Name:          target.Name,
				Path:          target.Path,
				Success:       true,
				AlreadyExists: true,
			}
		}
		if errMsg == "" {
			errMsg = err.Error()
		}
		return CloneResult{
			Name:    target.Name,
			Path:    target.Path,
			Success: false,
			Error:   errMsg,
		}
	}

	return CloneResult{
		Name:    target.Name,
		Path:    target.Path,
		Success: true,
	}
}

// CloneTargets executes CloneTarget across multiple repository targets in parallel using a worker pool.
// If callback is non-nil, it is called whenever a target finishes cloning.
func CloneTargets(workspaceRoot string, targets []manifest.RepoTarget, concurrency int, callback func(CloneResult)) []CloneResult {
	if len(targets) == 0 {
		return []CloneResult{}
	}

	if concurrency <= 0 {
		concurrency = defaultCloneConcurrency
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

	results := make([]CloneResult, len(targets))

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
				res := CloneTarget(workspaceRoot, j.target)
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
