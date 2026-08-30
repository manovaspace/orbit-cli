package onboard_test

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/doctor/healer"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestDoctorStageAllPassing(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	docModel := tuiOnboard.NewDoctorModel(root)

	// Inject mock all-passing report
	report := doctor.NewReport()
	report.Add(doctor.CheckResult{
		Name:     "Go Toolchain",
		Category: doctor.CategoryCore,
		Status:   doctor.StatusPass,
		Message:  "go version go1.26.0 linux/amd64",
	})
	docModel.SetReport(report)

	view := docModel.View()
	if !strings.Contains(view, "Go Toolchain") || !strings.Contains(view, "go1.26.0") {
		t.Errorf("expected passing check in doctor view, got: %s", view)
	}

	// [Enter] when all passed transitions to StageIdentity
	docModel.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if root.ActiveStage() != session.StageIdentity {
		t.Errorf("expected transition to StageIdentity, got: %v", root.ActiveStage())
	}
}

func TestDoctorStageHealableErrors(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	docModel := tuiOnboard.NewDoctorModel(root)

	// Inject healable missing tool check
	report := doctor.NewReport()
	report.Add(doctor.CheckResult{
		Name:       "Caddy Server",
		Category:   doctor.CategoryNetwork,
		Status:     doctor.StatusError,
		Message:    "caddy is not installed or not in PATH",
		IsHealable: true,
	})
	docModel.SetReport(report)

	view := docModel.View()
	if !strings.Contains(view, "Auto-fix available") && !strings.Contains(view, "[H] Auto-heal") {
		t.Errorf("expected auto-heal prompt in view, got: %s", view)
	}
}

func TestDoctorStageCriticalUnhealableErrorBlocksAdvance(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	docModel := tuiOnboard.NewDoctorModel(root)

	// Inject unhealable error check
	report := doctor.NewReport()
	report.Add(doctor.CheckResult{
		Name:       "Host OS",
		Category:   doctor.CategoryCore,
		Status:     doctor.StatusError,
		Message:    "FreeBSD is not supported",
		IsHealable: false,
	})
	docModel.SetReport(report)

	// Press [Enter] - should NOT advance to StageIdentity
	docModel.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if root.ActiveStage() == session.StageIdentity {
		t.Errorf("expected stage not to transition to StageIdentity on critical error, got: %v", root.ActiveStage())
	}

	view := docModel.View()
	if !strings.Contains(view, "Host OS") || !strings.Contains(view, "FreeBSD is not supported") {
		t.Errorf("expected error details in view, got: %s", view)
	}
}

func TestDoctorStageWarningAllowsAdvance(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	docModel := tuiOnboard.NewDoctorModel(root)

	// Inject passing core and warning optional tool
	report := doctor.NewReport()
	report.Add(doctor.CheckResult{
		Name:     "Go Compiler",
		Category: doctor.CategoryCore,
		Status:   doctor.StatusPass,
		Message:  "go version go1.26.0 linux/amd64",
	})
	report.Add(doctor.CheckResult{
		Name:     "Typst Compiler",
		Category: doctor.CategoryDevTools,
		Status:   doctor.StatusWarning,
		Message:  "Typst not installed (optional)",
	})
	docModel.SetReport(report)

	// Press [Enter] - should advance to StageIdentity even with warnings
	docModel.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if root.ActiveStage() != session.StageIdentity {
		t.Errorf("expected transition to StageIdentity with warnings, got: %v", root.ActiveStage())
	}
}

func TestDoctorStageAsyncDiagnostics(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	docModel := tuiOnboard.NewDoctorModel(root)

	// Mock custom runner to return instantly
	expectedReport := doctor.NewReport()
	expectedReport.Add(doctor.CheckResult{
		Name:     "Mock Git",
		Category: doctor.CategoryCore,
		Status:   doctor.StatusPass,
		Message:  "git version 2.45.0",
	})
	docModel.SetDiagnosticsRunner(func() *doctor.DoctorReport {
		return expectedReport
	})

	cmd := docModel.RunDiagnostics()
	if cmd == nil {
		t.Fatalf("expected non-nil tea.Cmd from RunDiagnostics")
	}

	msg := cmd()
	diagMsg, ok := msg.(tuiOnboard.DiagnosticsFinishedMsg)
	if !ok {
		t.Fatalf("expected DiagnosticsFinishedMsg, got %T", msg)
	}
	if len(diagMsg.Report.Results) != 1 {
		t.Fatalf("expected 1 result in finished msg, got %d", len(diagMsg.Report.Results))
	}

	// Deliver message to model
	docModel.Update(diagMsg)
	if docModel.IsRunning() {
		t.Errorf("expected model not to be running after receiving DiagnosticsFinishedMsg")
	}
	if docModel.Report() == nil || len(docModel.Report().Results) != 1 {
		t.Errorf("expected report to be set on model")
	}
}

