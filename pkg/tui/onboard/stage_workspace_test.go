package onboard_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/manovaspace/orbit-cli/pkg/session"
	tuiOnboard "github.com/manovaspace/orbit-cli/pkg/tui/onboard"
)

func sampleManifest() *manifest.WorkspaceManifest {
	return &manifest.WorkspaceManifest{
		Version:   "1.0",
		Workspace: "manova",
		Remotes: manifest.RemotesConfig{
			"forgejo": "git@git.dev.manova.space:",
		},
		Groups: map[string]manifest.GroupConfig{
			"orbit": {
				Path: "orbit",
				Repositories: []manifest.RepoConfig{
					{Name: "orbit-infra", Path: "orbit/orbit-infra", Required: true},
					{Name: "orbit-frontend", Path: "orbit/orbit-frontend", Required: true},
					{Name: "orbit-cli", Path: "orbit/orbit-cli", Required: false},
				},
			},
			"manovaspace": {
				Path: "manovaspace",
				Repositories: []manifest.RepoConfig{
					{Name: "ts", Path: "manovaspace/ts", Required: false},
					{Name: "design-system", Path: "manovaspace/design-system", Required: false},
				},
			},
			"clients": {
				Path: "clients",
				Clients: map[string]manifest.ClientConfig{
					"fryto": {
						Path: "clients/fryto",
						Repositories: []manifest.RepoConfig{
							{Name: "fryto-app", Path: "clients/fryto/fryto-app", Required: false},
						},
					},
				},
			},
		},
	}
}

func TestBuildRepoTreeItemsWithManifest(t *testing.T) {
	m := sampleManifest()

	// Default scope "core"
	items := tuiOnboard.BuildRepoTreeItems(m, "core")
	if len(items) != 6 {
		t.Fatalf("expected 6 total repository items, got %d", len(items))
	}

	for _, item := range items {
		if item.Required && !item.Selected {
			t.Errorf("expected required repo %q to be selected by default", item.Name)
		}
		if !item.Required && item.Selected {
			t.Errorf("expected optional repo %q to not be selected under 'core' scope", item.Name)
		}
	}

	// Scope "all"
	allItems := tuiOnboard.BuildRepoTreeItems(m, "all")
	for _, item := range allItems {
		if !item.Selected {
			t.Errorf("expected all repo %q to be selected under 'all' scope", item.Name)
		}
	}

	// Specific scope "orbit"
	orbitItems := tuiOnboard.BuildRepoTreeItems(m, "orbit")
	for _, item := range orbitItems {
		if item.Scope == "orbit" && !item.Selected {
			t.Errorf("expected orbit repo %q to be selected under 'orbit' scope", item.Name)
		}
		if item.Scope != "orbit" && !item.Required && item.Selected {
			t.Errorf("expected non-orbit repo %q to be unselected", item.Name)
		}
	}
}

func TestBuildRepoTreeItemsWithNilManifestDefaults(t *testing.T) {
	items := tuiOnboard.BuildRepoTreeItems(nil, "core")
	if len(items) == 0 {
		t.Fatalf("expected default repo tree items when manifest is nil")
	}

	hasRequired := false
	for _, item := range items {
		if item.Required {
			hasRequired = true
			if !item.Selected {
				t.Errorf("expected required default item %q to be selected", item.Name)
			}
		}
	}
	if !hasRequired {
		t.Errorf("expected at least one required default repository")
	}
}

func TestWorkspaceModelKeyboardNavigation(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	wsModel := tuiOnboard.NewWorkspaceModel(root)
	items := tuiOnboard.BuildRepoTreeItems(sampleManifest(), "core")
	wsModel.SetItems(items)

	if wsModel.Cursor() != 0 {
		t.Errorf("expected initial cursor 0, got %d", wsModel.Cursor())
	}

	// Down arrow moves cursor to 1
	wsModel.Update(tea.KeyMsg{Type: tea.KeyDown})
	if wsModel.Cursor() != 1 {
		t.Errorf("expected cursor 1 after KeyDown, got %d", wsModel.Cursor())
	}

	// 'j' moves cursor to 2
	wsModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if wsModel.Cursor() != 2 {
		t.Errorf("expected cursor 2 after 'j', got %d", wsModel.Cursor())
	}

	// Up arrow moves cursor back to 1
	wsModel.Update(tea.KeyMsg{Type: tea.KeyUp})
	if wsModel.Cursor() != 1 {
		t.Errorf("expected cursor 1 after KeyUp, got %d", wsModel.Cursor())
	}

	// 'k' moves cursor back to 0
	wsModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if wsModel.Cursor() != 0 {
		t.Errorf("expected cursor 0 after 'k', got %d", wsModel.Cursor())
	}

	// 'k' at 0 clamps to 0
	wsModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if wsModel.Cursor() != 0 {
		t.Errorf("expected cursor clamped at 0, got %d", wsModel.Cursor())
	}
}

