package healer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
)

// mockRunner records executed commands and scripts, with optional scripted responses/errors.
type mockRunner struct {
	mu          sync.Mutex
	commands    [][]string
	scripts     []string
	runErr      error
	shellErr    error
	shellOutput string
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = append(m.commands, append([]string{name}, args...))
	return "", m.runErr
}

func (m *mockRunner) RunShell(ctx context.Context, script string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scripts = append(m.scripts, script)
	return m.shellOutput, m.shellErr
}

func (m *mockRunner) getLastScript() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.scripts) == 0 {
		return ""
	}
	return m.scripts[len(m.scripts)-1]
}

// TestDefaultRunner verifies default process and shell execution.
func TestDefaultRunner(t *testing.T) {
	runner := &DefaultRunner{}
	ctx := context.Background()

	out, err := runner.Run(ctx, "echo", "test-default-runner")
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}
	if !strings.Contains(out, "test-default-runner") {
		t.Errorf("expected echo output, got: %s", out)
	}

	out, err = runner.RunShell(ctx, "echo 'shell-test'")
	if err != nil {
		t.Fatalf("unexpected RunShell error: %v", err)
	}
	if !strings.Contains(out, "shell-test") {
		t.Errorf("expected shell output, got: %s", out)
	}
}

// TestRegistry verifies healer registration, retrieval, and matching logic.
func TestRegistry(t *testing.T) {
	reg := NewRegistry()
	if len(reg.All()) != 0 {
		t.Fatalf("expected empty registry, got %d healers", len(reg.All()))
	}

	goHealer := NewGoHealer()
	bunHealer := NewBunHealer()

	reg.Register(goHealer)
	reg.Register(bunHealer)

	if len(reg.All()) != 2 {
		t.Fatalf("expected 2 healers, got %d", len(reg.All()))
	}

	if h, ok := reg.Get("Go 1.24"); !ok || h == nil {
		t.Fatalf("expected to retrieve Go healer by name")
	}

	if _, ok := reg.Get("NonExistent"); ok {
		t.Fatalf("expected not to find NonExistent healer")
	}

	// FindHealer for Go
	goResult := doctor.DiagnosticResult{
		Category: "Toolchain",
		Name:     "Go Compiler",
		Status:   doctor.StatusError,
		Message:  "Go compiler not found in PATH",
	}

	h, found := reg.FindHealer(goResult)
	if !found || h.Name() != "Go 1.24" {
		t.Fatalf("expected FindHealer to find Go healer, got found=%v, h=%v", found, h)
	}

	// StatusOK should never match
	okResult := doctor.DiagnosticResult{
		Category: "Toolchain",
		Name:     "Go Compiler",
		Status:   doctor.StatusOK,
		Message:  "Go v1.24 installed",
	}
	if _, found := reg.FindHealer(okResult); found {
		t.Fatalf("expected StatusOK not to match any healer")
	}

	// FindHealers deduplication
	duplicateResults := []doctor.DiagnosticResult{
		goResult,
		{
			Category: "Toolchain",
			Name:     "Go Compiler",
			Status:   doctor.StatusError,
			Message:  "Go v1.20 is below required version",
		},
		{
			Category: "Runtime",
			Name:     "Bun",
			Status:   doctor.StatusError,
			Message:  "Bun JavaScript runtime not found in PATH",
		},
		okResult,
	}

	matched := reg.FindHealers(duplicateResults)
	if len(matched) != 2 {
		t.Fatalf("expected 2 deduplicated healers (Go, Bun), got %d", len(matched))
	}
	if matched[0].Name() != "Go 1.24" || matched[1].Name() != "Bun" {
		t.Fatalf("unexpected matched healers order: %v, %v", matched[0].Name(), matched[1].Name())
	}
}

// TestDefaultRegistry verifies NewDefaultRegistry registers all 5 standard healers.
func TestDefaultRegistry(t *testing.T) {
	reg := NewDefaultRegistry()
	all := reg.All()
	if len(all) != 5 {
		t.Fatalf("expected 5 default healers, got %d", len(all))
	}

	names := []string{"Go 1.24", "Bun", "Node.js 22 LTS", "Git", "Docker Compose"}
	for _, expectedName := range names {
		if _, ok := reg.Get(expectedName); !ok {
			t.Fatalf("expected default registry to contain %q", expectedName)
		}
	}
}