func TestDoctorStageAutoHealExecution(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	docModel := tuiOnboard.NewDoctorModel(root)

	report := doctor.NewReport()
	report.Add(doctor.CheckResult{
		Name:       "Bun",
		Category:   doctor.CategoryRuntime,
		Status:     doctor.StatusError,
		Message:    "Bun not found",
		IsHealable: true,
	})
	docModel.SetReport(report)

	// Set custom healer mock
	healedCalled := false
	docModel.SetHealerRunner(func(ctx context.Context, results []doctor.DiagnosticResult, progress func(string, string)) ([]healer.HealResult, error) {
		healedCalled = true
		return []healer.HealResult{
			{
				HealerName: "Bun",
				Success:    true,
				Message:    "Bun installed",
			},
		}, nil
	})

	// Press 'h' key
	_, cmd := docModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if cmd == nil {
		t.Fatalf("expected non-nil tea.Cmd after pressing 'h'")
	}

	msg := cmd()
	healMsg, ok := msg.(tuiOnboard.HealFinishedMsg)
	if !ok {
		t.Fatalf("expected HealFinishedMsg, got %T", msg)
	}
	if !healedCalled {
		t.Errorf("expected healer runner to be called")
	}
	if len(healMsg.Results) != 1 || !healMsg.Results[0].Success {
		t.Errorf("expected successful heal result")
	}
}

func TestDoctorStageCategoryGrouping(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	docModel := tuiOnboard.NewDoctorModel(root)

	report := doctor.NewReport()
	report.Add(doctor.CheckResult{
		Name:     "Host OS",
		Category: doctor.CategoryCore,
		Status:   doctor.StatusPass,
		Message:  "Ubuntu 24.04 LTS",
	})
	report.Add(doctor.CheckResult{
		Name:     "Docker Daemon",
		Category: doctor.CategoryContainer,
		Status:   doctor.StatusPass,
		Message:  "Docker running",
	})
	report.Add(doctor.CheckResult{
		Name:     "SSH Key",
		Category: doctor.CategorySecurity,
		Status:   doctor.StatusPass,
		Message:  "Key configured",
	})
	docModel.SetReport(report)

	view := docModel.View()
	for _, cat := range []string{"Core", "Container", "Security"} {
		if !strings.Contains(view, cat) {
			t.Errorf("expected category header %q in view, got: %s", cat, view)
		}
	}
}

func TestDoctorStageRetryKey(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	docModel := tuiOnboard.NewDoctorModel(root)

	// Press 'r'
	_, cmd := docModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatalf("expected non-nil tea.Cmd on 'r' retry")
	}
	if !docModel.IsRunning() {
		t.Errorf("expected model to be in running state after 'r'")
	}
}

func TestDoctorStageRunningAndHealingViews(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	docModel := tuiOnboard.NewDoctorModel(root)

	// Trigger running
	docModel.RunDiagnostics()
	viewRunning := docModel.View()
	if !strings.Contains(viewRunning, "Running pre-flight system diagnostics") {
		t.Errorf("expected running text in view, got: %s", viewRunning)
	}

	// Trigger healing
	docModel.RunAutoHeal()
	viewHealing := docModel.View()
	if !strings.Contains(viewHealing, "Auto-healing toolchain dependencies") {
		t.Errorf("expected healing text in view, got: %s", viewHealing)
	}
}

func TestDoctorStageWizardIntegration(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	// Set stage to Doctor
	root.SetStage(session.StageDoctor)

	view := root.View()
	if !strings.Contains(view, "Pre-Flight System Diagnostics") {
		t.Errorf("expected Doctor stage view in WizardModel, got: %s", view)
	}
}

