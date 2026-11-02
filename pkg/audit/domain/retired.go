package domain

import "strings"

// retiredFolders are path segments whose documents are historical evidence,
// test data or generated fixtures rather than active guidance, so the
// retired-name sweep ignores them.
var retiredFolders = []string{
	"/archive/", "/plans/", "/specs/", "/history/", "/reviews/", "/migrations/",
	"/fixtures/", "/testdata/",
}

// retiredDocName is one name active handbook docs must not mention.
type retiredDocName struct {
	term string
	// adrOnly keeps the term legal inside an ADR under
	// handbook/docs/orbit/decisions/: decisions record history, so a retired
	// name there is evidence, not stale guidance.
	adrOnly bool
	// expected is the finding text the sweep reports; empty means the default.
	expected string
	fix      string
}

// retiredDocNames are the names the handbook retired. Active docs must point at
// the replacement instead of the old one.
var retiredDocNames = []retiredDocName{
	{
		term:     "handbook/cursor",
		adrOnly:  true,
		expected: "handbook/cursor only in an ADR under handbook/docs/orbit/decisions/",
		fix:      "remove the handbook/cursor path; agent configuration lives in handbook/agents/, and only an ADR may keep the old name",
	},
	{
		term:     "Forgejo",
		adrOnly:  true,
		expected: "Forgejo only in an ADR under handbook/docs/orbit/decisions/",
		fix:      "move the Forgejo reference into an ADR under handbook/docs/orbit/decisions/ or remove it",
	},
}

// RetiredHit is one retired name mentioned by an active handbook doc.
type RetiredHit struct {
	File     string
	Line     int
	Term     string
	Text     string
	Expected string
	Fix      string
}

// ActiveDocPath reports whether path is Markdown the retired-name sweep reads:
// a document under handbook/docs or handbook/agents, outside the archived,
// planning, history, review, migration, fixture and testdata folders and not a
// CHANGELOG.
func ActiveDocPath(path string) bool {
	slashed := "/" + strings.Trim(filepathSlash(path), "/") + "/"
	if !strings.HasPrefix(slashed, "/handbook/docs/") && !strings.HasPrefix(slashed, "/handbook/agents/") {
		return false
	}
	lower := strings.ToLower(slashed)
	if !strings.HasSuffix(lower, ".md/") && !strings.HasSuffix(lower, ".mdx/") {
		return false
	}
	for _, folder := range retiredFolders {
		if strings.Contains(slashed, folder) {
			return false
		}
	}
	return !strings.EqualFold(docBase(path), "CHANGELOG.md")
}

// ADRDocPath reports whether path is an architecture decision record.
func ADRDocPath(path string) bool {
	slashed := "/" + strings.Trim(filepathSlash(path), "/") + "/"
	return strings.Contains(slashed, "/handbook/docs/orbit/decisions/")
}

// RetiredHits returns every retired-name mention in one active handbook doc.
// The match is case-insensitive; the finding keeps the canonical spelling. A
// term marked ADR-only is skipped inside ADRs.
func RetiredHits(path string, content []byte) []RetiredHit {
	path = filepathSlash(path)
	if !ActiveDocPath(path) {
		return nil
	}
	adr := ADRDocPath(path)
	var hits []RetiredHit
	for index, line := range strings.Split(string(content), "\n") {
		line = strings.TrimRight(line, "\r")
		lower := strings.ToLower(line)
		for _, name := range retiredDocNames {
			if name.adrOnly && adr {
				continue
			}
			if !strings.Contains(lower, strings.ToLower(name.term)) {
				continue
			}
			expected := name.expected
			if expected == "" {
				expected = "no mention of " + name.term + " in active docs"
			}
			hits = append(hits, RetiredHit{
				File:     path,
				Line:     index + 1,
				Term:     name.term,
				Text:     strings.TrimSpace(line),
				Expected: expected,
				Fix:      name.fix,
			})
		}
	}
	return hits
}

// docBase returns the final path element without importing path/filepath.
func docBase(path string) string {
	slashed := filepathSlash(path)
	if index := strings.LastIndex(slashed, "/"); index >= 0 {
		return slashed[index+1:]
	}
	return slashed
}
