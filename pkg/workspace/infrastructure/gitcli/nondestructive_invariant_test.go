package gitcli

import (
	"context"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

// Tier-1 invariant 4 (git non-destructive): every git command the workspace
// adapter can issue, used by orbit sync and orbit repair, must avoid
// subcommands and flags that overwrite, discard or delete working files.
func TestNonDestructiveInvariantGitArgv(t *testing.T) {
	forbiddenSubcommands := map[string]bool{
		"reset": true, "clean": true, "checkout": true, "stash": true,
		"rebase": true, "restore": true, "switch": true, "branch": true,
	}
	forbiddenFlags := map[string]bool{
		"-f": true, "--force": true, "--hard": true, "-D": true,
		"-fd": true, "-df": true, "-fdx": true, "-dfx": true,
	}

	cases := []struct {
		name string
		call func(g *Git) error
	}{
		{"IsWorkTree", func(g *Git) error { _, err := g.IsWorkTree(context.Background(), "/ws/repo"); return err }},
		{"Branch", func(g *Git) error { _, err := g.Branch(context.Background(), "/ws/repo"); return err }},
		{"Status", func(g *Git) error { _, err := g.Status(context.Background(), "/ws/repo"); return err }},
		{"AheadBehind", func(g *Git) error { _, _, err := g.AheadBehind(context.Background(), "/ws/repo"); return err }},
		{"Fetch", func(g *Git) error { return g.Fetch(context.Background(), "/ws/repo") }},
		{"MergeFFOnly", func(g *Git) error {
			_, err := g.MergeFFOnly(context.Background(), "/ws/repo", "origin/main")
			return err
		}},
		{"CloneInWorkspace", func(g *Git) error {
			_, err := g.Clone(context.Background(), "/ws", "https://example.test/o/r.git", "/ws/r")
			return err
		}},
		{"CloneFromCwd", func(g *Git) error {
			_, err := g.Clone(context.Background(), "", "https://example.test/o/r.git", "/ws/r2")
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &runnertest.Fake{}
			fake.On("git ", runner.Result{Stdout: "true\n"}, nil)
			g := New(fake)

			_ = tc.call(g)

			calls := fake.Calls()
			if len(calls) == 0 {
				t.Fatalf("%s issued no git command", tc.name)
			}
			for _, c := range calls {
				if c.Name != "git" {
					t.Errorf("%s ran %q, want git", tc.name, c.Name)
				}
				for _, a := range c.Args {
					if forbiddenFlags[a] {
						t.Errorf("%s argv %v contains destructive flag %q", tc.name, c.Args, a)
					}
					if forbiddenSubcommands[a] {
						t.Errorf("%s argv %v contains destructive subcommand %q", tc.name, c.Args, a)
					}
				}
			}
		})
	}
}
