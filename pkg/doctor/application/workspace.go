package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
)

// workspaceCheck verifies that every manifest repository path exists and still
// carries a .git entry. It inspects the filesystem only: it never runs git, so
// it cannot refresh an index or mutate a repository.
type workspaceCheck struct {
	host     Host
	manifest Manifest
	root     string
	path     string
}

func (c workspaceCheck) Name() string { return "workspace" }

func (c workspaceCheck) Run(context.Context) []domain.DiagnosticResult {
	if _, err := c.host.Stat(c.path); err != nil {
		return []domain.DiagnosticResult{{
			Category:      "Workspace",
			Name:          "Manifest",
			Status:        domain.StatusError,
			Message:       "Workspace targets unverified: cannot inspect workspace.yaml",
			FixSuggestion: "Provide a readable workspace.yaml for workspace diagnostics.",
		}}
	}
	m, err := c.manifest.Load(c.path)
	if err != nil {
		return []domain.DiagnosticResult{{
			Category:      "Workspace",
			Name:          "Manifest",
			Status:        domain.StatusError,
			Message:       "Workspace targets unverified: cannot load workspace.yaml",
			FixSuggestion: "Fix workspace.yaml readability or parse errors.",
		}}
	}
	targets := m.ResolveScope("all")
	if len(targets) == 0 {
		return []domain.DiagnosticResult{{
			Category:      "Workspace",
			Name:          "Manifest",
			Status:        domain.StatusError,
			Message:       "Workspace targets unverified: workspace.yaml declares no repository targets",
			FixSuggestion: "Declare the intended repositories before workspace diagnostics.",
		}}
	}

	var results []domain.DiagnosticResult
	gitless, missing, inspectionErrors := 0, 0, 0
	for _, target := range targets {
		repoPath := filepath.Join(c.root, target.Path)
		info, err := c.host.Stat(repoPath)
		if errors.Is(err, fs.ErrNotExist) {
			missing++
			continue
		}
		if err != nil || !info.IsDir {
			inspectionErrors++
			results = append(results, domain.DiagnosticResult{Category: "Workspace", Name: target.Name, Status: domain.StatusError, Message: "Cannot inspect repository directory"})
			continue
		}
		if _, err := c.host.Lstat(filepath.Join(repoPath, ".git")); errors.Is(err, fs.ErrNotExist) {
			gitless++
		} else if err != nil {
			inspectionErrors++
			results = append(results, domain.DiagnosticResult{Category: "Workspace", Name: target.Name, Status: domain.StatusError, Message: "Cannot inspect repository .git entry"})
		}
	}

	switch {
	case gitless == 0 && missing == 0 && inspectionErrors == 0:
		results = append(results, domain.DiagnosticResult{
			Category: "Workspace",
			Name:     "Git trees",
			Status:   domain.StatusOK,
			Message:  fmt.Sprintf("all %d manifest paths have .git (repository integrity unverified)", len(targets)),
		})
	default:
		if gitless > 0 {
			results = append(results, domain.DiagnosticResult{
				Category:      "Workspace",
				Name:          "Gitless trees",
				Status:        domain.StatusWarning,
				Message:       fmt.Sprintf("%d manifest path(s) have files but no .git", gitless),
				FixSuggestion: "orbit repair",
			})
		}
		if missing > 0 {
			results = append(results, domain.DiagnosticResult{
				Category:      "Workspace",
				Name:          "Uncloned repos",
				Status:        domain.StatusWarning,
				Message:       fmt.Sprintf("%d manifest path(s) are not cloned", missing),
				FixSuggestion: "orbit init all",
			})
		}
	}
	return results
}
