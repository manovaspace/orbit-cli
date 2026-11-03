// Package rendertest compares rendered output against golden files next to the
// calling test. Run go test with -update-render to rewrite them.
package rendertest

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// updateRender uses its own flag name because cmd/orbit registers -update for
// its help goldens, and two packages cannot share one flag in the same binary.
var updateRender = flag.Bool("update-render", false, "rewrite render golden files")

// ansi matches CSI escape sequences emitted by lipgloss.
var ansi = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// Golden compares got with testdata/<name>.golden, stripping ANSI sequences
// first. With -update it writes the file instead.
func Golden(t testing.TB, name string, got string) {
	t.Helper()

	got = ansi.ReplaceAllString(got, "")
	path := filepath.Join("testdata", name+".golden")

	if *updateRender {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go test ./pkg/render ./cmd/orbit -update-render to create it)", path, err)
	}
	if got != string(want) {
		t.Errorf("%s golden changed; run 'go test ./pkg/render ./cmd/orbit -update-render' to refresh\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
