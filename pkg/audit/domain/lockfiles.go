package domain

import (
	"sort"
	"strings"
)

// ParseBunLockAll reads every resolved package entry of a bun.lock file.
func ParseBunLockAll(text string) []Resolved {
	var out []Resolved
	for _, line := range strings.Split(text, "\n") {
		match := bunLockEntry.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		name, version, ok := splitPackageSpec(match[2])
		if !ok {
			continue
		}
		out = append(out, Resolved{Key: match[1], Name: name, Version: version})
	}
	return uniqueResolved(out)
}

// ParseGoSum reads the module versions of a go.sum file. The duplicate
// "/go.mod" hash line of the same version is skipped.
func ParseGoSum(text string) []Resolved {
	var out []Resolved
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		name, version := fields[0], fields[1]
		if strings.HasSuffix(version, "/go.mod") {
			continue
		}
		if name == "" || version == "" {
			continue
		}
		out = append(out, Resolved{Name: name, Version: version})
	}
	return uniqueResolved(out)
}

func uniqueResolved(values []Resolved) []Resolved {
	seen := map[string]bool{}
	out := make([]Resolved, 0, len(values))
	for _, value := range values {
		key := value.Name + "\x00" + value.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}
