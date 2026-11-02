package manifest

import (
	"regexp"
	"strings"
)

// MatchRepos returns the repositories whose Name matches pattern. Only '*' is
// special: it matches any run of characters, including an empty one. Every
// other character is literal and matching is case-insensitive.
func (m *WorkspaceManifest) MatchRepos(pattern string) []RepoTarget {
	re := matchExpression(pattern)
	all := m.AllRepos()
	matched := make([]RepoTarget, 0, len(all))
	for _, target := range all {
		if re.MatchString(target.Name) {
			matched = append(matched, target)
		}
	}
	return matched
}

// matchExpression turns a '*' wildcard pattern into an anchored, case-insensitive
// regular expression. Splitting on '*' and quoting each segment keeps every
// other character (including regular expression metacharacters) literal.
func matchExpression(pattern string) *regexp.Regexp {
	segments := strings.Split(pattern, "*")
	for i, segment := range segments {
		segments[i] = regexp.QuoteMeta(segment)
	}
	return regexp.MustCompile("(?i)^" + strings.Join(segments, ".*") + "$")
}
