package main

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// TestFindWorkspaceRootIgnoresHomeOrbitCache pins the rule that ~/.orbit (the
// per-user CLI cache) is not a workspace marker, while workspace.yaml still is.
// Each case gets its own fake home so markers from one case cannot leak into another.
// The fake home sits under the real account home on most machines, so the walk
// also climbs past it into the real home, whose ~/.orbit must be ignored too.
func TestFindWorkspaceRootIgnoresHomeOrbitCache(t *testing.T) {
	t.Run("orbit marker below home is the workspace", func(t *testing.T) {
		home := newFakeHome(t)
		workspace := filepath.Join(home, "some", "workspace")
		mustMkdirAll(t, filepath.Join(workspace, ".orbit"))
		sub := filepath.Join(workspace, "pkg")
		mustMkdirAll(t, sub)

		if got := findWorkspaceRoot(sub); got != workspace {
			t.Fatalf("findWorkspaceRoot(%q) = %q, want %q", sub, got, workspace)
		}
	})

	t.Run("plain dir under home does not resolve to home", func(t *testing.T) {
		home := newFakeHome(t)
		dir := filepath.Join(home, "plain", "dir")
		mustMkdirAll(t, dir)

		got := findWorkspaceRoot(dir)
		assertNotHomeRoot(t, got, home)
		if want, _ := filepath.Abs(dir); got != want {
			t.Fatalf("findWorkspaceRoot(%q) = %q, want fallback %q", dir, got, want)
		}
	})

	t.Run("workspace.yaml below home is the workspace", func(t *testing.T) {
		home := newFakeHome(t)
		workspace := filepath.Join(home, "ws")
		mustMkdirAll(t, workspace)
		mustWriteFile(t, filepath.Join(workspace, "workspace.yaml"), "name: test\n")
		sub := filepath.Join(workspace, "sub")
		mustMkdirAll(t, sub)

		if got := findWorkspaceRoot(sub); got != workspace {
			t.Fatalf("findWorkspaceRoot(%q) = %q, want %q", sub, got, workspace)
		}
	})

	t.Run("workspace.yaml in home is still a marker", func(t *testing.T) {
		home := newFakeHome(t)
		mustWriteFile(t, filepath.Join(home, "workspace.yaml"), "name: test\n")
		dir := filepath.Join(home, "plain", "dir")
		mustMkdirAll(t, dir)

		if got := findWorkspaceRoot(dir); got != home {
			t.Fatalf("findWorkspaceRoot(%q) = %q, want home %q", dir, got, home)
		}
	})

	t.Run("symlink to home is treated as home", func(t *testing.T) {
		home := newFakeHome(t)
		link := filepath.Join(t.TempDir(), "homelink")
		if err := os.Symlink(home, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		dir := filepath.Join(link, "plain", "dir")

		got := findWorkspaceRoot(dir)
		assertNotHomeRoot(t, got, home)
		if got == link {
			t.Fatalf("findWorkspaceRoot(%q) = %q; the home symlink must not mark the workspace", dir, got)
		}
		if want, _ := filepath.Abs(dir); got != want {
			t.Fatalf("findWorkspaceRoot(%q) = %q, want fallback %q", dir, got, want)
		}
	})
}

// TestIsWorkspaceMarkerDirHomes checks the helper directly, independent of the
// machine's real account home: .orbit counts only outside the listed homes,
// and workspace.yaml counts everywhere.
func TestIsWorkspaceMarkerDirHomes(t *testing.T) {
	dir := t.TempDir()
	mustMkdirAll(t, filepath.Join(dir, ".orbit"))

	if !isWorkspaceMarkerDir(dir, nil) {
		t.Fatalf("isWorkspaceMarkerDir(%q, nil) = false; .orbit must count outside any home", dir)
	}
	if isWorkspaceMarkerDir(dir, []string{dir}) {
		t.Fatalf("isWorkspaceMarkerDir(%q, [%q]) = true; .orbit must not count in a listed home", dir, dir)
	}
	if isWorkspaceMarkerDir(dir, []string{t.TempDir(), dir}) {
		t.Fatalf("isWorkspaceMarkerDir(%q, ...) = true; .orbit must not count when any listed home matches", dir)
	}

	mustWriteFile(t, filepath.Join(dir, "workspace.yaml"), "name: test\n")
	if !isWorkspaceMarkerDir(dir, []string{dir}) {
		t.Fatalf("isWorkspaceMarkerDir(%q, [%q]) = false; workspace.yaml must count in a home", dir, dir)
	}
}

// TestHomeDirsOverriddenHomeAndAccountHome checks that homeDirs lists the
// overridden $HOME and the real account home, without empty or duplicate entries.
func TestHomeDirsOverriddenHomeAndAccountHome(t *testing.T) {
	fake := newFakeHome(t)
	account := accountHome()

	homes := homeDirs()
	assertHomesWellFormed(t, homes)
	if !containsDir(homes, fake) {
		t.Fatalf("homeDirs() = %q; want overridden HOME %q", homes, fake)
	}
	if account != "" && !containsDir(homes, account) {
		t.Fatalf("homeDirs() = %q; want account home %q", homes, account)
	}

	t.Setenv("HOME", "")
	homes = homeDirs()
	assertHomesWellFormed(t, homes)
	if account != "" && !containsDir(homes, account) {
		t.Fatalf("homeDirs() with empty HOME = %q; want account home %q", homes, account)
	}
}

// newFakeHome points HOME at a fresh temp dir that holds only the per-user
// CLI cache ~/.orbit/update-check.json, and returns that dir.
func newFakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustMkdirAll(t, filepath.Join(home, ".orbit"))
	mustWriteFile(t, filepath.Join(home, ".orbit", "update-check.json"), "{}")
	return home
}

// accountHome returns the real account home from os/user (it ignores HOME),
// or "" when the lookup fails.
func accountHome() string {
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}

// assertNotHomeRoot fails when got is the fake home or the real account home.
func assertNotHomeRoot(t *testing.T, got, fakeHome string) {
	t.Helper()
	if got == fakeHome {
		t.Fatalf("findWorkspaceRoot resolved to fake home %q; ~/.orbit must not mark the workspace", got)
	}
	if account := accountHome(); account != "" && got == account {
		t.Fatalf("findWorkspaceRoot resolved to real account home %q; its ~/.orbit must not mark the workspace", got)
	}
}

func assertHomesWellFormed(t *testing.T, homes []string) {
	t.Helper()
	for i, h := range homes {
		if h == "" {
			t.Fatalf("homeDirs() contains an empty entry: %q", homes)
		}
		for _, other := range homes[i+1:] {
			if isSameDir(h, other) {
				t.Fatalf("homeDirs() contains duplicate %q and %q", h, other)
			}
		}
	}
}

func containsDir(dirs []string, want string) bool {
	for _, d := range dirs {
		if isSameDir(d, want) {
			return true
		}
	}
	return false
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
