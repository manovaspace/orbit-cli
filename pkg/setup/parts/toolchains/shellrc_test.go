package toolchains

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

const wantLinuxBlock = `# >>> orbit >>>
# managed by orbit setup; edits inside this block are replaced
export PATH="$HOME/.local/bin:$HOME/.bun/bin:$HOME/go/bin:$PATH"
export GOTOOLCHAIN=auto
# <<< orbit <<<
`

const wantDarwinBlock = `# >>> orbit >>>
# managed by orbit setup; edits inside this block are replaced
if [ -x /opt/homebrew/bin/brew ]; then eval "$(/opt/homebrew/bin/brew shellenv)"; elif [ -x /usr/local/bin/brew ]; then eval "$(/usr/local/bin/brew shellenv)"; fi
if [ -n "$HOMEBREW_PREFIX" ]; then export PATH="$HOMEBREW_PREFIX/opt/node@24/bin:$HOMEBREW_PREFIX/opt/postgresql@18/bin:$PATH"; fi
export PATH="$HOME/.local/bin:$HOME/.bun/bin:$HOME/go/bin:$PATH"
export GOTOOLCHAIN=auto
# <<< orbit <<<
`

func TestBlockDarwinHasBrewShellenv(t *testing.T) {
	if got := Block("darwin", "/Users/u"); got != wantDarwinBlock {
		t.Fatalf("darwin block =\n%s\nwant\n%s", got, wantDarwinBlock)
	}
	if got := Block("linux", "/home/u"); got != wantLinuxBlock {
		t.Fatalf("linux block =\n%s\nwant\n%s", got, wantLinuxBlock)
	}
	if strings.Contains(Block("linux", "/home/u"), "brew") {
		t.Fatal("linux block mentions brew")
	}
}

func TestApplyBlockReplacesNotDuplicates(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		want     string
		changed  bool
	}{
		{"empty file", "", wantLinuxBlock, true},
		{"one trailing newline", "export A=1\n", "export A=1\n\n" + wantLinuxBlock, true},
		{"no trailing newline", "export A=1", "export A=1\n\n" + wantLinuxBlock, true},
		{"old block replaced in place", "before\n# >>> orbit >>>\nold line\n# <<< orbit <<<\nafter\n", "before\n" + wantLinuxBlock + "after\n", true},
		{"current block unchanged", wantLinuxBlock, wantLinuxBlock, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := ApplyBlock(tc.existing, wantLinuxBlock)
			if got != tc.want || changed != tc.changed {
				t.Fatalf("ApplyBlock = (%q, %v), want (%q, %v)", got, changed, tc.want, tc.changed)
			}
			again, changed := ApplyBlock(got, wantLinuxBlock)
			if again != got || changed {
				t.Fatalf("second ApplyBlock = (%q, %v), want unchanged", again, changed)
			}
			if n := strings.Count(again, beginMarker); n != 1 {
				t.Fatalf("begin markers = %d, want 1", n)
			}
		})
	}
}

func TestApplyBlockUnterminatedIsError(t *testing.T) {
	existing := "export A=1\n# >>> orbit >>>\nexport B=2\n"
	got, changed := ApplyBlock(existing, wantLinuxBlock)
	if changed || got != existing {
		t.Fatalf("ApplyBlock = (%q, %v), want existing text unchanged", got, changed)
	}

	h := newHarness(t)
	if err := os.WriteFile(filepath.Join(h.home, ".zshrc"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	s := h.state("linux", "native", &runnertest.Fake{}, &runnertest.Fake{})
	plan, err := h.part().Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %v, want none for an unterminated block", summaries(plan))
	}
	if len(plan.Findings) != 1 || plan.Findings[0].Severity != engine.SeverityError {
		t.Fatalf("findings = %+v, want one error finding", plan.Findings)
	}
	want := "~/.zshrc has an unterminated orbit block; fix it by hand"
	if plan.Findings[0].Message != want {
		t.Fatalf("message = %q, want %q", plan.Findings[0].Message, want)
	}
	data, err := os.ReadFile(filepath.Join(h.home, ".zshrc"))
	if err != nil || string(data) != existing {
		t.Fatalf("zshrc changed by Plan: %q, %v", data, err)
	}
}

func TestLoginShellLookupAndFallback(t *testing.T) {
	t.Run("linux getent passwd", func(t *testing.T) {
		run := &runnertest.Fake{}
		run.On("getent passwd alice", runner.Result{Stdout: "alice:x:1000:1000:Alice:/home/alice:/bin/zsh\n"}, nil)
		if got := loginShellForUser(context.Background(), run, "linux", "alice"); got != "/bin/zsh" {
			t.Fatalf("loginShellForUser(linux) = %q, want /bin/zsh", got)
		}
	})

	t.Run("darwin dscl UserShell", func(t *testing.T) {
		run := &runnertest.Fake{}
		run.On("dscl . -read /Users/alice UserShell", runner.Result{Stdout: "UserShell: /bin/zsh\n"}, nil)
		if got := loginShellForUser(context.Background(), run, "darwin", "alice"); got != "/bin/zsh" {
			t.Fatalf("loginShellForUser(darwin) = %q, want /bin/zsh", got)
		}
	})

	t.Run("lookup failure returns empty for fallback", func(t *testing.T) {
		run := &runnertest.Fake{}
		if got := loginShellForUser(context.Background(), run, "linux", "alice"); got != "" {
			t.Fatalf("loginShellForUser on error = %q, want empty", got)
		}
	})

	t.Run("planShellRC uses loginShell over SHELL env and falls back when empty", func(t *testing.T) {
		h := newHarness(t)
		h.env["SHELL"] = "/bin/bash"
		h.loginShell = func(context.Context, runner.Runner, string) string { return "/bin/zsh" }
		plan, err := h.part().Plan(context.Background(), h.state("linux", "native", &runnertest.Fake{}, &runnertest.Fake{}))
		if err != nil {
			t.Fatalf("Plan: %v", err)
		}
		if got := summaries(plan); !slices.Equal(got, []string{"Update ~/.zshrc"}) {
			t.Fatalf("loginShell override actions = %q, want [Update ~/.zshrc]", got)
		}

		h.loginShell = func(context.Context, runner.Runner, string) string { return "" }
		plan, err = h.part().Plan(context.Background(), h.state("linux", "native", &runnertest.Fake{}, &runnertest.Fake{}))
		if err != nil {
			t.Fatalf("Plan fallback: %v", err)
		}
		if got := summaries(plan); !slices.Equal(got, []string{"Update ~/.bashrc", "Update ~/.profile"}) {
			t.Fatalf("fallback actions = %q, want bashrc and profile", got)
		}
	})
}