func TestWorkspaceModelSelectionToggles(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	wsModel := tuiOnboard.NewWorkspaceModel(root)
	items := tuiOnboard.BuildRepoTreeItems(sampleManifest(), "core")
	wsModel.SetItems(items)

	// Find a required item
	requiredIdx := -1
	for i, it := range items {
		if it.Required {
			requiredIdx = i
			break
		}
	}
	if requiredIdx == -1 {
		t.Fatalf("expected at least one required item")
	}

	// Pressing [Space] on required item should keep it selected
	wsModel.SetCursor(requiredIdx)
	wsModel.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !wsModel.Items()[requiredIdx].Selected {
		t.Errorf("expected required item %d (%s) to remain selected after Space", requiredIdx, wsModel.Items()[requiredIdx].Name)
	}

	// Find an optional unselected item
	optionalIdx := -1
	for i, it := range wsModel.Items() {
		if !it.Required && !it.Selected {
			optionalIdx = i
			break
		}
	}
	if optionalIdx == -1 {
		t.Fatalf("expected at least one unselected optional item")
	}

	// Toggle optional item on with [Space]
	wsModel.SetCursor(optionalIdx)
	wsModel.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !wsModel.Items()[optionalIdx].Selected {
		t.Errorf("expected optional item %d to become selected after Space", optionalIdx)
	}

	// Toggle optional item off with [Space]
	wsModel.Update(tea.KeyMsg{Type: tea.KeySpace})
	if wsModel.Items()[optionalIdx].Selected {
		t.Errorf("expected optional item %d to become unselected after second Space", optionalIdx)
	}

	// Toggle All via [A] / [a]
	// First press -> selects all optional items
	wsModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for i, it := range wsModel.Items() {
		if !it.Selected {
			t.Errorf("expected item %d (%s) to be selected after ToggleAll", i, it.Name)
		}
	}

	// Second press -> deselects all optional items (required items remain selected)
	wsModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for i, it := range wsModel.Items() {
		if it.Required && !it.Selected {
			t.Errorf("expected required item %d to remain selected after ToggleAll", i)
		}
		if !it.Required && it.Selected {
			t.Errorf("expected optional item %d to be unselected after second ToggleAll", i)
		}
	}
}

func TestWorkspaceModelParallelCloningSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	sm, _ := session.NewSessionManager(tmpDir + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})
	root.SetStage(session.StageWorkspace)

	wsModel := tuiOnboard.NewWorkspaceModel(root)
	wsModel.SetWorkspaceRoot(tmpDir)

	items := []tuiOnboard.RepoTreeItem{
		{Name: "orbit-infra", Path: "orbit/orbit-infra", Scope: "orbit", Required: true, Selected: true, Status: "pending"},
		{Name: "orbit-frontend", Path: "orbit/orbit-frontend", Scope: "orbit", Required: true, Selected: true, Status: "pending"},
		{Name: "ts", Path: "manovaspace/ts", Scope: "manovaspace", Required: false, Selected: true, Status: "pending"},
	}
	wsModel.SetItems(items)

	runnerCalled := false
	wsModel.SetClonerRunner(func(workspaceRoot string, targets []manifest.RepoTarget, concurrency int, callback func(orchestrator.CloneResult)) []orchestrator.CloneResult {
		runnerCalled = true
		if len(targets) != 3 {
			t.Errorf("expected 3 targets to clone, got %d", len(targets))
		}
		if workspaceRoot != tmpDir {
			t.Errorf("expected workspace root %q, got %q", tmpDir, workspaceRoot)
		}

		var results []orchestrator.CloneResult
		for _, target := range targets {
			res := orchestrator.CloneResult{
				Name:    target.Name,
				Path:    target.Path,
				Success: true,
			}
			if target.Name == "orbit-infra" {
				res.AlreadyExists = true
			}
			if callback != nil {
				callback(res)
			}
			results = append(results, res)
		}
		return results
	})

	// Dispatch cloner via RunCloner
	cmd := wsModel.RunCloner()
	if cmd == nil {
		t.Fatalf("expected non-nil tea.Cmd from RunCloner")
	}

	if !wsModel.IsCloning() {
		t.Errorf("expected model to be in cloning state after RunCloner")
	}

	// Read stream until clone is complete
	currentCmd := cmd
	for currentCmd != nil {
		msg := currentCmd()
		if msg == nil {
			break
		}
		_, nextCmd := wsModel.Update(msg)
		currentCmd = nextCmd
	}

	if !runnerCalled {
		t.Errorf("expected cloner runner to be called")
	}

	// Verify all items are updated
	for _, it := range wsModel.Items() {
		if it.Status != "cloned" && it.Status != "exists" {
			t.Errorf("expected item %s status to be cloned/exists, got %s", it.Name, it.Status)
		}
	}

	// Verify session was updated with cloned repos and workspace path
	if root.Session.WorkspacePath != tmpDir {
		t.Errorf("expected session WorkspacePath %q, got %q", tmpDir, root.Session.WorkspacePath)
	}
	if len(root.Session.ClonedRepos) == 0 {
		t.Errorf("expected non-empty ClonedRepos in session, got %v", root.Session.ClonedRepos)
	}

	// Verify wizard transitioned to StageEnvironment
	if root.ActiveStage() != session.StageEnvironment {
		t.Errorf("expected transition to StageEnvironment, got %v", root.ActiveStage())
	}
}

