package onboard_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
	"golang.org/x/crypto/ssh"
)

func TestIdentitySSHKeyGeneration(t *testing.T) {
	tmpSSH := t.TempDir()
	keyPath := filepath.Join(tmpSSH, "subdir", "id_ed25519_orbit")

	// 1. Generate new keypair
	pubKey, err := tuiOnboard.EnsureSSHKeypair(keyPath)
	if err != nil {
		t.Fatalf("failed to generate SSH key: %v", err)
	}

	if len(pubKey) == 0 {
		t.Fatalf("expected non-empty public key string")
	}

	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("expected public key to start with 'ssh-ed25519 ', got: %s", pubKey)
	}

	// Verify private key file permissions
	fi, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("private key file does not exist: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 private key permissions, got %v", fi.Mode().Perm())
	}

	// Verify public key file permissions
	pubFi, err := os.Stat(keyPath + ".pub")
	if err != nil {
		t.Fatalf("public key file does not exist: %v", err)
	}
	if pubFi.Mode().Perm() != 0644 {
		t.Errorf("expected 0644 public key permissions, got %v", pubFi.Mode().Perm())
	}

	// Verify SSH key can be parsed
	parsedPub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubKey))
	if err != nil {
		t.Fatalf("failed to parse generated public key: %v", err)
	}
	if parsedPub.Type() != "ssh-ed25519" {
		t.Errorf("expected parsed key type 'ssh-ed25519', got %s", parsedPub.Type())
	}

	// 2. Call EnsureSSHKeypair again to ensure it reloads the existing key without regenerating
	secondPubKey, err := tuiOnboard.EnsureSSHKeypair(keyPath)
	if err != nil {
		t.Fatalf("failed to load existing SSH key: %v", err)
	}
	if strings.TrimSpace(secondPubKey) != strings.TrimSpace(pubKey) {
		t.Errorf("expected reused public key to match, got %s vs %s", secondPubKey, pubKey)
	}
}

