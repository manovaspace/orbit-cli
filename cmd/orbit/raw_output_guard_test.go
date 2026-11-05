package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rawOutputCall matches a direct fmt.Fprint* call, which ADR-047 D1 reserves
// for pkg/render. Command results must be printed by pkg/render, not here.
var rawOutputCall = regexp.MustCompile(`fmt\.Fprint(f|ln)?\(`)

const rawOutputAllowPath = "testdata/raw-output-allow.txt"

// rawOutputAllowEntry is one '<filename> <count>  # reason' line.
type rawOutputAllowEntry struct {
	file  string
	count int
	line  int
}

// TestRawOutputGuard enforces ADR-047 D1: command results go through
// pkg/render, never through fmt.Fprint*. The allowlist records the raw calls
// that remain in prompts, diagnostics and the batches not migrated yet, so a
// new call cannot land unnoticed and a migrated command must lower its entry.
func TestRawOutputGuard(t *testing.T) {
	counted, files := countRawOutputCalls(t)
	allowed := readRawOutputAllowlist(t)

	for i := 1; i < len(allowed); i++ {
		if allowed[i].file <= allowed[i-1].file {
			t.Errorf("%s line %d: entries are not sorted by filename: %q comes after %q; keep the allowlist sorted and document each raw call against pkg/render (ADR-047 D1)", rawOutputAllowPath, allowed[i].line, allowed[i].file, allowed[i-1].file)
		}
	}

	allowedByFile := make(map[string]rawOutputAllowEntry, len(allowed))
	for _, entry := range allowed {
		allowedByFile[entry.file] = entry
	}

	for _, file := range files {
		got := counted[file]
		entry, listed := allowedByFile[file]
		switch {
		case !listed && got > 0:
			t.Errorf("%s has %d raw fmt.Fprint* calls but %s allows 0 (no entry); route command results through pkg/render (ADR-047 D1) or record the call count with a reason", file, got, rawOutputAllowPath)
		case listed && got > entry.count:
			t.Errorf("%s has %d raw fmt.Fprint* calls but %s allows %d; route the new result through pkg/render (ADR-047 D1)", file, got, rawOutputAllowPath, entry.count)
		case listed && got < entry.count:
			t.Errorf("%s is allowed %d raw fmt.Fprint* calls but now has %d; lower the stale entry in %s and keep results on pkg/render (ADR-047 D1)", file, entry.count, got, rawOutputAllowPath)
		}
		delete(allowedByFile, file)
	}

	for _, entry := range allowed {
		if _, ok := allowedByFile[entry.file]; !ok {
			continue
		}
		t.Errorf("%s line %d lists %q, but cmd/orbit has no non-test .go file of that name; remove the entry (results belong on pkg/render, ADR-047 D1)", rawOutputAllowPath, entry.line, entry.file)
	}
}

// countRawOutputCalls returns the number of fmt.Fprint* calls in every
// non-test .go file in the package directory, and the sorted file names.
func countRawOutputCalls(t *testing.T) (map[string]int, []string) {
	t.Helper()

	dirEntries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	counted := make(map[string]int)
	var files []string
	for _, dirEntry := range dirEntries {
		name := dirEntry.Name()
		if dirEntry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		files = append(files, name)
		counted[name] = len(rawOutputCall.FindAllIndex(src, -1))
	}
	if len(files) == 0 {
		t.Fatal("no non-test .go files found in cmd/orbit; the guard is not checking anything")
	}
	return counted, files
}

// readRawOutputAllowlist parses '<filename> <count>  # reason' lines. Blank
// lines and lines whose first non-space character is '#' are comments.
func readRawOutputAllowlist(t *testing.T) []rawOutputAllowEntry {
	t.Helper()

	data, err := os.ReadFile(rawOutputAllowPath)
	if err != nil {
		t.Fatalf("read %s: %v", rawOutputAllowPath, err)
	}

	var entries []rawOutputAllowEntry
	seen := make(map[string]bool)
	for i, line := range strings.Split(string(data), "\n") {
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s line %d: want '<filename> <count>', got %q", rawOutputAllowPath, i+1, line)
		}
		count, err := strconv.Atoi(fields[1])
		if err != nil || count < 0 {
			t.Fatalf("%s line %d: invalid count %q", rawOutputAllowPath, i+1, fields[1])
		}
		if filepath.Base(fields[0]) != fields[0] {
			t.Fatalf("%s line %d: %q must be a bare file name, not a path", rawOutputAllowPath, i+1, fields[0])
		}
		if seen[fields[0]] {
			t.Fatalf("%s line %d: duplicate entry for %q", rawOutputAllowPath, i+1, fields[0])
		}
		seen[fields[0]] = true
		entries = append(entries, rawOutputAllowEntry{file: fields[0], count: count, line: i + 1})
	}

	if len(entries) == 0 {
		t.Fatalf("%s has no entries; list every file that still calls fmt.Fprint*", rawOutputAllowPath)
	}
	return entries
}
