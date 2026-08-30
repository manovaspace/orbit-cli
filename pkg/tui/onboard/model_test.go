package onboard_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestWizardModelInitAndResize(t *testing.T) {
	sm, err := session.NewSessionManager(t.TempDir() + "/session.json")
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	cmd := model.Init()
	if cmd == nil {
		t.Errorf("expected non-nil init command for spinner")
	}

	// Test window resize message
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, ok := updated.(*tuiOnboard.WizardModel)
	if !ok {
		t.Fatalf("expected updated model to be *WizardModel")
	}

	view := m.View()
	if len(view) == 0 {
		t.Errorf("expected non-empty view output")
	}
	if !strings.Contains(view, "Orbit") && !strings.Contains(view, "Welcome") {
		t.Errorf("expected view to contain header/stepper info, got: %s", view)
	}
}

func TestWizardModelStageTransition(t *testing.T) {
	sm, err := session.NewSessionManager(t.TempDir() + "/session.json")
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	model.SetStage(session.StageDoctor)
	if model.ActiveStage() != session.StageDoctor {
		t.Errorf("expected active stage %v, got %v", session.StageDoctor, model.ActiveStage())
	}

	// Verify checkpoint was persisted
	loaded, err := sm.LoadSession()
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}
	if loaded == nil || loaded.CurrentStage != session.StageDoctor {
		t.Errorf("expected saved session stage %v, got %v", session.StageDoctor, loaded)
	}
}

func TestWizardModelTerminalResizeConstraints(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	// Small terminal resize (<80x24)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	m := updated.(*tuiOnboard.WizardModel)

	view := m.View()
	if !strings.Contains(view, "too small") && !strings.Contains(view, "60x15") && !strings.Contains(view, "80x24") {
		t.Errorf("expected terminal too small warning, got: %s", view)
	}

	// Normal terminal resize (120x35)
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	m = updated.(*tuiOnboard.WizardModel)

	view = m.View()
	if strings.Contains(view, "too small") {
		t.Errorf("expected normal view without too small warning on 120x35")
	}
}

func TestWizardModelCtrlCQuit(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Errorf("expected non-nil tea.Quit command on Ctrl+C")
	}
}

func TestWizardModelResumeAndReset(t *testing.T) {
	sessionPath := t.TempDir() + "/session.json"
	sm, _ := session.NewSessionManager(sessionPath)

	// Save initial session at StageWorkspace
	state := sm.CreateSession("test@manova.space", "Test User")
	state.CurrentStage = session.StageWorkspace
	if err := sm.SaveCheckpoint(state); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	// 1. Resume should restore StageWorkspace
	resumeModel := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		Resume:         true,
	})
	if resumeModel.ActiveStage() != session.StageWorkspace {
		t.Errorf("expected resumed stage %v, got %v", session.StageWorkspace, resumeModel.ActiveStage())
	}

	// 2. Reset should wipe session and start at StageWelcome
	resetModel := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		Reset:          true,
	})
	if resetModel.ActiveStage() != session.StageWelcome {
		t.Errorf("expected reset stage %v, got %v", session.StageWelcome, resetModel.ActiveStage())
	}
}

func TestHeaderStepperRendering(t *testing.T) {
	stages := []session.Stage{
		session.StageWelcome,
		session.StageDoctor,
		session.StageIdentity,
		session.StageWorkspace,
		session.StageEnvironment,
		session.StageStack,
		session.StageCompleted,
	}

	for _, st := range stages {
		stepper := tuiOnboard.RenderHeaderStepper(st, 100)
		if len(stepper) == 0 {
			t.Errorf("expected non-empty stepper for stage %s", st)
		}
		if !strings.Contains(stepper, "Welcome") || !strings.Contains(stepper, "Dev Stack") {
			t.Errorf("expected stepper to contain stage names for stage %s, got: %s", st, stepper)
		}
	}
}

func TestFooterStatusBarRendering(t *testing.T) {
	footer := tuiOnboard.RenderFooterStatusBar(session.StageWelcome, 100, "[Space] Select")
	if len(footer) == 0 {
		t.Errorf("expected non-empty footer status bar")
	}
	if !strings.Contains(footer, "Enter") || !strings.Contains(footer, "Ctrl+C") {
		t.Errorf("expected footer to show key hints, got: %s", footer)
	}
}

func TestStepIndexForStage(t *testing.T) {
	tests := []struct {
		stage session.Stage
		want  int
	}{
		{session.StageInit, 0},
		{session.StageWelcome, 0},
		{session.StageDoctor, 1},
		{session.StageDoctorPassed, 1},
		{session.StageIdentity, 2},
		{session.StageKeypairReady, 2},
		{session.StageTokenClaimed, 2},
		{session.StageNetworkConfigured, 2},
		{session.StageWorkspace, 3},
		{session.StageReposCloned, 3},
		{session.StageEnvironment, 4},
		{session.StageEnvironmentReady, 4},
		{session.StageMCPConfigured, 4},
		{session.StageStack, 5},
		{session.StageStackReady, 5},
		{session.StageDevStackReady, 5},
		{session.StageComplete, 6},
		{session.StageCompleted, 6},
		{session.Stage("unknown_stage"), 0},
	}

	for _, tc := range tests {
		got := tuiOnboard.StepIndexForStage(tc.stage)
		if got != tc.want {
			t.Errorf("StepIndexForStage(%s) = %d; want %d", tc.stage, got, tc.want)
		}
	}
}

func TestWizardModelErrorAndLoading(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	model.SetError("something went wrong")
	if model.ErrorMsg != "something went wrong" {
		t.Errorf("expected error message to be set")
	}
	view := model.View()
	if !strings.Contains(view, "something went wrong") {
		t.Errorf("expected error in view, got: %s", view)
	}

	model.SetLoading(true, "Downloading assets...")
	if !model.IsLoading || model.LoadingMsg != "Downloading assets..." {
		t.Errorf("expected loading state to be active")
	}
	view = model.View()
	if !strings.Contains(view, "Downloading assets...") {
		t.Errorf("expected loading msg in view, got: %s", view)
	}
}

func TestWizardModelCustomViewsAndUpdates(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	model.StageViews[session.StageWelcome] = func(m *tuiOnboard.WizardModel) string {
		return "CUSTOM_WELCOME_VIEW"
	}

	customUpdateCalled := false
	model.StageUpdates[session.StageWelcome] = func(m *tuiOnboard.WizardModel, msg tea.Msg) (tea.Model, tea.Cmd) {
		customUpdateCalled = true
		return m, nil
	}

	view := model.View()
	if !strings.Contains(view, "CUSTOM_WELCOME_VIEW") {
		t.Errorf("expected custom welcome view in output, got: %s", view)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if !customUpdateCalled {
		t.Errorf("expected custom stage update handler to be invoked")
	}
}

func TestWizardModelZeroOptions(t *testing.T) {
	// Creating model with zero options should not panic and should assign default session
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{})
	if model == nil {
		t.Fatalf("expected non-nil model from empty options")
	}
	if model.ActiveStage() != session.StageWelcome {
		t.Errorf("expected default active stage to be StageWelcome, got: %s", model.ActiveStage())
	}
}

