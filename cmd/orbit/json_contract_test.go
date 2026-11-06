package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	updaterdomain "github.com/manovaspace/orbit-cli/pkg/updater/domain"
	"github.com/spf13/cobra"
)

// contractSkip documents leaves the --json contract test does not execute.
// Every entry is either interactive, mutates the host, or emits something other
// than a command result; none of them can be run safely or deterministically in
// a unit test.
var contractSkip = map[string]string{
	"orbit onboard":             "interactive onboarding wizard",
	"orbit admin init":          "interactive owner prompt and vault mutation",
	"orbit admin verify":        "interactive verification prompt and vault mutation",
	"orbit admin rotate-secret": "confirmation prompt and master-secret rotation",
	"orbit dev up":              "starts the container stack",
	"orbit dev down":            "stops the container stack",
	"orbit dev tier2":           "starts tier-2 containers",
	"orbit dev caddy":           "reloads the host reverse proxy",
	"orbit dev portal":          "starts the portal process",
	"orbit dev logs":            "streams container logs",
	"orbit self-update":         "replaces the running binary",
	"orbit uninstall":           "removes the running binary",
	"orbit completion zsh":      "emits a shell completion script, not a result envelope",
}

// contractUpdater keeps update and self-update off the network. SelfUpdate is
// never reached from the contract test because self-update is skipped.
type contractUpdater struct{}

func (contractUpdater) Check(_ context.Context, currentVersion, _, _ string) (*updaterdomain.UpdateCheckResult, error) {
	return &updaterdomain.UpdateCheckResult{CurrentVersion: currentVersion, LatestVersion: currentVersion}, nil
}

func (contractUpdater) CheckCached(ctx context.Context, currentVersion, _ string, _ time.Duration, apiURL, repoSlug string) (*updaterdomain.UpdateCheckResult, error) {
	return contractUpdater{}.Check(ctx, currentVersion, apiURL, repoSlug)
}

func (contractUpdater) SelfUpdate(context.Context, string, string) error {
	return fmt.Errorf("self-update is disabled in the JSON contract test")
}