// TestGoHealerCanHeal tests GoHealer matching.
func TestGoHealerCanHeal(t *testing.T) {
	h := NewGoHealer()

	tests := []struct {
		result   doctor.DiagnosticResult
		expected bool
	}{
		{doctor.DiagnosticResult{Name: "Go Compiler", Status: doctor.StatusError}, true},
		{doctor.DiagnosticResult{Name: "go compiler", Status: doctor.StatusWarning}, true},
		{doctor.DiagnosticResult{Category: "Toolchain", Name: "Go", Status: doctor.StatusError}, true},
		{doctor.DiagnosticResult{Name: "Go Compiler", Status: doctor.StatusOK}, false},
		{doctor.DiagnosticResult{Name: "Node.js", Status: doctor.StatusError}, false},
	}

	for _, tt := range tests {
		got := h.CanHeal(tt.result)
		if got != tt.expected {
			t.Errorf("GoHealer.CanHeal(%+v) = %v; want %v", tt.result, got, tt.expected)
		}
	}
}

// TestGoHealerHeal tests GoHealer execution logic across root and non-root modes.
func TestGoHealerHeal(t *testing.T) {
	ctx := context.Background()

	t.Run("Non-root Linux execution", func(t *testing.T) {
		runner := &mockRunner{}
		var progressSteps []string

		h := &GoHealer{
			Runner:  runner,
			Version: "1.24.0",
			GOOS:    "linux",
			GOARCH:  "amd64",
			IsRoot:  func() bool { return false },
		}

		err := h.Heal(ctx, func(step string) {
			progressSteps = append(progressSteps, step)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(progressSteps) == 0 {
			t.Fatalf("expected progress steps, got 0")
		}

		script := runner.getLastScript()
		if !strings.Contains(script, "https://go.dev/dl/go1.24.0.linux-amd64.tar.gz") {
			t.Errorf("script missing download url: %s", script)
		}
		if !strings.Contains(script, "sudo tar -C /usr/local") {
			t.Errorf("script expected sudo tar for non-root, got: %s", script)
		}
		if !strings.Contains(script, "/etc/profile.d/orbit-go.sh") {
			t.Errorf("script expected profile configuration, got: %s", script)
		}
	})

	t.Run("Root Linux execution", func(t *testing.T) {
		runner := &mockRunner{}
		h := &GoHealer{
			Runner:  runner,
			Version: "1.24.0",
			GOOS:    "linux",
			GOARCH:  "arm64",
			IsRoot:  func() bool { return true },
		}

		err := h.Heal(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		script := runner.getLastScript()
		if !strings.Contains(script, "https://go.dev/dl/go1.24.0.linux-arm64.tar.gz") {
			t.Errorf("script missing arm64 download url: %s", script)
		}
		if strings.Contains(script, "sudo ") {
			t.Errorf("script should not use sudo when root, got: %s", script)
		}
	})

	t.Run("Unsupported OS error", func(t *testing.T) {
		runner := &mockRunner{}
		h := &GoHealer{
			Runner: runner,
			GOOS:   "darwin",
			GOARCH: "amd64",
		}
		err := h.Heal(ctx, nil)
		if err == nil || !strings.Contains(err.Error(), "supported on Linux only") {
			t.Fatalf("expected unsupported OS error, got: %v", err)
		}
	})

	t.Run("Unsupported Arch error", func(t *testing.T) {
		runner := &mockRunner{}
		h := &GoHealer{
			Runner: runner,
			GOOS:   "linux",
			GOARCH: "riscv64",
		}
		err := h.Heal(ctx, nil)
		if err == nil || !strings.Contains(err.Error(), "unsupported architecture") {
			t.Fatalf("expected unsupported arch error, got: %v", err)
		}
	})

	t.Run("Execution failure", func(t *testing.T) {
		runner := &mockRunner{
			shellErr:    errors.New("tar extraction failed"),
			shellOutput: "disk full",
		}
		h := &GoHealer{
			Runner: runner,
			GOOS:   "linux",
			GOARCH: "amd64",
			IsRoot: func() bool { return true },
		}
		err := h.Heal(ctx, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to install Go") {
			t.Fatalf("expected execution failure, got: %v", err)
		}
	})
}

// TestBunHealer tests BunHealer matching and execution.
func TestBunHealer(t *testing.T) {
	h := NewBunHealer()

	if !h.CanHeal(doctor.DiagnosticResult{Name: "Bun", Status: doctor.StatusError}) {
		t.Errorf("expected BunHealer to match Bun error")
	}
	if h.CanHeal(doctor.DiagnosticResult{Name: "Bun", Status: doctor.StatusOK}) {
		t.Errorf("expected BunHealer not to match Bun OK")
	}
	if h.CanHeal(doctor.DiagnosticResult{Name: "Docker", Status: doctor.StatusError}) {
		t.Errorf("expected BunHealer not to match Docker")
	}

	t.Run("Successful heal", func(t *testing.T) {
		runner := &mockRunner{}
		h.Runner = runner

		var steps []string
		err := h.Heal(context.Background(), func(step string) {
			steps = append(steps, step)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		script := runner.getLastScript()
		if !strings.Contains(script, "https://bun.sh/install") {
			t.Errorf("script expected bun.sh install URL, got: %s", script)
		}
		if len(steps) == 0 {
			t.Errorf("expected progress steps")
		}
	})

	t.Run("Failed heal", func(t *testing.T) {
		runner := &mockRunner{shellErr: errors.New("network error")}
		h.Runner = runner

		err := h.Heal(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "failed to install Bun") {
			t.Fatalf("expected install failure, got: %v", err)
		}
	})
}

// TestNodeHealer tests NodeHealer matching and execution.
func TestNodeHealer(t *testing.T) {
	h := NewNodeHealer()

	if !h.CanHeal(doctor.DiagnosticResult{Name: "Node.js", Status: doctor.StatusError}) {
		t.Errorf("expected NodeHealer to match Node.js error")
	}
	if h.CanHeal(doctor.DiagnosticResult{Name: "Node.js", Status: doctor.StatusOK}) {
		t.Errorf("expected NodeHealer not to match Node.js OK")
	}

	t.Run("Non-root installation", func(t *testing.T) {
		runner := &mockRunner{}
		h.Runner = runner
		h.IsRoot = func() bool { return false }

		err := h.Heal(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		script := runner.getLastScript()
		if !strings.Contains(script, "setup_22.x") {
			t.Errorf("expected NodeSource setup_22.x in script, got: %s", script)
		}
		if !strings.Contains(script, "sudo -E bash -") {
			t.Errorf("expected sudo -E bash for non-root in script, got: %s", script)
		}
		if !strings.Contains(script, "sudo apt-get install -y nodejs") {
			t.Errorf("expected sudo apt-get install in script, got: %s", script)
		}
	})

	t.Run("Root installation", func(t *testing.T) {
		runner := &mockRunner{}
		h.Runner = runner
		h.IsRoot = func() bool { return true }

		err := h.Heal(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		script := runner.getLastScript()
		if strings.Contains(script, "sudo ") {
			t.Errorf("expected no sudo when root, got: %s", script)
		}
	})

	t.Run("Installation error", func(t *testing.T) {
		runner := &mockRunner{shellErr: errors.New("apt failure")}
		h.Runner = runner
		h.IsRoot = func() bool { return true }

		err := h.Heal(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "failed to install Node.js 22") {
			t.Fatalf("expected node install error, got: %v", err)
		}
	})
}

// TestGitHealer tests GitHealer matching and execution.
func TestGitHealer(t *testing.T) {
	h := NewGitHealer()

	if !h.CanHeal(doctor.DiagnosticResult{Name: "Git", Status: doctor.StatusError}) {
		t.Errorf("expected GitHealer to match Git error")
	}
	if !h.CanHeal(doctor.DiagnosticResult{Name: "Git CLI", Status: doctor.StatusError}) {
		t.Errorf("expected GitHealer to match Git CLI error")
	}
	if h.CanHeal(doctor.DiagnosticResult{Name: "Forgejo SSH (git.dev.manova.space)", Status: doctor.StatusWarning}) {
		t.Errorf("expected GitHealer not to match SSH auth result")
	}
	if h.CanHeal(doctor.DiagnosticResult{Name: "GitHub SSH (github.com)", Status: doctor.StatusWarning}) {
		t.Errorf("expected GitHealer not to match GitHub SSH auth result")
	}

	t.Run("Successful heal", func(t *testing.T) {
		runner := &mockRunner{}
		h.Runner = runner
		h.IsRoot = func() bool { return false }

		err := h.Heal(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		script := runner.getLastScript()
		if !strings.Contains(script, "sudo apt-get install -y git ca-certificates") {
			t.Errorf("expected git and ca-certificates install command, got: %s", script)
		}
	})

	t.Run("Failure heal", func(t *testing.T) {
		runner := &mockRunner{shellErr: errors.New("lock error")}
		h.Runner = runner
		h.IsRoot = func() bool { return true }

		err := h.Heal(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "failed to install git") {
			t.Fatalf("expected git failure, got: %v", err)
		}
	})
}

// TestDockerComposeHealer tests DockerComposeHealer matching and execution.
func TestDockerComposeHealer(t *testing.T) {
	h := NewDockerComposeHealer()

	if !h.CanHeal(doctor.DiagnosticResult{Name: "Docker Compose", Status: doctor.StatusError}) {
		t.Errorf("expected DockerComposeHealer to match Docker Compose error")
	}
	if h.CanHeal(doctor.DiagnosticResult{Name: "Docker Compose", Status: doctor.StatusOK}) {
		t.Errorf("expected DockerComposeHealer not to match Docker Compose OK")
	}

	t.Run("Successful heal", func(t *testing.T) {
		runner := &mockRunner{}
		h.Runner = runner
		h.IsRoot = func() bool { return false }

		err := h.Heal(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		script := runner.getLastScript()
		if !strings.Contains(script, "sudo apt-get install -y docker-compose-plugin") {
			t.Errorf("expected docker-compose-plugin install command, got: %s", script)
		}
	})

	t.Run("Failure heal", func(t *testing.T) {
		runner := &mockRunner{shellErr: errors.New("package not found")}
		h.Runner = runner
		h.IsRoot = func() bool { return true }

		err := h.Heal(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "failed to install docker-compose-plugin") {
			t.Fatalf("expected docker compose failure, got: %v", err)
		}
	})
}

// TestRegistryRunAndIsolation tests running healers with error isolation.
func TestRegistryRunAndIsolation(t *testing.T) {
	reg := NewRegistry()

	failingRunner := &mockRunner{shellErr: errors.New("network timeout")}
	successRunner := &mockRunner{}

	goHealer := &GoHealer{
		Runner:  failingRunner,
		Version: "1.24.0",
		GOOS:    "linux",
		GOARCH:  "amd64",
		IsRoot:  func() bool { return true },
	}
	bunHealer := &BunHealer{
		Runner: successRunner,
	}

	reg.Register(goHealer)
	reg.Register(bunHealer)

	results := []doctor.DiagnosticResult{
		{Name: "Go Compiler", Status: doctor.StatusError},
		{Name: "Bun", Status: doctor.StatusError},
	}

	var progressReports []string
	healResults, err := reg.Run(context.Background(), results, func(name, status string) {
		progressReports = append(progressReports, name+": "+status)
	})

	if err != nil {
		t.Fatalf("unexpected fatal error from Run: %v", err)
	}

	if len(healResults) != 2 {
		t.Fatalf("expected 2 heal results, got %d", len(healResults))
	}

	// Go should have failed, Bun should have succeeded (error isolation)
	if healResults[0].Success != false || healResults[0].Error == nil {
		t.Errorf("expected Go healer to fail, got: %+v", healResults[0])
	}
	if healResults[1].Success != true || healResults[1].Error != nil {
		t.Errorf("expected Bun healer to succeed, got: %+v", healResults[1])
	}

	if len(progressReports) == 0 {
		t.Errorf("expected progress reports during execution")
	}

	// Test empty results
	emptyResults, err := reg.Run(context.Background(), nil, nil)
	if err != nil || len(emptyResults) != 0 {
		t.Errorf("expected empty results for nil input, got: %v, %v", emptyResults, err)
	}
}

// TestRunHealersHelper tests the package-level RunHealers function.
func TestRunHealersHelper(t *testing.T) {
	// StatusOK input should return empty results and no error
	results := []doctor.DiagnosticResult{
		{Name: "Go Compiler", Status: doctor.StatusOK},
	}
	healResults, err := RunHealers(context.Background(), results, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(healResults) != 0 {
		t.Errorf("expected 0 heal results for OK diagnostic, got: %d", len(healResults))
	}
}

// TestRunHealersContextCancellation tests cancelling execution midway.
func TestRunHealersContextCancellation(t *testing.T) {
	reg := NewRegistry()
	runner := &mockRunner{}

	bunHealer := &BunHealer{Runner: runner}
	reg.Register(bunHealer)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	results := []doctor.DiagnosticResult{
		{Name: "Bun", Status: doctor.StatusError},
	}

	_, err := reg.Run(ctx, results, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}
}

// TestIsAutoHealable verifies helper check.
func TestIsAutoHealable(t *testing.T) {
	if !IsAutoHealable(doctor.DiagnosticResult{Name: "Go Compiler", Status: doctor.StatusError}) {
		t.Errorf("expected Go Compiler to be auto healable")
	}
	if IsAutoHealable(doctor.DiagnosticResult{Name: "Go Compiler", Status: doctor.StatusOK}) {
		t.Errorf("expected OK status not to be auto healable")
	}
	if IsAutoHealable(doctor.DiagnosticResult{Name: "Unknown Custom Tool", Status: doctor.StatusError}) {
		t.Errorf("expected Unknown Tool not to be auto healable")
	}
}
