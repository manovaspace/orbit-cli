package onboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/orchestrator"
	"github.com/manovaspace/orbit-cli/pkg/session"
)

// DefaultCloneConcurrency is the default number of parallel cloner workers.
const DefaultCloneConcurrency = 4

// RepoTreeItem represents an individual repository node in the workspace selection tree.
type RepoTreeItem struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Scope    string  `json:"scope"`
	Required bool    `json:"required"`
	Selected bool    `json:"selected"`
	Status   string  `json:"status"` // "pending", "cloning", "cloned", "exists", "failed"
	Error    string  `json:"error,omitempty"`
	Progress float64 `json:"progress"`
}

// CloneRunnerFunc defines the functional signature for executing parallel repository clones.
type CloneRunnerFunc func(workspaceRoot string, targets []manifest.RepoTarget, concurrency int, callback func(orchestrator.CloneResult)) []orchestrator.CloneResult

// CloneProgressMsg is dispatched when an individual repository completes cloning.
type CloneProgressMsg struct {
	Result orchestrator.CloneResult
	ch     <-chan orchestrator.CloneResult
}

// CloneFinishedMsg is dispatched when all parallel clone workers have completed.
type CloneFinishedMsg struct {
	Results []orchestrator.CloneResult
	Err     error
}

// BuildRepoTreeItems constructs a list of RepoTreeItem nodes from a workspace manifest and scope.
func BuildRepoTreeItems(m *manifest.WorkspaceManifest, defaultScope string) []RepoTreeItem {
	var items []RepoTreeItem
	s := strings.TrimSpace(strings.ToLower(defaultScope))
	if s == "" {
		s = "core"
	}

	if m == nil {
		defaults := []struct {
			name     string
			path     string
			scope    string
			required bool
		}{
			{name: "orbit-infra", path: "orbit/orbit-infra", scope: "orbit", required: true},
			{name: "orbit-frontend", path: "orbit/orbit-frontend", scope: "orbit", required: true},
			{name: "handbook", path: "handbook", scope: "handbook", required: false},
			{name: "ts", path: "manovaspace/ts", scope: "manovaspace", required: false},
			{name: "design-system", path: "manovaspace/design-system", scope: "manovaspace", required: false},
			{name: "docs", path: "manovaspace/docs", scope: "manovaspace", required: false},
		}

		for _, d := range defaults {
			selected := d.required
			if !selected {
				if s == "all" || s == "*" || strings.EqualFold(d.scope, s) || strings.EqualFold(d.name, s) {
					selected = true
				}
			}
			items = append(items, RepoTreeItem{
				Name:     d.name,
				Path:     d.path,
				Scope:    d.scope,
				Required: d.required,
				Selected: selected,
				Status:   "pending",
			})
		}
		return items
	}

	targets := m.AllRepos()
	for _, target := range targets {
		selected := target.Required
		if !selected {
			if s == "all" || s == "*" {
				selected = true
			} else if strings.EqualFold(target.Scope, s) || strings.HasPrefix(strings.ToLower(target.Scope), s+"/") {
				selected = true
			} else if strings.EqualFold(target.Name, s) {
				selected = true
			}
		}

		items = append(items, RepoTreeItem{
			Name:     target.Name,
			Path:     target.Path,
			Scope:    target.Scope,
			Required: target.Required,
			Selected: selected,
			Status:   "pending",
		})
	}
	return items
}

// WorkspaceModel manages Stage 4: Workspace Repository Selection and Parallel Cloner.
type WorkspaceModel struct {
	parent        *WizardModel
	manifest      *manifest.WorkspaceManifest
	items         []RepoTreeItem
	cursor        int
	isCloning     bool
	isComplete    bool
	hasError      bool
	results       []orchestrator.CloneResult
	clonerRunner  CloneRunnerFunc
	spinner       spinner.Model
	width         int
	workspaceRoot string
	defaultScope  string
	concurrency   int
}

