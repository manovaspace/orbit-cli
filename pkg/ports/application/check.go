package application

import (
	"fmt"
	"sort"

	"github.com/manovaspace/orbit-cli/pkg/ports/domain"
)

// SourceFile is one scannable file, with the repository it belongs to ("" when
// no repository owns it).
type SourceFile struct {
	Rel  string
	Repo string
}

// Source lists the scannable files under a workspace root and reads them. The
// infrastructure layer walks the disk; the use case stays file-system free.
type Source interface {
	Files(root string) ([]SourceFile, error)
	Read(root, rel string) (string, error)
}

// CheckResult is one `o port check` run. Failing counts findings that make the
// command exit 1; Warnings counts the rest.
type CheckResult struct {
	Root        string           `json:"root"`
	Registry    string           `json:"registry"`
	Files       int              `json:"files"`
	Occurrences int              `json:"occurrences"`
	Findings    []domain.Finding `json:"findings"`
	Counts      map[string]int   `json:"counts"`
	Failing     int              `json:"failing"`
	Warnings    int              `json:"warnings"`
	Strict      bool             `json:"strict"`
}

// Checker runs the port rules over every scannable file of a workspace.
type Checker struct {
	Source Source
}

// Run classifies every port occurrence against reg. strict makes warnings
// count as failing.
func (c Checker) Run(root, registryPath string, reg *domain.Registry, strict bool) (CheckResult, error) {
	files, err := c.Source.Files(root)
	if err != nil {
		return CheckResult{}, fmt.Errorf("list scannable files: %w", err)
	}

	result := CheckResult{
		Root:     root,
		Registry: registryPath,
		Findings: []domain.Finding{},
		Counts:   make(map[string]int, len(domain.AllKinds)),
		Strict:   strict,
	}
	for _, kind := range domain.AllKinds {
		result.Counts[string(kind)] = 0
	}

	for _, f := range files {
		content, err := c.Source.Read(root, f.Rel)
		if err != nil {
			return CheckResult{}, fmt.Errorf("read %s: %w", f.Rel, err)
		}
		result.Files++
		for _, occ := range domain.Extract(f.Rel, content) {
			result.Occurrences++
			for _, finding := range domain.Classify(reg, occ, f.Repo) {
				result.Findings = append(result.Findings, finding)
				result.Counts[string(finding.Kind)]++
				switch {
				case finding.Severity == domain.SeverityError, strict:
					result.Failing++
				default:
					result.Warnings++
				}
			}
		}
	}

	sort.SliceStable(result.Findings, func(i, j int) bool {
		a, b := result.Findings[i], result.Findings[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.Kind < b.Kind
	})
	return result, nil
}
