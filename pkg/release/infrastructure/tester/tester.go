// Package tester runs a repository's own test command with the release-tier
// environment through a runner.Runner.
package tester

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/release/application"
	releasedomain "github.com/manovaspace/orbit-cli/pkg/release/domain"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Tester detects and runs the repository's test command. The test command
// needs a per-run environment, so it uses runner.Exec directly; gh, git and
// psql still go through the injected runner.Runner.
type Tester struct {
	Files application.Files
	Out   io.Writer
}

// New builds a Tester.
func New(files application.Files) *Tester { return &Tester{Files: files} }

// Run executes the repository's test command with env. Output is streamed and
// redacted before it reaches Out.
func (t *Tester) Run(ctx context.Context, dir string, env map[string]string) error {
	name, args, err := t.command(dir)
	if err != nil {
		return err
	}
	exe := runner.Exec{Env: mergeEnv(env)}
	res, runErr := exe.Run(ctx, dir, name, args...)
	t.write(res.Stdout, env)
	t.write(res.Stderr, env)
	if runErr != nil {
		return clierr.Wrap(runErr, clierr.Failed("test command failed"))
	}
	return nil
}

func (t *Tester) command(dir string) (string, []string, error) {
	if t.Files.Exists(filepath.Join(dir, "go.mod")) {
		return "go", []string{"test", "-race", "-count=1", "./..."}, nil
	}
	if data, err := t.Files.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(data, &pkg) == nil && strings.TrimSpace(pkg.Scripts["test"]) != "" {
			return "bun", []string{"run", "test"}, nil
		}
		return "bun", []string{"test"}, nil
	}
	return "", nil, clierr.Unverified("no test command found; expected go.mod or package.json")
}

func (t *Tester) write(text string, env map[string]string) {
	if t.Out == nil || strings.TrimSpace(text) == "" {
		return
	}
	_, _ = t.Out.Write([]byte(releasedomain.Redact(text, secretValues(env))))
}

func mergeEnv(overrides map[string]string) []string {
	merged := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			merged[key] = value
		}
	}
	for key, value := range overrides {
		merged[key] = value
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+merged[key])
	}
	return out
}

func secretValues(env map[string]string) []string {
	var secrets []string
	for key, value := range env {
		upper := strings.ToUpper(key)
		if !strings.Contains(upper, "URL") && !strings.Contains(upper, "PASSWORD") &&
			!strings.Contains(upper, "SECRET") && !strings.Contains(upper, "TOKEN") {
			continue
		}
		secrets = append(secrets, value)
	}
	return secrets
}