// NewWorkspaceModel initializes a new WorkspaceModel attached to the root WizardModel.
func NewWorkspaceModel(parent *WizardModel) *WorkspaceModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorPurple)

	w := MinTerminalWidth
	if parent != nil && parent.Width > 0 {
		w = parent.Width
	}

	workspaceRoot := ""
	if parent != nil && parent.Session != nil && parent.Session.WorkspacePath != "" {
		workspaceRoot = parent.Session.WorkspacePath
	}
	if workspaceRoot == "" {
		if envWS := os.Getenv("ORBIT_WORKSPACE"); envWS != "" {
			workspaceRoot = envWS
		} else {
			workspaceRoot, _ = os.Getwd()
		}
	}

	defaultScope := "core"
	if parent != nil && parent.Session != nil && parent.Session.Metadata != nil {
		if sc := parent.Session.Metadata["default_scope"]; sc != "" {
			defaultScope = sc
		} else if sc := parent.Session.Metadata["manifest_scope"]; sc != "" {
			defaultScope = sc
		}
	}

	var m *manifest.WorkspaceManifest
	manifestPath := filepath.Join(workspaceRoot, "workspace.yaml")
	if _, err := os.Stat(manifestPath); err == nil {
		m, _ = manifest.Load(manifestPath)
	}

	items := BuildRepoTreeItems(m, defaultScope)

	return &WorkspaceModel{
		parent:        parent,
		manifest:      m,
		items:         items,
		cursor:        0,
		isCloning:     false,
		isComplete:    false,
		hasError:      false,
		clonerRunner:  orchestrator.CloneTargets,
		spinner:       sp,
		width:         w,
		workspaceRoot: workspaceRoot,
		defaultScope:  defaultScope,
		concurrency:   DefaultCloneConcurrency,
	}
}

