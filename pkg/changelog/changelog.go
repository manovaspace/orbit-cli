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
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	bulletStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dateStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	tagStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
)

// DefaultReleases provides built-in changelog entries when offline.
var DefaultReleases = []ReleaseEntry{
	{
		Version:     "v0.3.0",
		PublishedAt: time.Date(2026, 8, 25, 19, 16, 0, 0, time.UTC),
		Highlights: []string{
			"Multi-modal version selection in installer ('get.manova.space/vX.Y.Z', MANOVA_VERSION env, --version flag)",
			"Explicit version readout in installer and developer bootstrap completion messages",
			"Strictly verified passive background worker (never modifies binaries in background)",
		},
	},
	{
		Version:     "v0.2.9",
		PublishedAt: time.Date(2026, 8, 25, 19, 10, 0, 0, time.UTC),
		Highlights: []string{
			"New 'manova user' command (list, inspect, lock, unlock, deprovision, rotate-key)",
			"Zero-leak atomic offboarding across LLDAP, Forgejo Git, and WireGuard VPN",
			"Re-entrant init handling when non-root developer user already exists",
		},
	},
	{
		Version:     "v0.2.8",
		PublishedAt: time.Date(2026, 8, 25, 19, 0, 0, 0, time.UTC),
		Highlights: []string{
			"Strict version filtering on release banners (suppressed if binary is already up to date)",
			"Suppressed post-run update notices during initial machine bootstrap ('init') and doc export",
		},
	},
	{
		Version:     "v0.2.7",
		PublishedAt: time.Date(2026, 8, 25, 9, 46, 0, 0, time.UTC),
		Highlights: []string{
			"Full Unix roff man pages for 'manova', 'm', and all subcommands ('man m', 'man manova-doctor')",
			"Automated man page installation on bootstrap and refresh on every self-update",
			"Streamlined and grouped root help UX with categorized command listings",
			"New 'manova doc man' and 'manova doc markdown' generator commands",
		},
	},
	{
		Version:     "v0.2.6",
		PublishedAt: time.Date(2026, 8, 25, 7, 5, 0, 0, time.UTC),
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
		PublishedAt time.Time `json:"published_at"`
		Highlights  []string  `json:"highlights"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && payload.Version != "" {
		// If remote has a newer version than our top built-in, prepend it
		if len(DefaultReleases) == 0 || DefaultReleases[0].Version != payload.Version {
			latest := ReleaseEntry{
				Version:     payload.Version,
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

// FormatReleaseCard formats a single release entry for terminal display.
func FormatReleaseCard(r ReleaseEntry) string {
	var sb strings.Builder

	dateStr := ""
	if !r.PublishedAt.IsZero() {
		dateStr = fmt.Sprintf(" (%s)", r.PublishedAt.Format("2006-01-02"))
	}

	sb.WriteString(fmt.Sprintf("%s%s\n", tagStyle.Render(r.Version), dateStyle.Render(dateStr)))

	for _, h := range r.Highlights {
		sb.WriteString(fmt.Sprintf("  • %s\n", bulletStyle.Render(h)))
	}

	if r.Body != "" {
		sb.WriteString(fmt.Sprintf("  %s\n", dateStyle.Render(r.Body)))
	}

	return sb.String()
}
