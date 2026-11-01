package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

func TestInstallGitHooksCommandLines(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".githooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	realRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(root, ".githooks")

	fake := &runnertest.Fake{}
	fake.On("git -C "+repo+" rev-parse --show-toplevel", runner.Result{Stdout: realRoot + "\n"}, nil)
	fake.On("git -C "+repo+" config --local --get core.hooksPath", runner.Result{}, nil)
	fake.On("git config --global --show-origin --get core.hooksPath", runner.Result{}, nil)
	fake.On("git -C "+repo+" config --local core.hooksPath "+hooks, runner.Result{}, nil)

	if err := New(fake).InstallGitHooks(root, "repo"); err != nil {
		t.Fatalf("InstallGitHooks: %v", err)
	}

	calls := fake.Calls()
	want := [][]string{
		{"-C", repo, "rev-parse", "--show-toplevel"},
		{"-C", repo, "config", "--local", "--get", "core.hooksPath"},
		{"config", "--global", "--show-origin", "--get", "core.hooksPath"},
		{"-C", repo, "config", "--local", "core.hooksPath", hooks},
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i].Name != "git" {
			t.Errorf("call %d command = %q, want git", i, calls[i].Name)
		}
		if !reflect.DeepEqual(calls[i].Args, want[i]) {
			t.Errorf("call %d args = %v, want %v", i, calls[i].Args, want[i])
		}
	}
}

