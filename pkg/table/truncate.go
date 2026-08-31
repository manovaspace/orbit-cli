package table

import (
	"strings"
)

func TruncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if StringWidth(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}

	runes := []rune(s)
	for i := min(len(runes), maxLen); i >= 0; i-- {
		candidate := string(runes[:i]) + "…"
		if StringWidth(candidate) <= maxLen {
			return candidate
		}
	}
	return "…"
}

func TruncatePath(path string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if StringWidth(path) <= maxLen {
		return path
	}
	if maxLen <= 5 {
		return TruncateString(path, maxLen)
	}

	parts := strings.Split(path, "/")
	if len(parts) <= 1 {
		return TruncateString(path, maxLen)
	}

	prefix := parts[0] + "/"
	suffix := parts[len(parts)-1]

	candidate := prefix + "…/" + suffix
	if StringWidth(candidate) <= maxLen {
		return candidate
	}

	candidate = "…/" + suffix
	if StringWidth(candidate) <= maxLen {
		return candidate
	}

	return TruncateString(path, maxLen)
}
