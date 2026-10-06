package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNestedPackagesInheritRepositoryRuntimeRequirements(t *testing.T) {
	for _, tc := range []struct {
		name, nested, nodeVersion string
	}{
		{"no package requirements", `{"name":"nested"}`, "v23.0.0"},
		{"nested node constraint retained", `{"engines":{"node":">=26"}}`, "v24.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, bin := t.TempDir(), t.TempDir()
			repo := filepath.Join(root, "repo")
			nested := filepath.Join(repo, "packages", "nested")
			if err := os.MkdirAll(nested, 0755); err != nil {
				t.Fatal(err)
			}
			for name, contents := range map[string]string{
				filepath.Join(root, "package.json"):   `{"packageManager":"bun@9.0.0","engines":{"node":">=99"}}`,
				filepath.Join(repo, ".git"):           "gitdir: fixture\n",
				filepath.Join(repo, "package.json"):   `{"packageManager":"bun@1.4.0","engines":{"node":">=24"}}`,
				filepath.Join(nested, "package.json"): tc.nested,
			} {
				if err := os.WriteFile(name, []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for tool, version := range map[string]string{"go": "go1.26.7", "node": tc.nodeVersion, "bun": "1.3.14"} {
				if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\necho '"+version+"'\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(nested)
			t.Setenv("PATH", bin)
			seen := map[string]bool{}
			for _, result := range checkProjectToolchains() {
				if result.Name == "Bun" || result.Name == "Node.js" {
					seen[result.Name] = true
					if result.Status != StatusError {
						t.Errorf("nested project requirement ignored: %+v", result)
					}
					if strings.Contains(result.Message, "99") || strings.Contains(result.Message, "9.0.0") {
						t.Errorf("requirement crossed repository boundary: %+v", result)
					}
				}
			}
			if !seen["Bun"] || !seen["Node.js"] {
				t.Fatalf("runtime results missing: %v", seen)
			}
		})
	}
}
