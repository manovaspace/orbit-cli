package onboard_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func TestWelcomeStageInputAndValidation(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	welcome := tuiOnboard.NewWelcomeModel(root)
	welcome.Init()

	// Initial view has prompt and ASCII banner
	view := welcome.View()
	if !strings.Contains(view, "Welcome to Manova") && !strings.Contains(view, "Orbit Onboarding") {
		t.Errorf("expected welcome banner in view, got: %s", view)
	}

	// Type token "inv_test123"
	for _, ch := range "inv_test123" {
		welcome.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}

	if welcome.TokenValue() != "inv_test123" {
		t.Errorf("expected token 'inv_test123', got '%s'", welcome.TokenValue())
	}
}

func TestWelcomeStageExistingSessionPrompt(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	sess := sm.CreateSession("test@manova.space", "Test Engineer")
	sess.CurrentStage = session.StageWorkspace
	_ = sm.SaveCheckpoint(sess)

	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	welcome := tuiOnboard.NewWelcomeModel(root)
	welcome.Init()

	view := welcome.View()
	if !strings.Contains(view, "Resume incomplete session") && !strings.Contains(view, "Saved checkpoint found") {
		t.Errorf("expected resume prompt when pending session exists, got: %s", view)
	}
}

func TestWelcomeStageEnterWithEmptyToken(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	welcome := tuiOnboard.NewWelcomeModel(root)
	welcome.Init()

	// Press Enter without entering token
	welcome.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if root.ActiveStage() != session.StageWelcome {
		t.Errorf("expected stage to remain StageWelcome on empty token, got: %v", root.ActiveStage())
	}
	if root.ErrorMsg == "" {
		t.Errorf("expected error message when submitting empty token")
	}
}

func TestWelcomeStageEnterWithValidToken(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	welcome := tuiOnboard.NewWelcomeModel(root)
	welcome.Init()
	welcome.SetTokenValue("inv_sec_valid_token_999")

	// Submit token
	welcome.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if root.ActiveStage() != session.StageDoctor {
		t.Errorf("expected transition to StageDoctor on valid token, got: %v", root.ActiveStage())
	}
	if root.Session.InviteToken != "inv_sec_valid_token_999" {
		t.Errorf("expected session InviteToken to be 'inv_sec_valid_token_999', got: '%s'", root.Session.InviteToken)
	}
}

func TestWelcomeStageResumeAction(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	sess := sm.CreateSession("resume@manova.space", "Resuming User")
	sess.CurrentStage = session.StageWorkspace
	_ = sm.SaveCheckpoint(sess)

	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	welcome := tuiOnboard.NewWelcomeModel(root)
	welcome.Init()

	// Press 'r' to resume
	welcome.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	if root.ActiveStage() != session.StageWorkspace {
		t.Errorf("expected active stage to be StageWorkspace after resume, got: %v", root.ActiveStage())
	}
}

func TestWelcomeStageDiscardAction(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	sess := sm.CreateSession("discard@manova.space", "Discarding User")
	sess.CurrentStage = session.StageWorkspace
	_ = sm.SaveCheckpoint(sess)

	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	welcome := tuiOnboard.NewWelcomeModel(root)
	welcome.Init()

	// Press 'd' to discard
	welcome.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	// View should now display token entry field
	view := welcome.View()
	if strings.Contains(view, "Saved checkpoint found") {
		t.Errorf("expected resume prompt to disappear after discard, got: %s", view)
	}
	if !strings.Contains(view, "Enter Invitation Token") && !strings.Contains(view, "inv_") {
		t.Errorf("expected token input in view after discard, got: %s", view)
	}
}

func TestWelcomeStagePreSetToken(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		PreSetToken:    "orb_preset_tok_456",
	})

	welcome := tuiOnboard.NewWelcomeModel(root)
	welcome.Init()

	if welcome.TokenValue() != "orb_preset_tok_456" {
		t.Errorf("expected preset token 'orb_preset_tok_456', got '%s'", welcome.TokenValue())
	}
}
