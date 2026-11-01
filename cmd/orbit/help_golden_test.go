package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var update = flag.Bool("update", false, "rewrite help golden files")

// helpPaths returns the command path of every command in the tree, root first.
// Cobra's auto-generated help command is not part of the CLI surface.
func helpPaths(t *testing.T) [][]string {
	t.Helper()
	root := newRootCmd()

	var paths [][]string
	var walk func(cmd *cobra.Command, path []string)
	walk = func(cmd *cobra.Command, path []string) {
		paths = append(paths, path)
		for _, child := range cmd.Commands() {
			if child.Name() == "help" {
				continue
			}
			childPath := make([]string, len(path), len(path)+1)
			copy(childPath, path)
			walk(child, append(childPath, child.Name()))
		}
	}
	walk(root, nil)
	return paths
}

func helpGoldenName(path []string) string {
	if len(path) == 0 {
		return "root"
	}
	return strings.Join(path, "_")
}

func TestHelpGolden(t *testing.T) {
	t.Setenv("COLUMNS", "100")
	t.Setenv("NO_COLOR", "1")
	// Flag defaults that expand the user's home (for example --source on
	// `orbit ci secrets sync`) must not leak the developer's path into goldens.
	t.Setenv("HOME", "/home/orbit-test")

	goldenDir := filepath.Join("testdata", "help")
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
	}

	for _, path := range helpPaths(t) {
		name := helpGoldenName(path)
		t.Run(name, func(t *testing.T) {
			cmd := newRootCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(append(append([]string{}, path...), "--help"))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute %v --help: %v", path, err)
			}
			got := buf.String()

			goldenPath := filepath.Join(goldenDir, name+".golden")
			if *update {
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("write %s: %v", goldenPath, err)
				}
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read %s: %v (run go test ./cmd/orbit -run TestHelpGolden -update to create it)", goldenPath, err)
			}
			if got != string(want) {
				t.Errorf("%s help changed; run 'go test ./cmd/orbit -run TestHelpGolden -update' to refresh\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
			}
		})
	}
}