func TestIdentityConfigureSSHHost(t *testing.T) {
	tmpSSH := t.TempDir()
	configPath := filepath.Join(tmpSSH, "config")
	keyPath := filepath.Join(tmpSSH, "id_ed25519_orbit")

	// 1. Create config from scratch
	err := tuiOnboard.ConfigureSSHHost(configPath, "git.dev.manova.space", "git.dev.manova.space", "git", keyPath)
	if err != nil {
		t.Fatalf("failed to configure SSH host: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read ssh config: %v", err)
	}

	cfgStr := string(content)
	expectedDirectives := []string{
		"Host git.dev.manova.space",
		"HostName git.dev.manova.space",
		"User git",
		"IdentityFile " + keyPath,
		"IdentitiesOnly yes",
		"StrictHostKeyChecking accept-new",
	}
	for _, dir := range expectedDirectives {
		if !strings.Contains(cfgStr, dir) {
			t.Errorf("expected %q in ssh config, got:\n%s", dir, cfgStr)
		}
	}

	// Verify config file permissions are 0600
	fi, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat failed on ssh config: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions on ssh config, got %v", fi.Mode().Perm())
	}

	// 2. Update existing host block without corrupting other hosts
	preExisting := "Host other.server\n    HostName other.example.com\n    User admin\n\n"
	err = os.WriteFile(configPath, []byte(preExisting+cfgStr), 0600)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	err = tuiOnboard.ConfigureSSHHost(configPath, "git.dev.manova.space", "custom.manova.space", "gituser", "/custom/key")
	if err != nil {
		t.Fatalf("failed to update SSH host: %v", err)
	}

	updatedContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated ssh config: %v", err)
	}
	updatedStr := string(updatedContent)

	if !strings.Contains(updatedStr, "Host other.server") {
		t.Errorf("expected 'Host other.server' to be preserved, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "HostName custom.manova.space") {
		t.Errorf("expected updated HostName in config, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "User gituser") {
		t.Errorf("expected updated User in config, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "IdentityFile /custom/key") {
		t.Errorf("expected updated IdentityFile in config, got:\n%s", updatedStr)
	}
}

func TestIdentityModelViewRendering(t *testing.T) {
	tmpDir := t.TempDir()
	sm, _ := session.NewSessionManager(tmpDir + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		PreSetToken:    "inv_dev_secret_token_12345",
	})
	root.Session.DisplayName = "Grace Hopper"
	root.Session.UID = "ghopper"

	idModel := tuiOnboard.NewIdentityModel(root)
	idModel.SetSSHKeyPath(filepath.Join(tmpDir, "id_ed25519_test"))

	view := idModel.View()

	// Header check
	if !strings.Contains(view, "Developer Identity & Platform Registration") {
		t.Errorf("expected header in view, got:\n%s", view)
	}

	// Masked token preview check
	if !strings.Contains(view, "inv_dev_") {
		t.Errorf("expected masked token preview in view, got:\n%s", view)
	}

	// Form inputs check
	if !strings.Contains(view, "Grace Hopper") {
		t.Errorf("expected Display Name value in view, got:\n%s", view)
	}
	if !strings.Contains(view, "ghopper") {
		t.Errorf("expected UID value in view, got:\n%s", view)
	}

	// Action prompt check
	if !strings.Contains(view, "Submit Claim") {
		t.Errorf("expected submit action prompt in view, got:\n%s", view)
	}
}

func TestIdentityModelTabNavigation(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	idModel := tuiOnboard.NewIdentityModel(root)

	// Initially Display Name is focused (index 0)
	if idModel.FocusedIndex() != 0 {
		t.Errorf("expected initial focus index 0, got %d", idModel.FocusedIndex())
	}

	// Press Tab -> switches focus to Desired UID (index 1)
	idModel.Update(tea.KeyMsg{Type: tea.KeyTab})
	if idModel.FocusedIndex() != 1 {
		t.Errorf("expected focus index 1 after Tab, got %d", idModel.FocusedIndex())
	}

	// Press Tab again -> switches focus to Server URL or cycles back to 0
	idModel.Update(tea.KeyMsg{Type: tea.KeyTab})
	idModel.Update(tea.KeyMsg{Type: tea.KeyTab})
	if idModel.FocusedIndex() != 0 && idModel.FocusedIndex() != 1 && idModel.FocusedIndex() != 2 {
		t.Errorf("unexpected focus index %d", idModel.FocusedIndex())
	}

	// Press Shift+Tab -> moves backwards
	idModel.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
}

func TestIdentityModelClaimSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	sm, _ := session.NewSessionManager(tmpDir + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		PreSetToken:    "inv_dev_valid_token",
	})
	root.SetStage(session.StageIdentity)

	idModel := tuiOnboard.NewIdentityModel(root)
	keyPath := filepath.Join(tmpDir, "id_ed25519_orbit")
	idModel.SetSSHKeyPath(keyPath)
	idModel.SetDisplayName("Ada Lovelace")
	idModel.SetDesiredUID("alovelace")

	claimCalled := false
	idModel.SetClaimFunc(func(ctx context.Context, serverURL string, req client.ClaimRequest) (*client.ClaimResponse, error) {
		claimCalled = true
		if req.InviteToken != "inv_dev_valid_token" {
			t.Errorf("expected InviteToken 'inv_dev_valid_token', got %q", req.InviteToken)
		}
		if req.DesiredUID != "alovelace" {
			t.Errorf("expected DesiredUID 'alovelace', got %q", req.DesiredUID)
		}
		if req.DisplayName != "Ada Lovelace" {
			t.Errorf("expected DisplayName 'Ada Lovelace', got %q", req.DisplayName)
		}
		if len(req.SSHPublicKey) == 0 {
			t.Errorf("expected non-empty SSHPublicKey in claim request")
		}

		return &client.ClaimResponse{
			Status: "ok",
			User: client.User{
				UID:         "alovelace",
				Email:       "ada@manova.space",
				DisplayName: "Ada Lovelace",
				Groups:      []string{"engineering"},
			},
			Credentials: client.Credentials{
				ForgejoUsername: "alovelace",
				ForgejoMCPToken: "mcp_token_xyz123",
				WireGuardConfig: "[Interface]\nPrivateKey = privkey\nAddress = 10.0.0.2/32\n",
			},
			Workspace: client.WorkspaceInfo{
				GitRemoteBase:        "git@git.dev.manova.space:",
				DefaultManifestScope: "core",
			},
		}, nil
	})

	// Trigger claim via RunClaim()
	cmd := idModel.RunClaim()
	if cmd == nil {
		t.Fatalf("expected non-nil tea.Cmd from RunClaim")
	}

	msg := cmd()
	claimMsg, ok := msg.(tuiOnboard.ClaimFinishedMsg)
	if !ok {
		t.Fatalf("expected ClaimFinishedMsg, got %T", msg)
	}
	if claimMsg.Err != nil {
		t.Fatalf("unexpected claim error: %v", claimMsg.Err)
	}
	if !claimCalled {
		t.Errorf("expected claim function to be called")
	}

	// Pass ClaimFinishedMsg to Update
	idModel.Update(claimMsg)

	// Verify session was populated with credentials and user details
	if root.Session.UID != "alovelace" {
		t.Errorf("expected session UID 'alovelace', got %q", root.Session.UID)
	}
	if root.Session.Email != "ada@manova.space" {
		t.Errorf("expected session Email 'ada@manova.space', got %q", root.Session.Email)
	}
	if root.Session.DisplayName != "Ada Lovelace" {
		t.Errorf("expected session DisplayName 'Ada Lovelace', got %q", root.Session.DisplayName)
	}
	if root.Session.ForgejoToken != "mcp_token_xyz123" {
		t.Errorf("expected session ForgejoToken 'mcp_token_xyz123', got %q", root.Session.ForgejoToken)
	}
	if root.Session.WireGuardConfig == "" {
		t.Errorf("expected non-empty WireGuardConfig in session")
	}
	if root.Session.Metadata["git_remote_base"] != "git@git.dev.manova.space:" {
		t.Errorf("expected git_remote_base in session metadata, got %v", root.Session.Metadata)
	}

	// Verify wizard transitioned to StageWorkspace
	if root.ActiveStage() != session.StageWorkspace {
		t.Errorf("expected transition to StageWorkspace, got %v", root.ActiveStage())
	}
}

