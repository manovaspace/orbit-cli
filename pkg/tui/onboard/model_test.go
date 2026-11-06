package onboard_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	onboarddomain "github.com/manovaspace/orbit-cli/pkg/onboard/domain"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/sessionstore"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestWizardModelInitAndResize(t *testing.T) {
	sm, err := sessionstore.New(t.TempDir() + "/session.json")
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

	view := m.View().Content
	if len(view) == 0 {
		t.Errorf("expected non-empty view output")
	}
	if !strings.Contains(view, "Orbit") && !strings.Contains(view, "Welcome") {
		t.Errorf("expected view to contain header/stepper info, got: %s", view)
	}
}

func TestWizardModelStageTransition(t *testing.T) {
	sm, err := sessionstore.New(t.TempDir() + "/session.json")
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	model.SetStage(onboarddomain.StageDoctor)
	if model.ActiveStage() != onboarddomain.StageDoctor {
		t.Errorf("expected active stage %v, got %v", onboarddomain.StageDoctor, model.ActiveStage())
	}

	// Verify checkpoint was persisted
	loaded, err := sm.Load()
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}
	if loaded == nil || loaded.CurrentStage != onboarddomain.StageDoctor {
		t.Errorf("expected saved session stage %v, got %v", onboarddomain.StageDoctor, loaded)
	}
}

func TestWizardModelTerminalResizeConstraints(t *testing.T) {
	sm, _ := sessionstore.New(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	// Small terminal resize (<80x24)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	m := updated.(*tuiOnboard.WizardModel)

	view := m.View().Content
	if !strings.Contains(view, "too small") && !strings.Contains(view, "60x15") && !strings.Contains(view, "80x24") {
		t.Errorf("expected terminal too small warning, got: %s", view)
	}

	// Normal terminal resize (120x35)
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	m = updated.(*tuiOnboard.WizardModel)

	view = m.View().Content
	if strings.Contains(view, "too small") {
		t.Errorf("expected normal view without too small warning on 120x35")
	}
}

func TestWizardModelCtrlCQuit(t *testing.T) {
	sm, _ := sessionstore.New(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	_, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Errorf("expected non-nil tea.Quit command on Ctrl+C")
	}
}

func TestWizardModelResumeAndReset(t *testing.T) {
	sessionPath := t.TempDir() + "/session.json"
	sm, _ := sessionstore.New(sessionPath)

	// Save initial session at StageWorkspace
	state := sm.Create("test@manova.space", "Test User")
	state.CurrentStage = onboarddomain.StageWorkspace
	if err := sm.Save(state); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	// 1. Resume should restore StageWorkspace
	resumeModel := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		Resume:         true,
	})
	if resumeModel.ActiveStage() != onboarddomain.StageWorkspace {
		t.Errorf("expected resumed stage %v, got %v", onboarddomain.StageWorkspace, resumeModel.ActiveStage())
	}

	// 2. Reset should wipe session and start at StageWelcome
	resetModel := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		Reset:          true,
	})
	if resetModel.ActiveStage() != onboarddomain.StageWelcome {
		t.Errorf("expected reset stage %v, got %v", onboarddomain.StageWelcome, resetModel.ActiveStage())
	}
}

func TestHeaderStepperRendering(t *testing.T) {
	stages := []onboarddomain.Stage{
		onboarddomain.StageWelcome,
		onboarddomain.StageDoctor,
		onboarddomain.StageIdentity,
		onboarddomain.StageWorkspace,
		onboarddomain.StageEnvironment,
		onboarddomain.StageStack,
		onboarddomain.StageCompleted,
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
	footer := tuiOnboard.RenderFooterStatusBar(onboarddomain.StageWelcome, 100, "[Space] Select")
	if len(footer) == 0 {
		t.Errorf("expected non-empty footer status bar")
	}
	if !strings.Contains(footer, "Enter") || !strings.Contains(footer, "Ctrl+C") {
		t.Errorf("expected footer to show key hints, got: %s", footer)
	}
}

func TestStepIndexForStage(t *testing.T) {
	tests := []struct {
		stage onboarddomain.Stage
		want  int
	}{
		{onboarddomain.StageInit, 0},
		{onboarddomain.StageWelcome, 0},
		{onboarddomain.StageDoctor, 1},
		{onboarddomain.StageDoctorPassed, 1},
		{onboarddomain.StageIdentity, 2},
		{onboarddomain.StageKeypairReady, 2},
		{onboarddomain.StageTokenClaimed, 2},
		{onboarddomain.StageNetworkConfigured, 2},
		{onboarddomain.StageWorkspace, 3},
		{onboarddomain.StageReposCloned, 3},
		{onboarddomain.StageEnvironment, 4},
		{onboarddomain.StageEnvironmentReady, 4},
		{onboarddomain.StageMCPConfigured, 4},
		{onboarddomain.StageStack, 5},
		{onboarddomain.StageStackReady, 5},
		{onboarddomain.StageDevStackReady, 5},
		{onboarddomain.StageComplete, 6},
		{onboarddomain.StageCompleted, 6},
		{onboarddomain.Stage("unknown_stage"), 0},
	}

	for _, tc := range tests {
		got := tuiOnboard.StepIndexForStage(tc.stage)
		if got != tc.want {
			t.Errorf("StepIndexForStage(%s) = %d; want %d", tc.stage, got, tc.want)
		}
	}
}

func TestWizardModelErrorAndLoading(t *testing.T) {
	sm, _ := sessionstore.New(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	model.SetError("something went wrong")
	if model.ErrorMsg != "something went wrong" {
		t.Errorf("expected error message to be set")
	}
	view := model.View().Content
	if !strings.Contains(view, "something went wrong") {
		t.Errorf("expected error in view, got: %s", view)
	}

	model.SetLoading(true, "Downloading assets...")
	if !model.IsLoading || model.LoadingMsg != "Downloading assets..." {
		t.Errorf("expected loading state to be active")
	}
	view = model.View().Content
	if !strings.Contains(view, "Downloading assets...") {
		t.Errorf("expected loading msg in view, got: %s", view)
	}
}

func TestWizardModelCustomViewsAndUpdates(t *testing.T) {
	sm, _ := sessionstore.New(t.TempDir() + "/session.json")
	model := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	model.StageViews[onboarddomain.StageWelcome] = func(m *tuiOnboard.WizardModel) string {
		return "CUSTOM_WELCOME_VIEW"
	}

	customUpdateCalled := false
	model.StageUpdates[onboarddomain.StageWelcome] = func(m *tuiOnboard.WizardModel, msg tea.Msg) (tea.Model, tea.Cmd) {
		customUpdateCalled = true
		return m, nil
	}

	view := model.View().Content
	if !strings.Contains(view, "CUSTOM_WELCOME_VIEW") {
		t.Errorf("expected custom welcome view in output, got: %s", view)
	}

	model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
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
	if model.ActiveStage() != onboarddomain.StageWelcome {
		t.Errorf("expected default active stage to be StageWelcome, got: %s", model.ActiveStage())
	}
}
