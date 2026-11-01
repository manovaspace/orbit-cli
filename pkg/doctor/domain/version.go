package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ParseSemver parses semantic version from tool version outputs.
func ParseSemver(raw string, tool string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty version string")
	}

	var pattern *regexp.Regexp
	switch strings.ToLower(tool) {
	case "go":
		pattern = regexp.MustCompile(`go([0-9]+(?:\.[0-9]+)*)`)
	case "typst":
		pattern = regexp.MustCompile(`(?:typst\s+)?v?([0-9]+(?:\.[0-9]+)*)`)
	case "docker-compose":
		pattern = regexp.MustCompile(`(?:version\s+)?v?([0-9]+(?:\.[0-9]+)*)`)
	default:
		pattern = regexp.MustCompile(`v?([0-9]+(?:\.[0-9]+)*)`)
	}

	matches := pattern.FindStringSubmatch(raw)
	if len(matches) > 1 && matches[1] != "" {
		return matches[1], nil
	}

	generic := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)+)`)
	matches = generic.FindStringSubmatch(raw)
	if len(matches) > 1 && matches[1] != "" {
		return matches[1], nil
	}

	return "", fmt.Errorf("unable to parse %s version from %q", tool, raw)
}

// CompareVersions compares two semver strings (e.g. "1.23.1", "1.23", "22.8.0").
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2.
func CompareVersions(v1, v2 string) int {
	p1 := parseVersionComponents(v1)
	p2 := parseVersionComponents(v2)

	maxLen := len(p1)
	if len(p2) > maxLen {
		maxLen = len(p2)
	}

	for i := 0; i < maxLen; i++ {
		n1 := 0
		if i < len(p1) {
			n1 = p1[i]
		}
		n2 := 0
		if i < len(p2) {
			n2 = p2[i]
		}
		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}

	return 0
}

func parseVersionComponents(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = strings.TrimPrefix(v, "go")
	parts := strings.Split(v, ".")
	var res []int
	for _, p := range parts {
		var numStr string
		for _, r := range p {
			if unicode.IsDigit(r) {
				numStr += string(r)
			} else {
				break
			}
		}
		if numStr == "" {
			break
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			break
		}
		res = append(res, n)
	}
	return res
}