func TestWorkspaceModelCloningFailureAndRetry(t *testing.T) {
	tmpDir := t.TempDir()
	sm, _ := session.NewSessionManager(tmpDir + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})
	root.SetStage(session.StageWorkspace)

	wsModel := tuiOnboard.NewWorkspaceModel(root)
	wsModel.SetWorkspaceRoot(tmpDir)

	items := []tuiOnboard.RepoTreeItem{
		{Name: "orbit-infra", Path: "orbit/orbit-infra", Scope: "orbit", Required: true, Selected: true, Status: "pending"},
		{Name: "orbit-frontend", Path: "orbit/orbit-frontend", Scope: "orbit", Required: true, Selected: true, Status: "pending"},
	}
	wsModel.SetItems(items)

	attempt := 0
	wsModel.SetClonerRunner(func(workspaceRoot string, targets []manifest.RepoTarget, concurrency int, callback func(orchestrator.CloneResult)) []orchestrator.CloneResult {
		attempt++
		var results []orchestrator.CloneResult
		for _, target := range targets {
			res := orchestrator.CloneResult{
				Name: target.Name,
				Path: target.Path,
			}
			if attempt == 1 && target.Name == "orbit-frontend" {
				res.Success = false
				res.Error = "connection timeout to git remote"
			} else {
				res.Success = true
			}
			if callback != nil {
				callback(res)
			}
			results = append(results, res)
		}
		return results
	})

	// Attempt 1: Trigger clone
	cmd := wsModel.RunCloner()
	currentCmd := cmd
	for currentCmd != nil {
		msg := currentCmd()
		if msg == nil {
			break
		}
		_, nextCmd := wsModel.Update(msg)
		currentCmd = nextCmd
	}

	if !wsModel.HasError() {
		t.Errorf("expected workspace model to be in error state after failed clone")
	}
	if wsModel.IsCloning() {
		t.Errorf("expected model not to be in cloning state after error")
	}

	view := wsModel.View()
	if !strings.Contains(view, "connection timeout") && !strings.Contains(root.ErrorMsg, "connection timeout") {
		t.Errorf("expected failure error in view or root error banner, view: %s", view)
	}

	// Attempt 2: Press 'r' / 'R' to retry
	_, retryCmd := wsModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if retryCmd == nil {
		t.Fatalf("expected non-nil tea.Cmd on 'r' retry")
	}

	currentCmd = retryCmd
	for currentCmd != nil {
		msg := currentCmd()
		if msg == nil {
			break
		}
		_, nextCmd := wsModel.Update(msg)
		currentCmd = nextCmd
	}

	if wsModel.HasError() {
		t.Errorf("expected workspace model error to be cleared after successful retry")
	}
	if root.ActiveStage() != session.StageEnvironment {
		t.Errorf("expected transition to StageEnvironment after retry success, got %v", root.ActiveStage())
	}
}

func TestWorkspaceModelViewRendering(t *testing.T) {
	sm, _ := session.NewSessionManager(t.TempDir() + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	wsModel := tuiOnboard.NewWorkspaceModel(root)
	items := tuiOnboard.BuildRepoTreeItems(sampleManifest(), "core")
	wsModel.SetItems(items)

	// 1. Selection View
	view := wsModel.View()
	if !strings.Contains(view, "Workspace") && !strings.Contains(view, "Repositories") {
		t.Errorf("expected workspace title in view, got:\n%s", view)
	}
	if !strings.Contains(view, "orbit-infra") {
		t.Errorf("expected 'orbit-infra' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Start Clone") && !strings.Contains(view, "Clone") {
		t.Errorf("expected clone action button in view, got:\n%s", view)
	}

	// 2. Cloning Progress View
	wsModel.SetCloning(true)
	items[0].Status = "cloned"
	items[1].Status = "cloning"
	wsModel.SetItems(items)

	cloningView := wsModel.View()
	if !strings.Contains(cloningView, "Cloning") {
		t.Errorf("expected 'Cloning' in cloning view, got:\n%s", cloningView)
	}
}

func TestWorkspaceModelWizardIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	sm, _ := session.NewSessionManager(tmpDir + "/session.json")
	root := tuiOnboard.NewWizardModel(tuiOnboard.WizardOptions{
		SessionManager: sm,
	})

	root.SetStage(session.StageWorkspace)
	view := root.View()

	if !strings.Contains(view, "Workspace") && !strings.Contains(view, "Repositories") {
		t.Errorf("expected Workspace stage view rendered in WizardModel, got:\n%s", view)
	}
}
