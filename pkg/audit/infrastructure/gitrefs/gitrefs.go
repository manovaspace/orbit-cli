// Package gitrefs reads local git refs from .git files. It never runs git and
// never contacts a remote, so the audit cannot update a repository.
package gitrefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Refs reads HEAD and origin tracking refs.
type Refs struct{}

// New builds the adapter.
func New() *Refs { return &Refs{} }

// Head returns the local HEAD commit and the fetched origin default-branch
// commit. A repository without a fetched origin returns an empty origin.
func (r *Refs) Head(dir, branch string) (string, string, error) {
	gitDir, err := resolveGitDir(dir)
	if err != nil {
		return "", "", err
	}
	commonDir, err := commonDir(gitDir)
	if err != nil {
		return "", "", err
	}
	head, err := readRef(gitDir, commonDir, "HEAD")
	if err != nil {
		return "", "", err
	}
	if branch == "" {
		branch = "main"
	}
	origin, err := readRef(gitDir, commonDir, "refs/remotes/origin/"+branch)
	if err != nil {
		target, symErr := readRef(gitDir, commonDir, "refs/remotes/origin/HEAD")
		if symErr == nil {
			origin = target
		}
	}
	if origin == "" {
		return head, "", nil
	}
	return head, origin, nil
}

func resolveGitDir(dir string) (string, error) {
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", fmt.Errorf("%s: %w", dotGit, err)
	}
	if info.IsDir() {
		return dotGit, nil
	}
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("%s: unrecognised gitdir file", dotGit)
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return filepath.Clean(target), nil
}

// commonDir follows the commondir file worktrees use for shared refs.
func commonDir(gitDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir, nil
	}
	target := strings.TrimSpace(string(data))
	if target == "" {
		return gitDir, nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(gitDir, target)
	}
	return filepath.Clean(target), nil
}

func readRef(gitDir, commonDir, ref string) (string, error) {
	if ref == "HEAD" {
		data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
		if err != nil {
			return "", err
		}
		line := strings.TrimSpace(string(data))
		if target, ok := strings.CutPrefix(line, "ref:"); ok {
			return readRef(gitDir, commonDir, strings.TrimSpace(target))
		}
		return line, nil
	}
	if data, err := os.ReadFile(filepath.Join(commonDir, filepath.FromSlash(ref))); err == nil {
		value := strings.TrimSpace(string(data))
		if target, ok := strings.CutPrefix(value, "ref:"); ok {
			return readRef(gitDir, commonDir, strings.TrimSpace(target))
		}
		if value != "" {
			return value, nil
		}
	}
	data, err := os.ReadFile(filepath.Join(commonDir, "packed-refs"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == ref {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("ref %s not found", ref)
}
