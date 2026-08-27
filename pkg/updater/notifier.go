package updater

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	notifyBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1).
			MarginTop(1)

	notifyArrowStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8"))

	notifyCurrentVerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("13"))

	notifyLatestVerStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("10"))

	notifyCmdStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14"))
)

// ShouldSuppressNotification returns true if update notifications should be silenced.
func ShouldSuppressNotification() bool {
	if os.Getenv("CI") == "true" || os.Getenv("ORBIT_NO_UPDATE_NOTIFIER") == "1" {
		return true
	}
	return false
}

// NotifyIfUpdateAvailable reads the cached update check and prints a polite,
// non-intrusive notification if a newer version exists.
func NotifyIfUpdateAvailable(out io.Writer, currentVersion string) {
	if ShouldSuppressNotification() {
		return
	}

	result, err := CheckUpdateCached(currentVersion, "", DefaultTTL, "", "")
	if err != nil || result == nil || !result.HasUpdate {
		return
	}

	curVer := "v" + strings.TrimPrefix(currentVersion, "v")
	latestVer := "v" + strings.TrimPrefix(result.LatestVersion, "v")

	line1 := fmt.Sprintf("A new release of Orbit is available: %s %s %s",
		notifyCurrentVerStyle.Render(curVer),
		notifyArrowStyle.Render("→"),
		notifyLatestVerStyle.Render(latestVer),
	)
	line2 := fmt.Sprintf("To upgrade, run: %s", notifyCmdStyle.Render("o self-update"))

	content := fmt.Sprintf("%s\n%s", line1, line2)
	fmt.Fprintln(out, notifyBoxStyle.Render(content))
}
