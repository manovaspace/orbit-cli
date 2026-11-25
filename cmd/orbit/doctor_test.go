package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

func executeDoctor(t *testing.T, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	cmd := newDoctorCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	return out, cmd.Execute()
}

// TestDoctorLocalIsCheckOnly: a pending setup action makes doctor exit 1 and never runs Do.
func TestDoctorLocalIsCheckOnly(t *testing.T) {
	fx := newSetupFixture(t, "system")
	fx.saveMachine(t, "a")

	out, err := executeDoctor(t)
	if clierr.Code(err) != clierr.CodeFailed || clierr.ExitCode(err) != clierr.ExitFailed {
		t.Fatalf("err = %v, want failed (exit 1)", err)
	}
	if len(fx.ran) != 0 {
		t.Fatalf("doctor ran actions: %v", fx.ran)
	}
	if !strings.Contains(out.String(), "system change") {
		t.Fatalf("doctor output does not show the pending action:\n%s", out)
	}
}

func TestDoctorLocalPassesWhenClean(t *testing.T) {
	fx := newSetupFixture(t)
	fx.saveMachine(t, "a")
	if _, err := executeDoctor(t, "--local"); err != nil {
		t.Fatalf("doctor --local on a clean setup: %v", err)
	}
	if len(fx.ran) != 0 {
		t.Fatalf("doctor ran actions: %v", fx.ran)
	}
}

func TestDoctorLocalJSONEnvelope(t *testing.T) {
	fx := newSetupFixture(t, "system")
	fx.saveMachine(t, "a")

	out, err := executeDoctor(t, "--json")
	if err == nil {
		t.Fatalf("doctor --json with a pending action succeeded: %s", out)
	}
	// The envelope is the first line; cobra prints the error after it when the command runs alone.
	var envelope output.Envelope
	if err := json.Unmarshal([]byte(strings.SplitN(out.String(), "\n", 2)[0]), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != "failed" {
		t.Fatalf("envelope = %+v", envelope)
	}
	data, _ := json.Marshal(envelope.Data)
	var res engine.Result
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("decode data: %v\n%s", err, out)
	}
	if res.Changed != 1 || len(res.Parts) == 0 {
		t.Fatalf("result = %+v, want one pending part", res)
	}
}

// TestDoctorFixIsUsage: doctor never changes the machine, so --fix is refused before any check.
func TestDoctorFixIsUsage(t *testing.T) {
	fx := newSetupFixture(t, "system")
	fx.saveMachine(t, "a")

	_, err := executeDoctor(t, "--fix")
	if clierr.Code(err) != clierr.CodeUsage {
		t.Fatalf("err = %v, want usage error", err)
	}
	if err.Error() != "o doctor never changes the machine; run o setup" {
		t.Fatalf("message = %q", err.Error())
	}
	if len(fx.ran) != 0 {
		t.Fatalf("doctor --fix ran actions: %v", fx.ran)
	}
}

func TestDoctorFlagsRegistration(t *testing.T) {
	cmd := newDoctorCmd()
	for _, name := range []string{"local", "remote", "accept-host-keys", "fix", "json", "yes", "non-interactive"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("expected --%s flag to be registered", name)
		}
	}
}

func TestLocalDoctorRejectsMutation(t *testing.T) {
	for _, args := range [][]string{{"--local", "--fix"}, {"--local", "--remote"}, {"--accept-host-keys"}, {"--remote", "--accept-host-keys"}} {
		cmd := newDoctorCmd()
		cmd.SetArgs(args)
		cmd.SetOut(new(bytes.Buffer))
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted unsafe combination %v", args)
		}
	}
	if !shouldSuppressPostRunNotices(newDoctorCmd()) {
		t.Fatal("doctor permits updater side effects")
	}
}

// TestDoctorLocalFixIsUsage: --local --fix gets the same refusal as --fix.
func TestDoctorLocalFixIsUsage(t *testing.T) {
	fx := newSetupFixture(t, "system")
	fx.saveMachine(t, "a")

	_, err := executeDoctor(t, "--local", "--fix")
	if clierr.Code(err) != clierr.CodeUsage || clierr.ExitCode(err) != clierr.ExitUsage {
		t.Fatalf("err = %v, want usage exit %d", err, clierr.ExitUsage)
	}
	if err.Error() != "o doctor never changes the machine; run o setup" {
		t.Fatalf("message = %q", err.Error())
	}
	if len(fx.ran) != 0 {
		t.Fatalf("doctor --local --fix ran actions: %v", fx.ran)
	}
}
