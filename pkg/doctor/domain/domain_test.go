package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/host"
)

func TestParseVersionString(t *testing.T) {
	tests := []struct {
		raw      string
		tool     string
		expected string
		wantErr  bool
	}{
		{"go version go1.26.0 linux/amd64", "go", "1.26.0", false},
		{"go version go1.23.1 linux/amd64", "go", "1.23.1", false},
		{"v24.18.0\n", "node", "24.18.0", false},
		{"1.3.14\n", "bun", "1.3.14", false},
		{"v2.8.4 h1:DqdHgkP18Ebgblm2c+3gP57y3sL10\n", "caddy", "2.8.4", false},
		{"typst 0.15.1 (unknown commit)\n", "typst", "0.15.1", false},
		{"Docker Compose version 2.40.3+ds1-0ubuntu1\n", "docker-compose", "2.40.3", false},
		{"", "go", "", true},
		{"unrecognized output format", "go", "", true},
	}
	for _, tc := range tests {
		got, err := ParseSemver(tc.raw, tc.tool)
		if tc.wantErr {
			if err == nil {
				t.Errorf("expected error for %q, got version %q", tc.raw, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.raw, err)
		}
		if got != tc.expected {
			t.Errorf("ParseSemver(%q) = %q, want %q", tc.raw, got, tc.expected)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1, v2   string
		expected int
	}{
		{"1.23.1", "1.23.0", 1},
		{"1.23.0", "1.23", 0},
		{"1.23", "1.23.0", 0},
		{"1.22.9", "1.23.0", -1},
		{"24.18.0", "22.0.0", 1},
		{"18.19.0", "20.0.0", -1},
		{"1.3.14", "1.1.0", 1},
		{"1.0.0", "1.1.0", -1},
	}
	for _, tc := range tests {
		if got := CompareVersions(tc.v1, tc.v2); got != tc.expected {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.v1, tc.v2, got, tc.expected)
		}
	}
}

func TestEvaluateHost(t *testing.T) {
	ok := EvaluateHost(host.Report{OK: true})
	if ok.Status != StatusOK || ok.Category != "System" || ok.Name != "Host" {
		t.Errorf("ok result = %+v", ok)
	}
	if ok.Message != "Supported host: Ubuntu 24.04/26.04 LTS amd64, zsh, ~/.local/bin" {
		t.Errorf("unexpected OK message: %q", ok.Message)
	}

	failReport := host.Report{OK: false, Failures: []host.Failure{{Code: "os", Message: "darwin is not supported"}}}
	fail := EvaluateHost(failReport)
	if fail.Status != StatusError || fail.FixSuggestion == "" {
		t.Errorf("failure result = %+v", fail)
	}
	if fail.Message != strings.TrimSpace(host.Format(failReport)) {
		t.Errorf("message = %q", fail.Message)
	}
}

func TestEvaluateGoVersion(t *testing.T) {
	if res := EvaluateGoVersion("go version go1.26.0 linux/amd64", nil); res.Status != StatusOK || !strings.Contains(res.Message, ">= 1.26.0 required") {
		t.Errorf("go1.26.0 = %+v", res)
	}
	if res := EvaluateGoVersion("go version go1.26.2 linux/amd64", nil); res.Status != StatusOK {
		t.Errorf("go1.26.2 = %+v", res)
	}
	for _, raw := range []string{"go version go1.24.2 linux/amd64", "go version go1.25.9 linux/amd64", "go version go1.23.1 linux/amd64"} {
		if res := EvaluateGoVersion(raw, nil); res.Status != StatusError {
			t.Errorf("%s = %+v", raw, res)
		}
	}
	if res := EvaluateGoVersion("", errors.New("exec: not found")); res.Status != StatusError || !strings.Contains(res.FixSuggestion, "1.26.0") {
		t.Errorf("missing go = %+v", res)
	}
	if res := EvaluateGoVersion("custom go build without semver", nil); res.Status != StatusWarning {
		t.Errorf("unparseable go = %+v", res)
	}
}

func TestEvaluateNodeAndBunVersions(t *testing.T) {
	if res := EvaluateNodeVersion("v24.18.0\n", nil); res.Status != StatusOK {
		t.Errorf("node 24 = %+v", res)
	}
	if res := EvaluateNodeVersion("v22.14.0\n", nil); res.Status != StatusError {
		t.Errorf("node 22 = %+v", res)
	}
	if res := EvaluateNodeVersion("", errors.New("not found")); res.Status != StatusError {
		t.Errorf("missing node = %+v", res)
	}
	for _, raw := range []string{"1.4.0\n", "1.1.0\n", "1.5.0\n"} {
		if res := EvaluateBunVersion(raw, nil); res.Status != StatusOK {
			t.Errorf("bun %s = %+v", raw, res)
		}
	}
	if res := EvaluateBunVersion("", errors.New("not found")); res.Status != StatusError {
		t.Errorf("missing bun = %+v", res)
	}
}

func TestEvaluateDocker(t *testing.T) {
	if res := EvaluateDockerDaemon("Client: Docker Engine\nServer: Docker Engine\n", nil, true); res.Status != StatusOK {
		t.Errorf("running daemon = %+v", res)
	}
	if res := EvaluateDockerDaemon("", errors.New("Cannot connect"), false); !strings.Contains(res.Message, "CLI not found") {
		t.Errorf("missing CLI = %+v", res)
	}
	if res := EvaluateDockerDaemon("", errors.New("Cannot connect"), true); !strings.Contains(res.Message, "not running") {
		t.Errorf("stopped daemon = %+v", res)
	}
	if res := EvaluateDockerCompose("Docker Compose version 2.40.3\n", nil); res.Status != StatusOK {
		t.Errorf("compose = %+v", res)
	}
	if res := EvaluateDockerCompose("", errors.New("not found")); res.Status != StatusError {
		t.Errorf("missing compose = %+v", res)
	}
}

func TestEvaluateGitHubSSH(t *testing.T) {
	if res := EvaluateGitHubSSH("Hi octocat! You've successfully authenticated, but GitHub does not provide shell access.", errors.New("exit status 1")); res.Status != StatusOK {
		t.Errorf("github auth = %+v", res)
	}
	if res := EvaluateGitHubSSH("git@github.com: Permission denied (publickey).", errors.New("exit status 255")); res.Status != StatusWarning {
		t.Errorf("github denied = %+v", res)
	}
}

func TestEvaluatePortScan(t *testing.T) {
	if res := EvaluatePortScan(map[int]bool{10000: false, 10050: false}); res.Status != StatusOK {
		t.Errorf("free ports = %+v", res)
	}
	if res := EvaluatePortScan(map[int]bool{10000: false, 10050: true}); res.Status != StatusWarning {
		t.Errorf("bound ports = %+v", res)
	}
}

func TestEvaluateOptionalTools(t *testing.T) {
	if res := EvaluateCaddyVersion("v2.8.4 h1:xyz\n", nil); res.Status != StatusOK {
		t.Errorf("caddy = %+v", res)
	}
	if res := EvaluateCaddyVersion("", errors.New("not found")); res.Status != StatusWarning {
		t.Errorf("missing caddy = %+v", res)
	}
	if res := EvaluateTypstVersion("typst 0.15.1 (unknown commit)\n", nil); res.Status != StatusOK {
		t.Errorf("typst = %+v", res)
	}
	if res := EvaluateTypstVersion("", errors.New("not found")); res.Status != StatusWarning {
		t.Errorf("missing typst = %+v", res)
	}
}

func TestDoctorReportAggregation(t *testing.T) {
	report := &DoctorReport{}
	if report.HasErrors() || report.HasWarnings() {
		t.Error("empty report reports problems")
	}
	report.Add(DiagnosticResult{Category: "System", Name: "OS", Status: StatusOK, Message: "Ubuntu"})
	if report.HasErrors() || report.HasWarnings() {
		t.Error("OK-only report reports problems")
	}
	report.Add(DiagnosticResult{Category: "Optional Tools", Name: "Caddy", Status: StatusWarning})
	if !report.HasWarnings() || report.HasErrors() {
		t.Error("warning not counted")
	}
	report.Add(DiagnosticResult{Category: "Toolchain", Name: "Go", Status: StatusError})
	if !report.HasErrors() || report.CountErrors() != 1 || len(report.Results) != 3 {
		t.Errorf("error not counted: %+v", report)
	}
}

type fakeCheck struct {
	name    string
	results []DiagnosticResult
}

func (c fakeCheck) Name() string                           { return c.name }
func (c fakeCheck) Run(context.Context) []DiagnosticResult { return c.results }

func TestCheckRegistryJoinsWithOneLine(t *testing.T) {
	registry := NewRegistry()
	registry.Register(fakeCheck{name: "host", results: []DiagnosticResult{{Name: "Host", Status: StatusOK}}})
	registry.Register(fakeCheck{name: "git", results: []DiagnosticResult{{Name: "Git", Status: StatusOK}}})
	if len(registry.All()) != 2 {
		t.Fatalf("checks = %d, want 2", len(registry.All()))
	}
	report := registry.Run(context.Background())
	if len(report.Results) != 2 || report.Results[1].Name != "Git" {
		t.Fatalf("report = %+v", report)
	}
}

type fakeHealer struct {
	name string
	fix  bool
}

func (h fakeHealer) Name() string { return h.name }
func (h fakeHealer) CanHeal(result DiagnosticResult) bool {
	return h.fix && result.Status != StatusOK && result.Status != StatusUnverified
}
func (h fakeHealer) Heal(context.Context, func(string)) error { return nil }

func TestHealerRegistryDeduplicates(t *testing.T) {
	registry := NewHealerRegistry()
	registry.Register(fakeHealer{name: "one", fix: true})
	registry.Register(fakeHealer{name: "two", fix: false})
	results := []DiagnosticResult{
		{Name: "Go Compiler", Status: StatusError},
		{Name: "Go Compiler", Status: StatusWarning},
		{Name: "Bun", Status: StatusOK},
	}
	healers := registry.FindHealers(results)
	if len(healers) != 1 || healers[0].Name() != "one" {
		t.Fatalf("healers = %+v", healers)
	}
	if _, ok := registry.FindHealer(DiagnosticResult{Name: "Go", Status: StatusUnverified}); ok {
		t.Error("unverified result selected a healer")
	}
}
