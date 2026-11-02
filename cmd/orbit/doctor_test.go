package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	doctorapp "github.com/manovaspace/orbit-cli/pkg/doctor/application"
	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/output"
)

// stubProbe answers every probe with a healthy version.
type stubProbe struct {
	gitMissing bool
}

func (p stubProbe) Run(_ context.Context, _ time.Duration, name string, args ...string) (doctorapp.ProbeResult, error) {
	switch name {
	case "git":
		if p.gitMissing {
			return doctorapp.ProbeResult{}, errors.New("git missing")
		}
		return doctorapp.ProbeResult{Output: "git version 2.50.0"}, nil
	case "go":
		return doctorapp.ProbeResult{Output: "go version go1.26.7 linux/amd64"}, nil
	case "node":
		return doctorapp.ProbeResult{Output: "v24.0.0"}, nil
	case "bun":
		return doctorapp.ProbeResult{Output: "1.4.0"}, nil
	case "caddy":
		return doctorapp.ProbeResult{Output: "v2.10.0"}, nil
	case "typst":
		return doctorapp.ProbeResult{Output: "typst 0.14.0"}, nil
	}
	return doctorapp.ProbeResult{}, nil
}

// stubHost is a healthy Ubuntu host with no project manifests.
type stubHost struct{}

func (stubHost) Report() host.Report { return host.Report{OK: true} }
func (stubHost) Resources() domain.Resources {
	return domain.Resources{}
}
func (stubHost) LookPath(string) (string, error) {
	return "/usr/bin/tool", nil
}
func (stubHost) Getwd() (string, error)                  { return "/tmp", nil }
func (stubHost) ReadFile(string) ([]byte, error)         { return nil, errNotExist{} }
func (stubHost) Stat(string) (doctorapp.FileInfo, error) { return doctorapp.FileInfo{}, errNotExist{} }
func (stubHost) Lstat(string) (doctorapp.FileInfo, error) {
	return doctorapp.FileInfo{}, errNotExist{}
}
func (stubHost) Getenv(string) string { return "" }

type stubPorts struct{}

func (stubPorts) Scan(start, end int) map[int]bool { return map[int]bool{} }

type stubManifest struct{}

func (stubManifest) Load(string) (*manifest.WorkspaceManifest, error) {
	return &manifest.WorkspaceManifest{}, nil
}

type errNotExist struct{}

func (errNotExist) Error() string { return "not exist" }
func (errNotExist) Unwrap() error { return fs.ErrNotExist }

// healthyDoctorService is a deterministic diagnostics service for command tests.
func healthyDoctorService(probe stubProbe, healers *domain.HealerRegistry) func(io.Writer) *doctorapp.Service {
	return func(out io.Writer) *doctorapp.Service {
		if healers == nil {
			healers = domain.NewHealerRegistry()
		}
		return &doctorapp.Service{
			Probe:    probe,
			Host:     stubHost{},
			Ports:    stubPorts{},
			Manifest: stubManifest{},
			Healers:  healers,
			Out:      out,
			UI:       doctorUI(),
		}
	}
}

func executeDoctor(t *testing.T, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	cmd := newDoctorCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	return out, cmd.Execute()
}

func TestDoctorCmdExecution(t *testing.T) {
	previous := doctorNewService
	previousPaths := doctorWorkspacePaths
	t.Cleanup(func() { doctorNewService, doctorWorkspacePaths = previous, previousPaths })
	doctorNewService = healthyDoctorService(stubProbe{}, nil)
	doctorWorkspacePaths = func() (string, string) { return "", "" }

	out, err := executeDoctor(t)
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	text := out.String()
	if !strings.Contains(text, "Orbit System Doctor") {
		t.Errorf("expected header, got: %s", text)
	}
	for _, category := range []string{"System", "Toolchain", "Runtime", "Container", "Authentication", "Ports"} {
		if !strings.Contains(text, category) {
			t.Errorf("missing category %q: %s", category, text)
		}
	}
}