// contractEnvelope is the minimum shape every --json run must produce.
type contractEnvelope struct {
	SchemaVersion *int  `json:"schemaVersion"`
	OK            *bool `json:"ok"`
	Error         *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// TestJSONContract runs every non-interactive leaf with --json and no
// arguments. The invocation is deliberately the no-argument one: commands with
// required arguments exercise the usage-error envelope, and commands without
// them exercise the success envelope. In both cases stdout must hold exactly
// one versioned envelope (ADR-047 D5/D6/D9).
func TestJSONContract(t *testing.T) {
	t.Setenv("ORBIT_SKIP_HOSTGATE", "1")

	// Keep forge, git, release and updater calls local and deterministic.
	previousUpdater, previousPR, previousRelease := releaseUpdater, prRunner, releaseRunner
	releaseUpdater = contractUpdater{}
	prRunner = &runnertest.Fake{}
	releaseRunner = &runnertest.Fake{}
	t.Cleanup(func() {
		releaseUpdater, prRunner, releaseRunner = previousUpdater, previousPR, previousRelease
	})

	seen := make(map[string]bool, len(contractSkip))
	for _, leaf := range contractLeaves(t, newRootCmdForTest(t)) {
		path := strings.Join(leaf.Path, " ")
		if reason, skip := contractSkip[path]; skip {
			seen[path] = true
			t.Run(slug(path), func(t *testing.T) {
				t.Skip(reason)
			})
			continue
		}

		if !leaf.HasJSON {
			t.Errorf("%s has no --json flag (ADR-047 D6)", path)
			continue
		}

		t.Run(slug(path), func(t *testing.T) {
			home := t.TempDir()
			workspace := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", home+"/.config")
			t.Setenv("ORBIT_WORKSPACE", workspace)
			t.Setenv("ORBIT_OWNER_STORE", home+"/.config/orbit/owner.json")
			t.Chdir(workspace)

			var stdout, stderr bytes.Buffer
			args := append(append([]string{}, leaf.Path...), "--json")
			code := run(args, &stdout, &stderr)

			env := decodeContractEnvelope(t, path, stdout.String())
			if env.SchemaVersion == nil || *env.SchemaVersion != output.SchemaVersion {
				t.Fatalf("%s schemaVersion = %v, want %d\nstdout: %s", path, env.SchemaVersion, output.SchemaVersion, stdout.String())
			}
			if env.OK == nil {
				t.Fatalf("%s envelope has no ok field\nstdout: %s", path, stdout.String())
			}

			if code == clierr.ExitOK {
				if !*env.OK {
					t.Fatalf("%s exited 0 but ok=false\nstdout: %s", path, stdout.String())
				}
				if env.Error != nil {
					t.Fatalf("%s exited 0 with an error body: %+v", path, env.Error)
				}
			} else {
				if *env.OK {
					t.Fatalf("%s exited %d but ok=true\nstdout: %s", path, code, stdout.String())
				}
				if env.Error == nil || env.Error.Code == "" {
					t.Fatalf("%s exited %d without an error body\nstdout: %s", path, code, stdout.String())
				}
			}

			// A flag error is a usage error and must still be exactly one
			// envelope; this path never runs the command body.
			var flagOut, flagErr bytes.Buffer
			flagArgs := append(append([]string{}, leaf.Path...), "--json", "--contract-unknown-flag")
			flagCode := run(flagArgs, &flagOut, &flagErr)
			if flagCode != clierr.ExitUsage {
				t.Fatalf("%s with an unknown flag exited %d, want %d\nstdout: %s", path, flagCode, clierr.ExitUsage, flagOut.String())
			}
			flagEnv := decodeContractEnvelope(t, path+" --contract-unknown-flag", flagOut.String())
			if flagEnv.SchemaVersion == nil || *flagEnv.SchemaVersion != output.SchemaVersion || flagEnv.OK == nil || *flagEnv.OK || flagEnv.Error == nil {
				t.Fatalf("%s unknown-flag envelope = %+v\nstdout: %s", path, flagEnv, flagOut.String())
			}
		})
	}

	for path := range contractSkip {
		if !seen[path] {
			t.Errorf("contractSkip lists %q, but the command tree has no such leaf; remove the stale entry", path)
		}
	}
}

// contractLeaf is one runnable entry point in the tree.
type contractLeaf struct {
	Path    []string
	HasJSON bool
}

// contractLeaves walks the command tree and returns every leaf with the
// --json flag it exposes (local or inherited).
func contractLeaves(t *testing.T, root *cobra.Command) []contractLeaf {
	t.Helper()
	var leaves []contractLeaf
	var walk func(*cobra.Command, []string)
	walk = func(cmd *cobra.Command, path []string) {
		next := append(append([]string{}, path...), cmd.Name())
		// The root itself only prints help. Every other runnable command counts
		// as an entry point, even when it also has subcommands (audit, migrate).
		if cmd != root && (len(cmd.Commands()) == 0 || cmd.Runnable()) {
			leaves = append(leaves, contractLeaf{Path: next, HasJSON: hasJSONFlag(cmd)})
		}
		for _, child := range cmd.Commands() {
			walk(child, next)
		}
	}
	walk(root, nil)
	return leaves
}

// hasJSONFlag looks for --json on the command or any parent's persistent flags.
func hasJSONFlag(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Flags().Lookup("json") != nil || c.PersistentFlags().Lookup("json") != nil {
			return true
		}
	}
	return false
}

// decodeContractEnvelope requires stdout to be exactly one JSON object.
func decodeContractEnvelope(t *testing.T, path, stdout string) contractEnvelope {
	t.Helper()
	trimmed := strings.TrimSuffix(stdout, "\n")
	if strings.Contains(trimmed, "\n") {
		t.Fatalf("%s wrote %d lines on stdout, want exactly one envelope:\n%s", path, strings.Count(trimmed, "\n")+1, stdout)
	}
	if trimmed == "" {
		t.Fatalf("%s wrote no JSON envelope on stdout", path)
	}
	var env contractEnvelope
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		t.Fatalf("%s stdout is not one JSON object: %v\nstdout: %s", path, err, stdout)
	}
	return env
}

// slug turns a command path into a subtest name.
func slug(path string) string {
	return strings.ReplaceAll(path, " ", "_")
}
