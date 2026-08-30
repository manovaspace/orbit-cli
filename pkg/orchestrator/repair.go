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

// RepairTarget attaches git metadata to a gitless working tree without
// checkout -f or reset --hard. Existing files stay as they are; git status
// then shows the delta against origin.
func RepairTarget(workspaceRoot string, target manifest.RepoTarget) RepairResult {
	if target.Path == "" {
		return RepairResult{Name: target.Name, Success: false, Error: "target path is empty"}
	}
	if target.RemoteURL == "" {
		return RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: "remote URL is empty"}
	}

	destPath := filepath.Join(workspaceRoot, target.Path)
	gitPath := filepath.Join(destPath, ".git")

	if _, err := os.Stat(gitPath); err == nil {
		return RepairResult{
			Name:    target.Name,
			Path:    target.Path,
			Success: true,
			Skipped: "already a git repository",
		}
	}

	info, err := os.Stat(destPath)
	if err != nil {
		if os.IsNotExist(err) {
			clone := CloneTarget(workspaceRoot, target)
			return RepairResult{
				Name:    target.Name,
				Path:    target.Path,
				Success: clone.Success,
				Error:   clone.Error,
			}
		}
		return RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: err.Error()}
	}
	if !info.IsDir() {
		return RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: "path is not a directory"}
	}

	tmp, err := os.MkdirTemp("", "orbit-repair-*")
	if err != nil {
		return RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: err.Error()}
	}
	defer os.RemoveAll(tmp)

	cloneDir := filepath.Join(tmp, "clone")
	cmd := exec.Command("git", "clone", "--", target.RemoteURL, cloneDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: fmt.Sprintf("git clone failed: %s", msg)}
	}

	srcGit := filepath.Join(cloneDir, ".git")
	if _, err := os.Stat(srcGit); err != nil {
		return RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: "clone produced no .git directory"}
	}

	cp := exec.Command("cp", "-a", srcGit, gitPath)
	if out, err := cp.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return RepairResult{Name: target.Name, Path: target.Path, Success: false, Error: fmt.Sprintf("copy .git failed: %s", msg)}
	}

	return RepairResult{Name: target.Name, Path: target.Path, Success: true}
}

// RepairTargets runs RepairTarget across targets with a worker pool.
func RepairTargets(workspaceRoot string, targets []manifest.RepoTarget, concurrency int, callback func(RepairResult)) []RepairResult {
	if len(targets) == 0 {
		return []RepairResult{}
	}
	if concurrency <= 0 {
		concurrency = defaultCloneConcurrency
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

	results := make([]RepairResult, len(targets))
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
				res := RepairTarget(workspaceRoot, j.target)
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