// Init initializes the Bubble Tea spinner.
func (m *WorkspaceModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Items returns the current list of repository tree items.
func (m *WorkspaceModel) Items() []RepoTreeItem {
	return m.items
}

// SetItems updates the repository tree items.
func (m *WorkspaceModel) SetItems(items []RepoTreeItem) {
	m.items = items
	if m.cursor >= len(items) && len(items) > 0 {
		m.cursor = len(items) - 1
	}
}

// Cursor returns the active item index.
func (m *WorkspaceModel) Cursor() int {
	return m.cursor
}

// SetCursor changes the active item index.
func (m *WorkspaceModel) SetCursor(idx int) {
	if len(m.items) == 0 {
		m.cursor = 0
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.items) {
		idx = len(m.items) - 1
	}
	m.cursor = idx
}

// IsCloning returns true if background parallel cloning is currently executing.
func (m *WorkspaceModel) IsCloning() bool {
	return m.isCloning
}

// SetCloning updates the cloning state.
func (m *WorkspaceModel) SetCloning(cloning bool) {
	m.isCloning = cloning
}

// HasError returns true if a cloning error occurred.
func (m *WorkspaceModel) HasError() bool {
	return m.hasError
}

// SetClonerRunner injects a custom clone runner (useful for testing).
func (m *WorkspaceModel) SetClonerRunner(fn CloneRunnerFunc) {
	m.clonerRunner = fn
}

// WorkspaceRoot returns the target directory for cloning repositories.
func (m *WorkspaceModel) WorkspaceRoot() string {
	return m.workspaceRoot
}

// SetWorkspaceRoot updates the workspace root directory.
func (m *WorkspaceModel) SetWorkspaceRoot(root string) {
	m.workspaceRoot = root
}

// DefaultScope returns the initial default scope.
func (m *WorkspaceModel) DefaultScope() string {
	return m.defaultScope
}

// SetDefaultScope updates the default scope and rebuilds items.
func (m *WorkspaceModel) SetDefaultScope(scope string) {
	m.defaultScope = scope
	m.items = BuildRepoTreeItems(m.manifest, scope)
}

// SelectedTargets resolves all currently selected items into manifest.RepoTarget instances.
func (m *WorkspaceModel) SelectedTargets() []manifest.RepoTarget {
	var targets []manifest.RepoTarget
	for _, it := range m.items {
		if it.Selected {
			var remoteURL string
			defaultBranch := "main"

			if m.manifest != nil {
				for _, rt := range m.manifest.AllRepos() {
					if rt.Path == it.Path || rt.Name == it.Name {
						remoteURL = rt.RemoteURL
						if rt.DefaultBranch != "" {
							defaultBranch = rt.DefaultBranch
						}
						break
					}
				}
			}

			if remoteURL == "" {
				remoteBase := "git@git.dev.manova.space:"
				if m.parent != nil && m.parent.Session != nil && m.parent.Session.Metadata != nil && m.parent.Session.Metadata["git_remote_base"] != "" {
					remoteBase = m.parent.Session.Metadata["git_remote_base"]
				}
				remoteURL = remoteBase + it.Name + ".git"
			}

			targets = append(targets, manifest.RepoTarget{
				Name:          it.Name,
				Path:          it.Path,
				RemoteURL:     remoteURL,
				DefaultBranch: defaultBranch,
				Required:      it.Required,
				Scope:         it.Scope,
			})
		}
	}
	return targets
}

// ToggleSelected flips the selection status of the item at index if it is optional.
func (m *WorkspaceModel) ToggleSelected(idx int) {
	if idx < 0 || idx >= len(m.items) {
		return
	}
	if !m.items[idx].Required {
		m.items[idx].Selected = !m.items[idx].Selected
	}
}

// ToggleAll toggles selection of all optional items.
func (m *WorkspaceModel) ToggleAll() {
	allOptionalSelected := true
	hasOptional := false

	for _, it := range m.items {
		if !it.Required {
			hasOptional = true
			if !it.Selected {
				allOptionalSelected = false
				break
			}
		}
	}

	if !hasOptional {
		return
	}

	targetSelected := !allOptionalSelected
	for i := range m.items {
		if !m.items[i].Required {
			m.items[i].Selected = targetSelected
		}
	}
}

func waitForCloneProgress(ch <-chan orchestrator.CloneResult) tea.Cmd {
	return func() tea.Msg {
		res, ok := <-ch
		if !ok {
			return CloneFinishedMsg{}
		}
		return CloneProgressMsg{
			Result: res,
			ch:     ch,
		}
	}
}

// RunCloner initiates parallel asynchronous cloning of all selected repositories.
func (m *WorkspaceModel) RunCloner() tea.Cmd {
	m.isCloning = true
	m.hasError = false
	m.results = nil
	if m.parent != nil {
		m.parent.ErrorMsg = ""
	}

	for i := range m.items {
		if m.items[i].Selected {
			m.items[i].Status = "cloning"
			m.items[i].Error = ""
			m.items[i].Progress = 0.0
		}
	}

	targets := m.SelectedTargets()
	if len(targets) == 0 {
		m.isCloning = false
		m.isComplete = true
		if m.parent != nil {
			m.parent.SetStage(session.StageEnvironment)
		}
		return nil
	}

	ch := make(chan orchestrator.CloneResult, len(targets))

	runner := m.clonerRunner
	if runner == nil {
		runner = orchestrator.CloneTargets
	}

	wsRoot := m.workspaceRoot
	concurrency := m.concurrency
	if concurrency <= 0 {
		concurrency = DefaultCloneConcurrency
	}

	go func() {
		_ = runner(wsRoot, targets, concurrency, func(r orchestrator.CloneResult) {
			ch <- r
		})
		close(ch)
	}()

	return waitForCloneProgress(ch)
}

// Update processes Bubble Tea messages, navigation keystrokes, and parallel clone progress.
func (m *WorkspaceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case CloneProgressMsg:
		res := msg.Result
		for i := range m.items {
			if m.items[i].Name == res.Name || m.items[i].Path == res.Path {
				if res.Success {
					if res.AlreadyExists {
						m.items[i].Status = "exists"
					} else {
						m.items[i].Status = "cloned"
					}
					m.items[i].Progress = 1.0
					m.items[i].Error = ""
				} else {
					m.items[i].Status = "failed"
					m.items[i].Error = res.Error
					m.items[i].Progress = 0.0
				}
				break
			}
		}
		m.results = append(m.results, res)

		if msg.ch != nil {
			cmds = append(cmds, waitForCloneProgress(msg.ch))
		}
		return m, tea.Batch(cmds...)

	case CloneFinishedMsg:
		m.isCloning = false

		hasFailure := false
		var failureNames []string
		for _, it := range m.items {
			if it.Selected && it.Status == "failed" {
				hasFailure = true
				failureNames = append(failureNames, it.Name)
			}
		}

		if hasFailure {
			m.hasError = true
			errMsg := fmt.Sprintf("Cloning failed for %s. Press [R] to retry.", strings.Join(failureNames, ", "))
			if m.parent != nil {
				m.parent.SetError(errMsg)
			}
			return m, nil
		}

		// All selected items cloned successfully
		m.isComplete = true
		m.hasError = false

		if m.parent != nil {
			m.parent.ErrorMsg = ""
			if m.parent.Session != nil {
				m.parent.Session.WorkspacePath = m.workspaceRoot
				var clonedList []string
				for _, it := range m.items {
					if it.Selected && (it.Status == "cloned" || it.Status == "exists") {
						clonedList = append(clonedList, it.Path)
					}
				}
				m.parent.Session.ClonedRepos = clonedList
				m.parent.Session.CurrentStage = session.StageReposCloned

				if m.parent.SessionManager != nil {
					_ = m.parent.SessionManager.SaveCheckpoint(m.parent.Session)
				}
			}
			m.parent.SetStage(session.StageEnvironment)
		}
		return m, nil

	case tea.KeyMsg:
		// Retry trigger on failure
		if (msg.String() == "r" || msg.String() == "R") && m.hasError {
			return m, m.RunCloner()
		}

		if !m.isCloning {
			switch msg.Type {
			case tea.KeyDown:
				m.SetCursor(m.cursor + 1)
				return m, nil

			case tea.KeyUp:
				m.SetCursor(m.cursor - 1)
				return m, nil

			case tea.KeySpace:
				m.ToggleSelected(m.cursor)
				return m, nil

			case tea.KeyEnter:
				selectedTargets := m.SelectedTargets()
				if len(selectedTargets) > 0 {
					return m, m.RunCloner()
				}
				return m, nil

			case tea.KeyRunes:
				switch string(msg.Runes) {
				case "j", "J":
					m.SetCursor(m.cursor + 1)
					return m, nil
				case "k", "K":
					m.SetCursor(m.cursor - 1)
					return m, nil
				case "a", "A":
					m.ToggleAll()
					return m, nil
				}
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func renderProgressBar(progress float64, width int) string {
	if width <= 0 {
		width = 30
	}
	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	pct := int(progress * 100)
	return fmt.Sprintf("[%s] %d%%", bar, pct)
}

// View renders the Workspace Repository Selection and Parallel Cloner screen.
func (m *WorkspaceModel) View() string {
	title := TitleStyle.Render("✦ Workspace Repositories & Parallel Cloner")
	subtitle := SubduedStyle.Render("Select and clone platform and client repositories into your local workspace.")

	selectedCount := 0
	completedCount := 0
	for _, it := range m.items {
		if it.Selected {
			selectedCount++
			if it.Status == "cloned" || it.Status == "exists" {
				completedCount++
			}
		}
	}

	var contentSection string

	if m.isCloning {
		// Progress view during cloning
		progressFraction := 0.0
		if selectedCount > 0 {
			progressFraction = float64(completedCount) / float64(selectedCount)
		}

		progressHeader := fmt.Sprintf("%s %s", m.spinner.View(), TitleStyle.Render(fmt.Sprintf("Cloning Repositories (%d/%d complete)...", completedCount, selectedCount)))
		pbar := renderProgressBar(progressFraction, 36)

		var lines []string
		lines = append(lines, progressHeader, "", "  "+pbar, "")

		for _, it := range m.items {
			if !it.Selected {
				continue
			}

			var statusLine string
			switch it.Status {
			case "cloned":
				statusLine = fmt.Sprintf("  %s %-20s %s", SuccessStyle.Render("✔"), lipgloss.NewStyle().Bold(true).Render(it.Name), SuccessStyle.Render("(cloned)"))
			case "exists":
				statusLine = fmt.Sprintf("  %s %-20s %s", SuccessStyle.Render("✔"), lipgloss.NewStyle().Bold(true).Render(it.Name), SubduedStyle.Render("(already present)"))
			case "cloning":
				statusLine = fmt.Sprintf("  %s %-20s %s", m.spinner.View(), lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(it.Name), InfoStyle.Render("(cloning...)"))
			case "failed":
				statusLine = fmt.Sprintf("  %s %-20s %s", ErrorStyle.Render("✖"), lipgloss.NewStyle().Bold(true).Render(it.Name), ErrorStyle.Render(fmt.Sprintf("(failed: %s)", it.Error)))
			default:
				statusLine = fmt.Sprintf("  %s %-20s %s", SubduedStyle.Render("○"), SubduedStyle.Render(it.Name), SubduedStyle.Render("(waiting)"))
			}
			lines = append(lines, statusLine)
		}

		lines = append(lines, "", SubduedStyle.Render("  Cloning in parallel with 4 workers..."))
		contentSection = strings.Join(lines, "\n")
	} else if m.hasError {
		// Error view with retry prompt
		errHeader := ErrorStyle.Render("✖ One or more repositories failed to clone.")
		var lines []string
		lines = append(lines, errHeader, "")

		for _, it := range m.items {
			if it.Selected && it.Status == "failed" {
				lines = append(lines, fmt.Sprintf("  %s %s: %s", ErrorStyle.Render("✖"), lipgloss.NewStyle().Bold(true).Render(it.Name), ErrorStyle.Render(it.Error)))
			}
		}

		retryCard := ActiveCardBoxStyle.Render(fmt.Sprintf("%s %s", KeyStyle.Render("[R]"), lipgloss.NewStyle().Foreground(ColorWhite).Render("Retry Failed Repositories")))
		hint := SubduedStyle.Render("Press [R] to retry failed repository clones.")

		lines = append(lines, "", retryCard, "", hint)
		contentSection = strings.Join(lines, "\n")
	} else {
		// Selection list view
		headerInfo := fmt.Sprintf("  %s %s    %s %s",
			SubduedStyle.Render("Workspace:"), InfoStyle.Render(m.workspaceRoot),
			SubduedStyle.Render("Selected:"), lipgloss.NewStyle().Foreground(ColorWhite).Bold(true).Render(fmt.Sprintf("%d / %d repos", selectedCount, len(m.items))),
		)

		var itemRows []string
		for i, it := range m.items {
			cursorMarker := "  "
			if i == m.cursor {
				cursorMarker = KeyStyle.Render("❯ ")
			}

			var checkbox string
			if it.Required {
				checkbox = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true).Render("[✔]")
			} else if it.Selected {
				checkbox = SuccessStyle.Render("[x]")
			} else {
				checkbox = SubduedStyle.Render("[ ]")
			}

			nameStyle := lipgloss.NewStyle()
			if it.Selected {
				nameStyle = nameStyle.Foreground(ColorWhite).Bold(true)
			} else {
				nameStyle = nameStyle.Foreground(ColorSubdued)
			}

			nameText := nameStyle.Render(fmt.Sprintf("%-18s", it.Name))
			pathText := SubduedStyle.Render(fmt.Sprintf("%-26s", it.Path))
			scopeBadge := lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("[%s]", it.Scope))

			var reqBadge string
			if it.Required {
				reqBadge = lipgloss.NewStyle().Foreground(ColorPurple).Render(" (required)")
			}

			row := fmt.Sprintf("%s%s %s %s %s%s", cursorMarker, checkbox, nameText, pathText, scopeBadge, reqBadge)
			itemRows = append(itemRows, row)
		}

		actionCard := ActiveCardBoxStyle.Render(
			fmt.Sprintf("%s %s", KeyStyle.Render("[Enter]"), lipgloss.NewStyle().Foreground(ColorWhite).Render(fmt.Sprintf("Start Clone (%d Repositories)", selectedCount))),
		)

		hint := SubduedStyle.Render("Press [↑/↓/j/k] to navigate, [Space] to toggle, [A] to toggle all, [Enter] to clone.")

		contentSection = lipgloss.JoinVertical(
			lipgloss.Left,
			headerInfo,
			"",
			strings.Join(itemRows, "\n"),
			"",
			actionCard,
			"",
			hint,
		)
	}

	w := m.width
	if m.parent != nil && m.parent.Width > 0 {
		w = m.parent.Width
	}

	cardWidth := 74
	if w > 20 {
		cardWidth = w - 8
		if cardWidth > 84 {
			cardWidth = 84
		}
	}

	box := CardBoxStyle.
		Width(cardWidth).
		Render(lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"",
			subtitle,
			"",
			contentSection,
		))

	if w > 0 {
		return lipgloss.NewStyle().
			Width(w).
			Align(lipgloss.Center).
			Render(box)
	}
	return box
}
