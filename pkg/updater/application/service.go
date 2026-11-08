// Package application implements the update-check and self-update use cases
// over the Updater port.
package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/updater/domain"
)

// Updater is the release source the use cases read and install from.
type Updater interface {
	Check(ctx context.Context, currentVersion, apiURL, repoSlug string) (*domain.UpdateCheckResult, error)
	CheckCached(ctx context.Context, currentVersion, cacheFile string, ttl time.Duration, apiURL, repoSlug string) (*domain.UpdateCheckResult, error)
	// SelfUpdate installs exactly targetVersion (for example "v0.19.1") or fails.
	SelfUpdate(ctx context.Context, targetVersion, repoSlug string) error
}

// UI carries the decoration cmd/orbit already renders.
type UI struct {
	OK    string
	Info  string
	Error string
	Warn  string
	Arrow string

	Title   func(string) string
	Header  func(string) string
	Bold    func(string) string
	Success func(string) string
	Warning func(string) string
	Subtle  func(string) string
	Code    func(string) string
}

// Service is the updater use-case layer.
type Service struct {
	Updater Updater
	Out     io.Writer
	UI      UI
}

// CheckInput is one update check.
type CheckInput struct {
	CurrentVersion string
	APIURL         string
	RepoSlug       string
}

// SelfUpdateInput is one `orbit self-update` invocation.
type SelfUpdateInput struct {
	CurrentVersion string
	APIURL         string
	RepoSlug       string
	CheckOnly      bool
	Yes            bool
	ExecutablePath string
	// Confirm asks the user to proceed. nil cancels when a prompt would be
	// needed, so non-interactive callers never install by surprise.
	Confirm func(question string, defaultYes bool) bool
}

// SelfUpdateResult is what `orbit self-update` produced.
type SelfUpdateResult struct {
	CurrentVersion string              `json:"currentVersion"`
	LatestVersion  string              `json:"latestVersion"`
	HasUpdate      bool                `json:"hasUpdate"`
	UpToDate       bool                `json:"upToDate"`
	CheckOnly      bool                `json:"checkOnly"`
	Updated        bool                `json:"updated"`
	Cancelled      bool                `json:"cancelled"`
	Highlights     []string            `json:"highlights,omitempty"`
	Release        *domain.ReleaseInfo `json:"release,omitempty"`
}

// Check queries the release API. Errors are returned unwrapped so callers can
// keep their own messages.
func (s *Service) Check(ctx context.Context, in CheckInput) (*domain.UpdateCheckResult, error) {
	return s.Updater.Check(ctx, in.CurrentVersion, in.APIURL, in.RepoSlug)
}

// SelfUpdate checks, reports and installs the latest release.
func (s *Service) SelfUpdate(ctx context.Context, in SelfUpdateInput) (SelfUpdateResult, error) {
	curVer := "v" + strings.TrimPrefix(in.CurrentVersion, "v")
	fmt.Fprintln(s.out(), s.title("Orbit CLI Self-Update"))

	res, err := s.Updater.Check(ctx, in.CurrentVersion, in.APIURL, in.RepoSlug)
	if err != nil {
		return SelfUpdateResult{}, clierr.Wrap(err, clierr.Failed("failed to check for updates"))
	}

	latestVer := "v" + strings.TrimPrefix(res.LatestVersion, "v")
	result := SelfUpdateResult{
		CurrentVersion: curVer,
		LatestVersion:  latestVer,
		HasUpdate:      res.HasUpdate,
		CheckOnly:      in.CheckOnly,
		Release:        res.Release,
	}

	if !res.HasUpdate {
		result.UpToDate = true
		fmt.Fprintf(s.out(), "  %s  %s (Current: %s)\n",
			s.UI.OK,
			s.success("Orbit CLI is already up to date!"),
			s.code(curVer),
		)
		return result, nil
	}

	fmt.Fprintf(s.out(), "  %s  %s %s %s\n",
		s.UI.Info,
		s.code(curVer),
		s.UI.Arrow,
		s.success(latestVer),
	)

	// Display top 5 release highlights if present
	if res.Release != nil && res.Release.ReleaseNotes != "" {
		highlights := domain.ExtractReleaseHighlights(res.Release.ReleaseNotes, 5)
		if len(highlights) > 0 {
			result.Highlights = highlights
			fmt.Fprintf(s.out(), "\n  %s\n", s.bold("Highlights in "+latestVer+":"))
			for _, h := range highlights {
				fmt.Fprintf(s.out(), "    %s %s\n", s.subtle("•"), h)
			}
		}
	}
	fmt.Fprintln(s.out())

	if in.CheckOnly {
		fmt.Fprintf(s.out(), "  Run '%s' to install.\n", s.bold("orbit self-update"))
		return result, nil
	}

	if !in.Yes {
		if in.Confirm == nil || !in.Confirm(fmt.Sprintf("Update Orbit to %s?", latestVer), true) {
			result.Cancelled = true
			fmt.Fprintf(s.out(), "\n  %s  Update cancelled.\n", s.UI.Info)
			return result, nil
		}
	}

	fmt.Fprintf(s.out(), "\n%s\n", s.header("Downloading and installing update..."))

	if err := s.Updater.SelfUpdate(ctx, latestVer, in.RepoSlug); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			execPath := in.ExecutablePath
			if execPath == "" {
				execPath = "/usr/local/bin/orbit"
			}
			return result, clierr.Failed(fmt.Sprintf("insufficient permissions to replace %s. Try running with sudo: sudo orbit self-update", execPath))
		}
		return result, clierr.Wrap(err, clierr.Failed("self-update failed"))
	}

	result.Updated = true
	fmt.Fprintf(s.out(), "  %s  %s\n", s.UI.OK, s.success(fmt.Sprintf("Successfully updated Orbit to %s!", latestVer)))
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

func (s *Service) subtle(text string) string {
	if s.UI.Subtle == nil {
		return text
	}
	return s.UI.Subtle(text)
}

func (s *Service) code(text string) string {
	if s.UI.Code == nil {
		return text
	}
	return s.UI.Code(text)
}
