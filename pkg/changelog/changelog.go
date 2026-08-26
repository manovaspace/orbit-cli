package changelog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/updater"
)

var (
	cardBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			PaddingLeft(1).PaddingRight(1)

	cardVersionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	cardTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	cardDateStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	cardBulletStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	cardBodyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)

	// Keep backward-compatible exports for legacy callers
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	bulletStyle = cardBulletStyle
	dateStyle   = cardDateStyle
	tagStyle    = cardVersionStyle
)

// DefaultReleases provides built-in changelog entries when offline.
var DefaultReleases = []ReleaseEntry{
	{
		Version:     "v0.1.0",
		PublishedAt: time.Date(2026, 8, 26, 19, 0, 0, 0, time.UTC),
		Title:       "Orbit CLI Initial Release & Workspace Orchestrator",
		Highlights: []string{
			"Orbit Platform & Workspace Orchestrator (`orbit`, shortcut `o`)",
			"Zero-leak developer onboarding with cryptographic token validation (`orbit onboard`)",
			"Centralized developer user lifecycle management (`orbit user`)",
			"Automated multi-repo workspace synchronization (`orbit sync`)",
			"Local container stack orchestration & development routing (`orbit dev`)",
			"Unified Unix roff manual pages (`man orbit`, `man o`)",
		},
	},
}

// FetchReleases fetches the latest release changelog entries from the remote feed,
// falling back to DefaultReleases if unreachable.
func FetchReleases(ctx context.Context, feedURL string) []ReleaseEntry {
	if feedURL == "" {
		feedURL = "https://orbit.manova.space/version"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return DefaultReleases
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return DefaultReleases
	}
	defer resp.Body.Close()

	var payload struct {
		Version     string    `json:"version"`
		Title       string    `json:"title,omitempty"`
		PublishedAt time.Time `json:"published_at"`
		Highlights  []string  `json:"highlights"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && payload.Version != "" {
		if len(DefaultReleases) == 0 || DefaultReleases[0].Version != payload.Version {
			latest := ReleaseEntry{
				Version:     payload.Version,
				Title:       payload.Title,
				PublishedAt: payload.PublishedAt,
				Highlights:  payload.Highlights,
			}
			return append([]ReleaseEntry{latest}, DefaultReleases...)
		}
	}

	return DefaultReleases
}

// GetRecentReleases returns up to limit releases.
func GetRecentReleases(releases []ReleaseEntry, limit int) []ReleaseEntry {
	if limit <= 0 || limit > len(releases) {
		limit = len(releases)
	}
	return releases[:limit]
}

// FindRelease returns the release entry for a specific version tag.
func FindRelease(releases []ReleaseEntry, tag string) *ReleaseEntry {
	norm := strings.ToLower(strings.TrimSpace(tag))
	if !strings.HasPrefix(norm, "v") {
		norm = "v" + norm
	}

	for _, r := range releases {
		if strings.ToLower(r.Version) == norm {
			copyR := r
			return &copyR
		}
	}
	return nil
}

// FormatReleaseCard formats a single release entry as a rounded box card.
func FormatReleaseCard(r ReleaseEntry) string {
	var inner strings.Builder

	// Header line: version  ·  date
	dateStr := ""
	if !r.PublishedAt.IsZero() {
		dateStr = cardDateStyle.Render("  ·  " + r.PublishedAt.Format("2006-01-02"))
	}
	inner.WriteString(fmt.Sprintf("%s%s\n", cardVersionStyle.Render(r.Version), dateStr))

	// Optional title
	if r.Title != "" {
		inner.WriteString(cardTitleStyle.Render(r.Title) + "\n")
	}

	// Blank separator before bullets
	inner.WriteString("\n")

	// Highlight bullets
	for _, h := range r.Highlights {
		inner.WriteString(fmt.Sprintf("  %s %s\n",
			cardVersionStyle.Render("•"),
			updater.FormatTerminalHighlight(h),
		))
	}

	// Optional body note
	if r.Body != "" {
		inner.WriteString("\n" + cardBodyStyle.Render("  "+r.Body) + "\n")
	}

	return cardBorderStyle.Render(inner.String())
}

// FormatAllCards renders all releases as cards separated by a blank line.
func FormatAllCards(releases []ReleaseEntry) string {
	var sb strings.Builder
	for i, r := range releases {
		sb.WriteString(FormatReleaseCard(r))
		if i < len(releases)-1 {
			sb.WriteString("\n\n")
		}
	}
	return sb.String()
}
