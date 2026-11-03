package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/spf13/cobra"
)

// withRuntimeFixture registers a throwaway command in the root tree so a test
// can drive run() through a runtime failure.
func withRuntimeFixture(t *testing.T, runE func(*cobra.Command, []string) error) {
	t.Helper()
	t.Setenv("ORBIT_SKIP_HOSTGATE", "1")

	previous := rootCmdFactory
	rootCmdFactory = func() *cobra.Command {
		root := newRootCmd()
		var jsonFlag bool
		fixture := &cobra.Command{Use: "fixture-runtime", RunE: runE}
		fixture.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
		root.AddCommand(fixture)
		return root
	}
	t.Cleanup(func() { rootCmdFactory = previous })
}

// onlyEnvelope fails unless stdout is exactly one envelope line.
func onlyEnvelope(t *testing.T, out string) output.Envelope {
	t.Helper()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("stdout does not end in a newline: %q", out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("stdout has %d lines, want 1: %q", len(lines), out)
	}
	var env output.Envelope
	if err := json.Unmarshal([]byte(lines[0]), &env); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, out)
	}
	return env
}

func TestRunJSONUsageErrorHasOneEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"port", "allocate", "--json"}, &stdout, &stderr)

	if code != clierr.ExitUsage {
		t.Errorf("exit = %d, want %d", code, clierr.ExitUsage)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	env := onlyEnvelope(t, stdout.String())
	if env.OK {
		t.Error("ok = true, want false")
	}
	if env.Error == nil || env.Error.Code != clierr.CodeUsage {
		t.Fatalf("error = %+v, want code %q", env.Error, clierr.CodeUsage)
	}
}

func TestRunUnknownCommandText(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"nosuchcmd"}, &stdout, &stderr)

	if code != clierr.ExitUsage {
		t.Errorf("exit = %d, want %d", code, clierr.ExitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"Error: unknown command",
		"Run 'orbit --help' for usage.",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q does not contain %q", stderr.String(), want)
		}
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr printed the usage block: %q", stderr.String())
	}
}

func TestRunUnknownFlagText(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"port", "allocate", "--nope"}, &stdout, &stderr)

	if code != clierr.ExitUsage {
		t.Errorf("exit = %d, want %d", code, clierr.ExitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"Error: unknown flag: --nope",
		"Run 'orbit port allocate --help' for usage.",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q does not contain %q", stderr.String(), want)
		}
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr printed the usage block: %q", stderr.String())
	}
}

func TestRunUnknownFlagJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"port", "allocate", "--json", "--nope"}, &stdout, &stderr)

	if code != clierr.ExitUsage {
		t.Errorf("exit = %d, want %d", code, clierr.ExitUsage)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	env := onlyEnvelope(t, stdout.String())
	if env.Error == nil || env.Error.Code != clierr.CodeUsage {
		t.Fatalf("error = %+v, want code %q", env.Error, clierr.CodeUsage)
	}
	if !strings.Contains(env.Error.Message, "unknown flag") {
		t.Errorf("message = %q, want it to mention the unknown flag", env.Error.Message)
	}
}

func TestRunRuntimeErrorText(t *testing.T) {
	withRuntimeFixture(t, func(cmd *cobra.Command, args []string) error {
		return clierr.WithHint(clierr.Tool("x"), "y")
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"fixture-runtime"}, &stdout, &stderr)

	if code != clierr.ExitTool {
		t.Errorf("exit = %d, want %d", code, clierr.ExitTool)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if got, want := stderr.String(), "Error: x\n  hint: y\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr printed the usage block: %q", stderr.String())
	}
}

