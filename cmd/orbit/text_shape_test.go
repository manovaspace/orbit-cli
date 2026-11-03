package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// batch1Summary matches one shared summary line: an icon, a count and a label.
var batch1Summary = regexp.MustCompile(`^[✔⚠✖ℹ○?] [0-9]+ .+`)

func lineAt(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}

// TestBatch1TextShape enforces the shared text shape on every batch 1 golden.
func TestBatch1TextShape(t *testing.T) {
	for _, golden := range batch1TextGoldens {
		t.Run(golden.name, func(t *testing.T) {
			path := filepath.Join("testdata", golden.name+".golden")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v (run go test ./cmd/orbit -update-render)", path, err)
			}
			text := string(data)
			if strings.Contains(text, "\x1b") {
				t.Fatalf("%s contains ANSI escapes", path)
			}

			lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
			if len(lines) < 3 {
				t.Fatalf("%s is too short: %q", path, text)
			}
			if lines[0] != golden.title {
				t.Errorf("first line = %q, want the title %q", lines[0], golden.title)
			}
			if lines[1] != "" {
				t.Errorf("line 2 = %q, want an empty line after the title", lines[1])
			}
			if golden.kvs > 0 {
				separator := 2 + golden.kvs
				if separator >= len(lines) || lines[separator] != "" {
					t.Errorf("line %d = %q, want one empty line after the KV block", separator+1, lineAt(lines, separator))
				}
				if separator+1 >= len(lines) || lines[separator+1] == "" {
					t.Errorf("line %d is empty, want the body immediately after the KV separator", separator+2)
				}
			}

			for i, line := range lines {
				if strings.TrimRight(line, " \t") != line {
					t.Errorf("line %d has trailing whitespace: %q", i+1, line)
				}
				if strings.Contains(line, "\x1b") {
					t.Errorf("line %d contains ANSI escapes", i+1)
				}
			}

			var summaries []int
			for i, line := range lines {
				if batch1Summary.MatchString(line) {
					summaries = append(summaries, i)
				}
			}
			if len(summaries) != 1 {
				t.Fatalf("found %d summary lines, want exactly 1: %v\n%s", len(summaries), summaries, text)
			}
			if summaries[0] != len(lines)-1 {
				t.Errorf("summary line is line %d, want the last line %d", summaries[0]+1, len(lines))
			}
		})
	}
}