func TestDoctorCmdJSONEnvelope(t *testing.T) {
	previous := doctorNewService
	previousPaths := doctorWorkspacePaths
	t.Cleanup(func() { doctorNewService, doctorWorkspacePaths = previous, previousPaths })
	doctorNewService = healthyDoctorService(stubProbe{}, nil)
	doctorWorkspacePaths = func() (string, string) { return "", "" }

	out, err := executeDoctor(t, "--json")
	if err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, out)
	}
	var envelope output.Envelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if envelope.SchemaVersion != output.SchemaVersion || !envelope.OK {
		t.Fatalf("envelope = %+v", envelope)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("re-encode data: %v", err)
	}
	var report doctor.DoctorReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if len(report.Results) == 0 {
		t.Fatal("no diagnostic results in envelope data")
	}
}

func TestDoctorCmdJSONFailureEnvelope(t *testing.T) {
	previous := doctorNewService
	previousPaths := doctorWorkspacePaths
	t.Cleanup(func() { doctorNewService, doctorWorkspacePaths = previous, previousPaths })
	doctorNewService = healthyDoctorService(stubProbe{gitMissing: true}, nil)
	doctorWorkspacePaths = func() (string, string) { return "", "" }

	out, err := executeDoctor(t, "--json")
	if err == nil {
		t.Fatalf("doctor --json with findings succeeded: %s", out)
	}
	var envelope output.Envelope
	if err := json.Unmarshal([]byte(strings.SplitN(out.String(), "\n", 2)[0]), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != "failed" {
		t.Fatalf("envelope = %+v", envelope)
	}
}

type countingHealer struct{ runs int }

func (h *countingHealer) Name() string { return "counter" }
func (h *countingHealer) CanHeal(result doctor.DiagnosticResult) bool {
	return result.Status == doctor.StatusError
}
func (h *countingHealer) Heal(context.Context, func(string)) error { h.runs++; return nil }

func TestDoctorCmdFixRequiresFlag(t *testing.T) {
	previous := doctorNewService
	previousPaths := doctorWorkspacePaths
	t.Cleanup(func() { doctorNewService, doctorWorkspacePaths = previous, previousPaths })
	healer := &countingHealer{}
	registry := domain.NewHealerRegistry()
	registry.Register(healer)
	doctorNewService = healthyDoctorService(stubProbe{gitMissing: true}, registry)
	doctorWorkspacePaths = func() (string, string) { return "", "" }

	if _, err := executeDoctor(t, "--json"); err == nil {
		t.Fatal("expected findings")
	}
	if healer.runs != 0 {
		t.Fatal("doctor healed without --fix")
	}
	if _, err := executeDoctor(t, "--json", "--fix"); err == nil {
		t.Fatal("expected findings after fix")
	}
	if healer.runs != 1 {
		t.Fatalf("healer runs = %d, want 1", healer.runs)
	}
}

func TestDoctorCmdFlagsRegistration(t *testing.T) {
	cmd := newDoctorCmd()

	fixFlag := cmd.Flags().Lookup("fix")
	if fixFlag == nil {
		t.Fatal("expected --fix flag to be registered")
	}
	if fixFlag.Shorthand != "f" {
		t.Errorf("expected shorthand for --fix to be 'f', got %q", fixFlag.Shorthand)
	}

	if cmd.Flags().Lookup("json") == nil {
		t.Fatal("expected --json flag to be registered")
	}

	yesFlag := cmd.Flags().Lookup("yes")
	if yesFlag == nil {
		t.Fatal("expected --yes flag to be registered")
	}
	if yesFlag.Shorthand != "y" {
		t.Errorf("expected shorthand for --yes to be 'y', got %q", yesFlag.Shorthand)
	}
	if yesFlag.Usage != "Compatibility flag; doctor does not prompt" {
		t.Errorf("unexpected --yes usage %q", yesFlag.Usage)
	}

	nonInteractiveFlag := cmd.Flags().Lookup("non-interactive")
	if nonInteractiveFlag == nil {
		t.Fatal("expected --non-interactive flag to be registered")
	}
	if nonInteractiveFlag.Usage != "Compatibility flag; doctor does not prompt" {
		t.Errorf("unexpected --non-interactive usage %q", nonInteractiveFlag.Usage)
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
