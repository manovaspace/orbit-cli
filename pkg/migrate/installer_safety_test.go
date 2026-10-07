package migrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSymlinkPreservesConflictingOriginals(t *testing.T) {
	for _, kind := range []string{"file", "dir", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			src, dest := filepath.Join(root, "source"), filepath.Join(root, "dest")
			if err := os.WriteFile(src, []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
			original := dest
			switch kind {
			case "dir":
				if err := os.Mkdir(dest, 0700); err != nil {
					t.Fatal(err)
				}
				original = filepath.Join(dest, "content")
			case "symlink":
				original = filepath.Join(root, "old")
				if err := os.Symlink(original, dest); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(original, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := createSymlink(src, dest); err == nil {
				t.Error("conflict accepted")
			}
			data, err := os.ReadFile(original)
			if err != nil || string(data) != "preserve" {
				t.Errorf("original lost: %q %v", data, err)
			}
			if kind == "symlink" {
				if target, _ := os.Readlink(dest); target != original {
					t.Error("conflicting symlink replaced")
				}
			}
		})
	}
}

func TestHooksDoNotConfigureAncestorRepo(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := exec.Command("git", "-C", root, "init", "-q").Run(); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "not-a-repo")
	if err := os.MkdirAll(filepath.Join(child, ".githooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := InstallGitHooks(child); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("git", "-C", root, "config", "--local", "--get", "core.hooksPath").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("ancestor config changed: %s", out)
	}
}

func TestSelectedRepositoryHooksOnly(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[core]\n hooksPath = /synthetic/global-hooks\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if err := os.Mkdir(filepath.Join(root, ".githooks"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"selected", "other"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := exec.Command("git", "-C", path, "init", "-q").Run(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := InstallGitHooks(root, "selected"); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("git", "-C", filepath.Join(root, "selected"), "config", "--local", "--get", "core.hooksPath").Output()
	if err != nil || strings.TrimSpace(string(out)) != filepath.Join(root, ".githooks") {
		t.Fatalf("selected hooks absent: %s %v", out, err)
	}
	out, _ = exec.Command("git", "-C", filepath.Join(root, "other"), "config", "--local", "--get", "core.hooksPath").Output()
	if len(out) > 0 {
		t.Fatal("unselected repo changed")
	}
	after, _ := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if string(after) != string(before) {
		t.Fatal("global config changed")
	}
}

func TestWorkspaceInstallRejectsParentAliases(t *testing.T) {
	for _, operation := range []string{"env", "rules", "skills", "link", "workspace-dir"} {
		t.Run(operation, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			alias := filepath.Join(root, ".cursor")
			var install func() error
			switch operation {
			case "env":
				install = func() error { return SetupMCPEnvironment(root) }
			case "rules", "skills":
				source := filepath.Join(root, "handbook", "cursor", operation)
				if err := os.MkdirAll(source, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(source, "fixture"), []byte("rule"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(alias, 0755); err != nil {
					t.Fatal(err)
				}
				alias = filepath.Join(alias, operation)
				install = func() error { return SymlinkCursorRules(root) }
			case "link":
				install = func() error {
					return createSymlink(filepath.Join(root, "source"), filepath.Join(alias, "nested", "link"))
				}
			case "workspace-dir":
				alias = filepath.Join(root, "orbit")
				install = func() error { return EnsureWorkspaceDirs(root) }
			}
			if err := os.Symlink(outside, alias); err != nil {
				t.Fatal(err)
			}
			if err := install(); err == nil {
				t.Error("aliased destination accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("outside modified: %v %v", entries, err)
			}
			if target, err := os.Readlink(alias); err != nil || target != outside {
				t.Fatal("alias changed")
			}
		})
	}
}

func TestMCPEnvironmentRejectsDanglingLink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "uncreated-secret")
	if err := os.Mkdir(filepath.Join(root, ".cursor"), 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, ".cursor", "mcp.env")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if err := SetupMCPEnvironment(root); err == nil {
		t.Error("dangling environment link accepted")
	}
	if _, err := os.Lstat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside secret created: %v", err)
	}
	if got, _ := os.Readlink(target); got != outside {
		t.Fatal("existing link changed")
	}
}
