package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsDefaultDeniesNetworkAndWrites(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("PATH", bin)
	t.Setenv("SSH_AUTH_SOCK", "")
	for _, tool := range []string{"git", "go", "node", "bun", "docker", "caddy", "typst", "ssh"} {
		script := "#!/bin/sh\necho '" + tool + " ' \"$@\" >> '" + logPath + "'\n"
		if tool == "ssh" {
			script += "echo changed > \"$HOME/known_hosts\"\nexit 99\n"
		} else {
			script += "echo 'v24.0.0 go1.26.0'\n"
		}
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	report := RunDiagnostics()
	calls, _ := os.ReadFile(logPath)
	for _, forbidden := range []string{"ssh ", "docker info"} {
		if strings.Contains(string(calls), forbidden) {
			t.Errorf("local diagnostics called %q: %s", forbidden, calls)
		}
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Errorf("HOME mutated: %v", entries)
	}
	found := false
	for _, r := range report.Results {
		if r.Category == "Network" && r.Status != StatusOK && strings.Contains(r.Message, "unverified") {
			found = true
		}
	}
	if !found {
		t.Error("missing explicit unverified remote result")
	}
}

func TestLocalDiagnosticsUsesProjectBunPin(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", bin)
	t.Setenv("SSH_AUTH_SOCK", "")
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"packageManager":"bun@1.3.14","engines":{"node":">=20"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for tool, version := range map[string]string{"git": "2.50.0", "go": "go1.26.7", "node": "20.10.0", "bun": "1.3.14"} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\necho '"+version+"'\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	report := RunDiagnostics()
	for _, result := range report.Results {
		if result.Name == "Bun" || result.Name == "Node.js" {
			if result.Status != StatusOK {
				t.Errorf("project pinned runtime rejected: %+v", result)
			}
		}
	}
}

func TestConflictingBunManifestRequirements(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	t.Chdir(root)
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"packageManager":"bun@1.3.14","engines":{"bun":">=1.4.0"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for tool, version := range map[string]string{"git": "2.50.0", "go": "go1.26.7", "node": "24.0.0", "bun": "1.3.14"} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\necho '"+version+"'\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	found := false
	for _, result := range RunDiagnostics().Results {
		if result.Name == "Bun manifest conflict" && result.Status == StatusError {
			found = true
		}
	}
	if !found {
		t.Fatal("conflicting manifests not reported")
	}
}

func TestRuntimeManifestDoesNotLeakAcrossRepoBoundary(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"packageManager":"bun@1.4.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module fixture\ngo 1.26.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	for tool, version := range map[string]string{"git": "2.50.0", "go": "go1.26.7", "node": "24.0.0", "bun": "1.3.14"} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\necho '"+version+"'\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, result := range RunDiagnostics().Results {
		if result.Name == "Bun" && result.Status != StatusOK {
			t.Fatalf("workspace package pin leaked into independent Go repo: %+v", result)
		}
	}
}
