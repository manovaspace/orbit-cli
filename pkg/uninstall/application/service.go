// Package application implements the uninstall use case over a filesystem port.
package application

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/uninstall/domain"
)

// FS is the filesystem and process seam the uninstall use case needs. It is
// deliberately small so tests can fake every removal.
type FS interface {
	Stat(path string) (domain.FileInfo, bool, error)
	RemoveElevated(ctx context.Context, path string) error
	RemoveAll(path string) error
	Glob(pattern string) ([]string, error)
	RefreshManPages(ctx context.Context)
	HomeDir() (string, error)
	Executable() (string, error)
}

// UI carries the decoration cmd/orbit already renders.
type UI struct {
	OK    string
	Warn  string
	Error string
	Info  string

	Title     func(string) string
	Success   func(string) string
	Warning   func(string) string
	ErrorText func(string) string
	Subtle    func(string) string
}

// Service is the uninstall use-case layer.
type Service struct {
	FS  FS
	Out io.Writer
	UI  UI
}

// Input is one `orbit uninstall` invocation.
type Input struct {
	Force          bool
	PurgeState     bool
	PurgeWorkspace bool
	Interactive    bool
	// DirtyRepos lists repositories with uncommitted changes, already
	// formatted for display.
	DirtyRepos []string
	// Confirm asks before uninstalling. nil cancels when a prompt would be
	// needed.
	Confirm func(question string, defaultYes bool) bool
}

// Uninstall removes the binaries, completions, man pages and optional state.
func (s *Service) Uninstall(ctx context.Context, in Input) (domain.Result, error) {
	result := domain.Result{
		RemovedBinaries:    []string{},
		RemovedCompletions: []string{},
		RemovedManPages:    []string{},
		PurgedState:        []string{},
	}

	if len(in.DirtyRepos) > 0 {
		fmt.Fprintf(s.out(), "\n  %s  %s\n", s.UI.Warn, s.warning("Workspace Uncommitted Changes Detected:"))
		for _, dr := range in.DirtyRepos {
			fmt.Fprintf(s.out(), "     • %s\n", dr)
		}
		if !in.PurgeWorkspace {
			fmt.Fprintln(s.out(), s.subtle("     (Note: Your source code repositories will remain intact on disk)"))
		}
		fmt.Fprintln(s.out())
	}

	if in.PurgeWorkspace && len(in.DirtyRepos) > 0 && !in.Force {
		return result, clierr.Failed(fmt.Sprintf("cannot purge workspace: %d repositories have uncommitted changes. Commit or stash them first, or pass --force", len(in.DirtyRepos)))
	}

	if !in.Force && in.Interactive {
		fmt.Fprintln(s.out(), s.title("Orbit CLI Uninstaller"))
		if in.Confirm == nil || !in.Confirm("Are you sure you want to uninstall Orbit CLI from this system?", false) {
			fmt.Fprintln(s.out(), s.subtle("Uninstallation aborted."))
			result.NoBinaries = true
			return result, nil
		}
	}

	fmt.Fprintln(s.out(), s.title("Uninstalling Orbit CLI..."))

	home, err := s.FS.HomeDir()
	if err != nil {
		home = ""
	}
	candidates := []string{
		"/usr/local/bin/orbit",
		"/usr/local/bin/o",
		filepath.Join(home, ".local", "bin", "orbit"),
		filepath.Join(home, ".local", "bin", "o"),
	}
	if execPath, err := s.FS.Executable(); err == nil && execPath != "" {
		candidates = append(candidates, execPath)
	}

	seen := make(map[string]bool)
	for _, path := range candidates {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		info, exists, statErr := s.FS.Stat(path)
		if statErr != nil || !exists || info.IsDir {
			continue
		}
		if err := s.FS.RemoveElevated(ctx, path); err == nil {
			fmt.Fprintf(s.out(), "  %s  Removed binary: %s\n", s.UI.OK, s.subtle(path))
			result.RemovedBinaries = append(result.RemovedBinaries, path)
		} else {
			fmt.Fprintf(s.out(), "  %s  Failed to remove binary %s: %v\n", s.UI.Error, path, err)
		}
	}

	// Clean shell completions
	completionCandidates := []string{
		filepath.Join(home, ".local", "share", "zsh", "site-functions", "_orbit"),
		filepath.Join(home, ".local", "share", "zsh", "site-functions", "_o"),
	}
	for _, cp := range completionCandidates {
		if _, exists, err := s.FS.Stat(cp); err == nil && exists {
			_ = s.FS.RemoveElevated(ctx, cp)
			fmt.Fprintf(s.out(), "  %s  Removed shell completion: %s\n", s.UI.OK, s.subtle(cp))
			result.RemovedCompletions = append(result.RemovedCompletions, cp)
		}
	}

	// Clean installed man pages
	manDirs := []string{
		filepath.Join(home, ".local", "share", "man", "man1"),
	}
	for _, md := range manDirs {
		matches, err := s.FS.Glob(filepath.Join(md, "orbit*.1"))
		if err != nil {
			continue
		}
		for _, mf := range matches {
			if err := s.FS.RemoveElevated(ctx, mf); err == nil {
				fmt.Fprintf(s.out(), "  %s  Removed man page: %s\n", s.UI.OK, s.subtle(mf))
				result.RemovedManPages = append(result.RemovedManPages, mf)
			}
		}
	}
	s.FS.RefreshManPages(ctx)

	// Purge state directories if requested
	if in.PurgeState {
		stateDirs := []string{
			filepath.Join(home, ".orbit"),
			filepath.Join(home, ".config", "orbit"),
		}
		for _, sd := range stateDirs {
			if _, exists, err := s.FS.Stat(sd); err == nil && exists {
				_ = s.FS.RemoveAll(sd)
				fmt.Fprintf(s.out(), "  %s  Purged workspace state cache: %s\n", s.UI.OK, s.subtle(sd))
				result.PurgedState = append(result.PurgedState, sd)
			}
		}
	}

	if len(result.RemovedBinaries) == 0 {
		result.NoBinaries = true
		fmt.Fprintln(s.out(), s.subtle("No active Orbit binaries found in standard locations."))
	} else {
		fmt.Fprintf(s.out(), "\n  %s  %s\n\n", s.UI.OK, s.success("Orbit CLI uninstalled successfully."))
	}
	return result, nil
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) title(text string) string {
	if s.UI.Title == nil {
		return text
	}
	return s.UI.Title(text)
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

func (s *Service) subtle(text string) string {
	if s.UI.Subtle == nil {
		return text
	}
	return s.UI.Subtle(text)
}