func TestRunRuntimeErrorJSON(t *testing.T) {
	withRuntimeFixture(t, func(cmd *cobra.Command, args []string) error {
		return clierr.WithHint(clierr.Tool("x"), "y")
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"fixture-runtime", "--json"}, &stdout, &stderr)

	if code != clierr.ExitTool {
		t.Errorf("exit = %d, want %d", code, clierr.ExitTool)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	env := onlyEnvelope(t, stdout.String())
	if env.Error == nil {
		t.Fatal("error = nil, want a body")
	}
	if env.Error.Code != clierr.CodeTool || env.Error.Message != "x" || env.Error.Hint != "y" {
		t.Errorf("error = %+v, want {code: tool, message: x, hint: y}", env.Error)
	}
}

func TestRunCommandEnvelopeIsNotDuplicated(t *testing.T) {
	failure := clierr.Tool("x")
	withRuntimeFixture(t, func(cmd *cobra.Command, args []string) error {
		if err := output.Write(cmd.OutOrStdout(), output.Failure(failure)); err != nil {
			return err
		}
		return failure
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"fixture-runtime", "--json"}, &stdout, &stderr)

	if code != clierr.ExitTool {
		t.Errorf("exit = %d, want %d", code, clierr.ExitTool)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	env := onlyEnvelope(t, stdout.String())
	if env.Error == nil || env.Error.Message != "x" {
		t.Errorf("error = %+v, want the command's own envelope", env.Error)
	}
}

func TestRunVersionText(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)

	if code != clierr.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	if !strings.HasPrefix(out, "orbit version "+version) {
		t.Errorf("stdout = %q, want it to start with %q", out, "orbit version "+version)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("stdout is not a single line: %q", out)
	}
}

func TestRunVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version", "--json"}, &stdout, &stderr)

	if code != clierr.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	env := onlyEnvelope(t, stdout.String())
	if !env.OK || env.Error != nil {
		t.Fatalf("envelope = %+v, want a success envelope", env)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want an object", env.Data)
	}
	for _, key := range []string{"version", "commit", "built"} {
		if _, present := data[key]; !present {
			t.Errorf("data is missing %q: %+v", key, data)
		}
	}
	if len(data) != 3 {
		t.Errorf("data has %d keys, want 3: %+v", len(data), data)
	}
	if got := data["version"]; got != version {
		t.Errorf("version = %v, want %q", got, version)
	}
}

func TestAsUsageError(t *testing.T) {
	plain := errors.New("boom")
	tests := []struct {
		name     string
		err      error
		wantCode string
		wantSame bool
	}{
		{name: "nil", err: nil, wantCode: ""},
		{name: "unknown command", err: errors.New(`unknown command "x" for "orbit"`), wantCode: clierr.CodeUsage},
		{name: "unknown flag", err: errors.New("unknown flag: --nope"), wantCode: clierr.CodeUsage},
		{name: "unknown shorthand flag", err: errors.New("unknown shorthand flag: 'z' in -z"), wantCode: clierr.CodeUsage},
		{name: "flag needs an argument", err: errors.New("flag needs an argument: --dir"), wantCode: clierr.CodeUsage},
		{name: "invalid argument", err: errors.New(`invalid argument "x" for "--count"`), wantCode: clierr.CodeUsage},
		{name: "required flag", err: errors.New(`required flag(s) "dir" not set`), wantCode: clierr.CodeUsage},
		{name: "accepts args", err: errors.New("accepts 2 arg(s), received 0"), wantCode: clierr.CodeUsage},
		{name: "at least", err: errors.New("requires at least 1 arg(s), only received 0"), wantCode: clierr.CodeUsage},
		{name: "at most", err: errors.New("accepts at most 1 arg(s), received 2"), wantCode: clierr.CodeUsage},
		{name: "help topic", err: errors.New(`unknown help topic ["x"]`), wantCode: clierr.CodeUsage},
		{name: "already clierr", err: clierr.Tool("git missing"), wantCode: clierr.CodeTool, wantSame: true},
		{name: "plain", err: plain, wantCode: clierr.CodeFailed, wantSame: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := asUsageError(tc.err)
			if tc.err == nil {
				if got != nil {
					t.Fatalf("asUsageError(nil) = %v, want nil", got)
				}
				return
			}
			if code := clierr.Code(got); code != tc.wantCode {
				t.Errorf("Code = %q, want %q (err: %v)", code, tc.wantCode, got)
			}
			if tc.wantSame && got != tc.err {
				t.Errorf("asUsageError changed an error that already had a class: %v", got)
			}
		})
	}
}

func TestJSONRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "empty", args: nil, want: false},
		{name: "bare", args: []string{"--json"}, want: true},
		{name: "true", args: []string{"--json=true"}, want: true},
		{name: "false", args: []string{"--json=false"}, want: false},
		{name: "after subcommand", args: []string{"port", "list", "--json"}, want: true},
		{name: "after terminator", args: []string{"--", "--json"}, want: false},
		{name: "other flags", args: []string{"--config", "x", "--json"}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := jsonRequested(tc.args); got != tc.want {
				t.Errorf("jsonRequested(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}