func TestIdentityModelClaimFailureAndRetry(t *testing.T) {
	tmpDir := t.TempDir()
	sm, _ := session.NewSessionManager(tmpDir + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		PreSetToken:    "inv_invalid_token",
	})
	root.SetStage(session.StageIdentity)

	idModel := tuiOnboard.NewIdentityModel(root)
	idModel.SetSSHKeyPath(filepath.Join(tmpDir, "id_ed25519_orbit"))

	idModel.SetClaimFunc(func(ctx context.Context, serverURL string, req client.ClaimRequest) (*client.ClaimResponse, error) {
		return nil, &client.APIError{StatusCode: 401, Message: "invitation token has expired or is invalid"}
	})

	// Dispatch claim
	cmd := idModel.RunClaim()
	msg := cmd()
	idModel.Update(msg)

	if idModel.IsSubmitting() {
		t.Errorf("expected model not to be in submitting state after error")
	}

	if root.ErrorMsg == "" {
		t.Errorf("expected root ErrorMsg to be set on claim failure")
	}

	view := idModel.View()
	if !strings.Contains(view, "invitation token has expired") && !strings.Contains(root.ErrorMsg, "invitation token has expired") {
		t.Errorf("expected error message in view or root error banner, view: %s, root.ErrorMsg: %s", view, root.ErrorMsg)
	}

	// Press 'r' to retry
	_, retryCmd := idModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if retryCmd == nil {
		t.Errorf("expected non-nil tea.Cmd on 'r' retry")
	}
}

func TestIdentityModelSubmittingView(t *testing.T) {
	tmpDir := t.TempDir()
	sm, _ := session.NewSessionManager(tmpDir + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	idModel := tuiOnboard.NewIdentityModel(root)
	idModel.SetSSHKeyPath(filepath.Join(tmpDir, "id_ed25519_orbit"))

	idModel.SetSubmitting(true)
	view := idModel.View()

	if !strings.Contains(view, "Submitting cryptographic claim to Orbit Server") {
		t.Errorf("expected submitting message in view, got:\n%s", view)
	}
}

func TestIdentityModelWizardIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	sm, _ := session.NewSessionManager(tmpDir + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
		PreSetToken:    "inv_dev_test",
	})

	root.SetStage(session.StageIdentity)
	view := root.View()

	if !strings.Contains(view, "Developer Identity & Platform Registration") {
		t.Errorf("expected Identity stage view rendered in WizardModel, got:\n%s", view)
	}
}