func TestSymlinkPreservesConflictingOriginals(t *testing.T) {
	for _, kind := range []string{"file", "dir", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			src, dest := filepath.Join(root, "source"), filepath.Join(root, "dest")
			if err := os.WriteFile(src, []byte("source"), 0o600); err != nil {
				t.Fatal(err)
			}
			original := dest
			switch kind {
			case "dir":
				if err := os.Mkdir(dest, 0o700); err != nil {
					t.Fatal(err)
				}
				original = filepath.Join(dest, "content")
			case "symlink":
				original = filepath.Join(root, "old")
				if err := os.Symlink(original, dest); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(original, []byte("preserve"), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Join(child, ".githooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := New(runner.Exec{}).InstallGitHooks(child); err != nil {
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
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[core]\n hooksPath = /synthetic/global-hooks\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if err := os.Mkdir(filepath.Join(root, ".githooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"selected", "other"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := exec.Command("git", "-C", path, "init", "-q").Run(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := New(runner.Exec{}).InstallGitHooks(root, "selected"); err != nil {
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
			b := New(runner.Exec{})
			var install func() error
			switch operation {
			case "env":
				install = func() error { return b.SetupMCPEnvironment(root) }
			case "rules", "skills":
				source := filepath.Join(root, "handbook", "cursor", operation)
				if err := os.MkdirAll(source, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(source, "fixture"), []byte("rule"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(alias, 0o755); err != nil {
					t.Fatal(err)
				}
				alias = filepath.Join(alias, operation)
				install = func() error { return b.SymlinkCursorRules(root) }
			case "link":
				install = func() error {
					return createSymlink(filepath.Join(root, "source"), filepath.Join(alias, "nested", "link"))
				}
			case "workspace-dir":
				alias = filepath.Join(root, "orbit")
				install = func() error { return b.EnsureWorkspaceDirs(root) }
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
	if err := os.Mkdir(filepath.Join(root, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, ".cursor", "mcp.env")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if err := New(runner.Exec{}).SetupMCPEnvironment(root); err == nil {
		t.Error("dangling environment link accepted")
	}
	if _, err := os.Lstat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside secret created: %v", err)
	}
	if got, _ := os.Readlink(target); got != outside {
		t.Fatal("existing link changed")
	}
}

func TestEnsureWorkspaceDirs(t *testing.T) {
	root := t.TempDir()
	if err := New(runner.Exec{}).EnsureWorkspaceDirs(root); err != nil {
		t.Fatalf("EnsureWorkspaceDirs failed: %v", err)
	}
	for _, dir := range []string{"orbit", "manovaspace", "clients", "documents", "share", "temp"} {
		path := filepath.Join(root, dir)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Errorf("expected directory %s, err: %v", path, err)
		}
	}
}

func TestSetupMCPEnvironment(t *testing.T) {
	b := New(runner.Exec{})

	t.Run("PreservesExisting", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".cursor"), 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, ".cursor", "mcp.env")
		if err := os.WriteFile(path, []byte("EXISTING_TOKEN=123\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := b.SetupMCPEnvironment(root); err != nil {
			t.Fatal(err)
		}
		content, _ := os.ReadFile(path)
		if string(content) != "EXISTING_TOKEN=123\n" {
			t.Errorf("existing mcp.env overwritten: %s", content)
		}
	})

	t.Run("CopiesExample", func(t *testing.T) {
		root := t.TempDir()
		example := filepath.Join(root, "handbook", "cursor")
		if err := os.MkdirAll(example, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(example, "mcp.env.example"), []byte("TOKEN=example\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := b.SetupMCPEnvironment(root); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, ".cursor", "mcp.env")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "TOKEN=example\n" {
			t.Errorf("content = %q", content)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("permissions = %#o", info.Mode().Perm())
		}
	})

	t.Run("DefaultTemplate", func(t *testing.T) {
		root := t.TempDir()
		if err := b.SetupMCPEnvironment(root); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filepath.Join(root, ".cursor", "mcp.env"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "Cursor MCP") {
			t.Errorf("content = %q", content)
		}
	})
}

func TestSymlinkCursorRules(t *testing.T) {
	b := New(runner.Exec{})

	t.Run("NoHandbook", func(t *testing.T) {
		if err := b.SymlinkCursorRules(t.TempDir()); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("LinksRulesSkillsAndWorkspaceFiles", func(t *testing.T) {
		root := t.TempDir()
		rules := filepath.Join(root, "handbook", "cursor", "rules")
		skills := filepath.Join(root, "handbook", "cursor", "skills", "test-skill")
		if err := os.MkdirAll(rules, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(skills, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rules, "sample.mdc"), []byte("# Sample Rule"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "handbook", "cursor", ".cursorignore"), []byte("node_modules\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "handbook", "cursor", "AGENTS.workspace.md"), []byte("# Agents"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := b.SymlinkCursorRules(root); err != nil {
			t.Fatal(err)
		}
		if target, err := os.Readlink(filepath.Join(root, ".cursor", "rules", "sample.mdc")); err != nil || target != filepath.Join(rules, "sample.mdc") {
			t.Errorf("rule symlink = %q, %v", target, err)
		}
		if target, err := os.Readlink(filepath.Join(root, ".cursor", "skills", "test-skill")); err != nil || target != skills {
			t.Errorf("skill symlink = %q, %v", target, err)
		}
		if target, err := os.Readlink(filepath.Join(root, "AGENTS.md")); err != nil || target != filepath.Join(root, "handbook", "cursor", "AGENTS.workspace.md") {
			t.Errorf("agents symlink = %q, %v", target, err)
		}

		if err := b.SymlinkCursorRules(root); err != nil {
			t.Errorf("re-run failed: %v", err)
		}

		conflict := filepath.Join(root, ".cursor", "rules", "sample.mdc")
		_ = os.Remove(conflict)
		if err := os.WriteFile(conflict, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := b.SymlinkCursorRules(root); err == nil {
			t.Fatal("expected replacement conflict")
		}
		if data, _ := os.ReadFile(conflict); string(data) != "old" {
			t.Fatal("conflicting file changed")
		}
	})
}

func TestSetupWorkspace(t *testing.T) {
	root := t.TempDir()
	rules := filepath.Join(root, "handbook", "cursor", "rules")
	if err := os.MkdirAll(rules, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rules, "sample.mdc"), []byte("# Sample Rule"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := New(runner.Exec{}).SetupWorkspace(root); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}
	for _, dir := range []string{"orbit", "manovaspace", "clients", "documents", "share", "temp"} {
		if info, err := os.Stat(filepath.Join(root, dir)); err != nil || !info.IsDir() {
			t.Errorf("expected directory %s, err: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".cursor", "mcp.env")); err != nil {
		t.Errorf("expected .cursor/mcp.env: %v", err)
	}
	if _, err := os.Readlink(filepath.Join(root, ".cursor", "rules", "sample.mdc")); err != nil {
		t.Errorf("expected rule symlink: %v", err)
	}
}
