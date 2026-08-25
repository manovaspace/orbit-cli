package changelog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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
		Version:     "v0.3.2",
		PublishedAt: time.Date(2026, 8, 25, 19, 48, 0, 0, time.UTC),
		Title:       "Single Unified Man Page & Purged Subpages",
		Highlights: []string{
			"Single comprehensive man page consolidating all commands, flags, files, and examples",
			"Auto-purge of 50+ legacy fragmented subpages on every update",
			"Full in-pager search across all commands and options ('man manova', 'man m')",
		},
	},
	{
		Version:     "v0.3.1",
		PublishedAt: time.Date(2026, 8, 25, 19, 24, 0, 0, time.UTC),
		Title:       "Multi-Modal Version Pinning & Terminal Changelog",
		Highlights: []string{
			"Install any version: get.manova.space/v0.2.8, MANOVA_VERSION env var, or --version flag",
			"New 'manova changelog' (aliases: whatsnew, news) terminal release viewer",
			"Pin specific version upgrades/downgrades via 'manova self-update --version <tag>'",
		},
	},
	{
		Version:     "v0.3.0",
		PublishedAt: time.Date(2026, 8, 25, 19, 16, 0, 0, time.UTC),
		Title:       "Explicit Version Diagnostics & Verified Passive Worker",
		Highlights: []string{
			"Multi-modal version selection in installer ('get.manova.space/vX.Y.Z', MANOVA_VERSION env, --version flag)",
			"Explicit version readout in installer and developer bootstrap completion messages",
			"Strictly verified passive background worker (never modifies binaries in background)",
		},
	},
	{
		Version:     "v0.2.9",
		PublishedAt: time.Date(2026, 8, 25, 19, 10, 0, 0, time.UTC),
		Title:       "Developer User Management",
		Highlights: []string{
			"New 'manova user' command (list, inspect, lock, unlock, deprovision, rotate-key)",
			"Zero-leak atomic offboarding across LLDAP, Forgejo Git, and WireGuard VPN",
			"Re-entrant init handling when non-root developer user already exists",
		},
	},
	{
		Version:     "v0.2.8",
		PublishedAt: time.Date(2026, 8, 25, 19, 0, 0, 0, time.UTC),
		Title:       "Smart Update Notification Filtering",
		Highlights: []string{
			"Strict version filtering on release banners (suppressed if binary is already up to date)",
			"Suppressed post-run update notices during initial machine bootstrap ('init') and doc export",
		},
	},
	{
		Version:     "v0.2.7",
		PublishedAt: time.Date(2026, 8, 25, 9, 46, 0, 0, time.UTC),
		Title:       "Unix Man Pages & Streamlined Help UX",
		Highlights: []string{
			"Full Unix roff man pages for 'manova', 'm', and all subcommands",
			"Automated man page installation on bootstrap and refresh on every self-update",
			"Streamlined and grouped root help UX with categorized command listings",
			"New 'manova doc man' and 'manova doc markdown' generator commands",
		},
	},
	{
		Version:     "v0.2.6",
		PublishedAt: time.Date(2026, 8, 25, 7, 5, 0, 0, time.UTC),
		Title:       "Bootstrap Experience & Shell Integration",
		Highlights: []string{
			"Installer automatically hands off to 'manova init --bootstrap' or 'manova onboard'",
			"Dedicated 'dev' user creation prompt when running on fresh VPS as root",
			"Single-prompt sudo warmup cache for unprivileged developer setups",
			"Automatic ~/.zshrc configuration with Oh My Zsh and 'm' alias",
		},
	},
}

// FetchReleases fetches the latest release changelog entries from the remote feed,
// falling back to DefaultReleases if unreachable.
func FetchReleases(ctx context.Context, feedURL string) []ReleaseEntry {
	if feedURL == "" {
		feedURL = "https://get.manova.space/version"
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
// The card header shows the version tag + date; the title (if set) is on a
// subtitle line; highlights are indented bullet points; an optional body note
// is rendered in muted italic at the bottom.
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
			cardBulletStyle.Render(h),
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
