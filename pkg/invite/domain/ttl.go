package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseTTL parses a human duration such as "7d", "24h" or "168h". A blank
// value means seven days.
func ParseTTL(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 7 * 24 * time.Hour, nil
	}

	// Handle day suffix "d" or "D", e.g. "7d", "14d", "1d"
	if strings.HasSuffix(s, "d") || strings.HasSuffix(s, "D") {
		daysStr := s[:len(s)-1]
		days, err := strconv.Atoi(daysStr)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid day duration %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration format %q (e.g., '7d', '24h', '168h'): %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be greater than zero, got %v", d)
	}
	return d, nil
}

// FormatRemainingLabel renders a remaining duration with a trailing "remaining"
// word for the human invitation summary.
func FormatRemainingLabel(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	if d > 24*time.Hour {
		days := int(d.Hours() / 24)
		hours := int(d.Hours()) % 24
		if hours == 0 {
			return fmt.Sprintf("%dd remaining", days)
		}
		return fmt.Sprintf("%dd %dh remaining", days, hours)
	}
	if d > time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm remaining", hours, mins)
	}
	return fmt.Sprintf("%dm remaining", int(d.Minutes()))
}
