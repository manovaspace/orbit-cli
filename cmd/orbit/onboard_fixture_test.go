package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Full-flow fixtures isolate onboarding's file writes and system prerequisite
// commands. Individual diagnostic and healer packages verify those operations.
func setupOnboardPrerequisiteFixture(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"sudo":    "#!/bin/sh\ncase $1 in apt-get|chsh) exec \"$@\";; *) exit 97;; esac\n",
		"apt-get": "#!/bin/sh\nexit 0\n",
		"chsh":    "#!/bin/sh\nexit 0\n",
		"bun":     "#!/bin/sh\ncase $1 in --version|-v) printf '1.4.0\\n';; *) exit 97;; esac\n",
		"caddy":   "#!/bin/sh\ncase $1 in version) printf 'v2.11.4\\n';; trust) exit 0;; *) exit 97;; esac\n",
		"typst":   "#!/bin/sh\n[ \"$1\" = --version ] || exit 97\nprintf 'typst 0.15.1\\n'\n",
		"ssh-add": "#!/bin/sh\nexit 0\n",
		"curl":    "#!/bin/sh\ntouch \"$HOME/blocked-installer\"\nexit 97\n",
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ORBIT_SKIP_PREFLIGHT", "1")
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Cleanup(func() {
		if _, err := os.Stat(filepath.Join(home, "blocked-installer")); !os.IsNotExist(err) {
			t.Error("onboarding fixture attempted an external installer")
		}
	})
}
