package onboard

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/session"
	"golang.org/x/crypto/ssh"
)

// Default paths and server configuration.
const (
	DefaultSSHKeyName = "id_ed25519_orbit"
	DefaultServerURL  = "https://api.dev.manova.space"
	DefaultSSHHost    = "git.dev.manova.space"
	DefaultSSHUser    = "git"
)

// IdentityClaimFunc defines the signature for asynchronous claim submission.
type IdentityClaimFunc func(ctx context.Context, serverURL string, req client.ClaimRequest) (*client.ClaimResponse, error)

// ClaimFinishedMsg is sent when background claim submission completes.
type ClaimFinishedMsg struct {
	Response *client.ClaimResponse
	Err      error
}

// EnsureSSHKeypair generates a new Ed25519 keypair if missing, saves the private key with
// POSIX 0600 permissions and public key with 0644 permissions, and returns the authorized_keys string.
func EnsureSSHKeypair(keyPath string) (string, error) {
	if keyPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to determine user home directory: %w", err)
		}
		keyPath = filepath.Join(home, ".ssh", DefaultSSHKeyName)
	}

	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create ssh directory %s: %w", dir, err)
	}
	_ = os.Chmod(dir, 0700)

	pubKeyPath := keyPath + ".pub"

	// If key already exists, load and return existing public key
	if _, err := os.Stat(keyPath); err == nil {
		if pubData, err := os.ReadFile(pubKeyPath); err == nil {
			trimmed := strings.TrimSpace(string(pubData))
			if trimmed != "" {
				return trimmed, nil
			}
		}

		// Public key missing but private key exists, derive public key
		privData, err := os.ReadFile(keyPath)
		if err != nil {
			return "", fmt.Errorf("failed to read existing private key: %w", err)
		}

		signer, err := ssh.ParsePrivateKey(privData)
		if err == nil {
			pubBytes := ssh.MarshalAuthorizedKey(signer.PublicKey())
			pubStr := fmt.Sprintf("%s orbit-developer\n", strings.TrimSpace(string(pubBytes)))
			_ = os.WriteFile(pubKeyPath, []byte(pubStr), 0644)
			_ = os.Chmod(pubKeyPath, 0644)
			return strings.TrimSpace(pubStr), nil
		}
	}

	// Generate fresh Ed25519 keypair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate ed25519 keypair: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", fmt.Errorf("failed to marshal PKCS#8 private key: %w", err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	})

	if err := os.WriteFile(keyPath, privPEM, 0600); err != nil {
		return "", fmt.Errorf("failed to write private key to %s: %w", keyPath, err)
	}
	_ = os.Chmod(keyPath, 0600)

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("failed to format ssh public key: %w", err)
	}

	pubStr := fmt.Sprintf("%s orbit-developer\n", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))))
	if err := os.WriteFile(pubKeyPath, []byte(pubStr), 0644); err != nil {
		return "", fmt.Errorf("failed to write public key to %s: %w", pubKeyPath, err)
	}
	_ = os.Chmod(pubKeyPath, 0644)

	return strings.TrimSpace(pubStr), nil
}

// PublicKeyFingerprint returns the SHA256 fingerprint for an OpenSSH public key string.
func PublicKeyFingerprint(pubKeyStr string) string {
	if strings.TrimSpace(pubKeyStr) == "" {
		return ""
	}
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubKeyStr))
	if err != nil {
		return ""
	}
	return ssh.FingerprintSHA256(parsed)
}

// ConfigureSSHHost appends or updates the Host entry in ~/.ssh/config.
func ConfigureSSHHost(sshConfigPath, host, hostname, user, identityFile string) error {
	if sshConfigPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to determine user home directory: %w", err)
		}
		sshConfigPath = filepath.Join(home, ".ssh", "config")
	}

	dir := filepath.Dir(sshConfigPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory for ssh config: %w", err)
	}

	if host == "" {
		host = DefaultSSHHost
	}
	if hostname == "" {
		hostname = host
	}
	if user == "" {
		user = DefaultSSHUser
	}
	if identityFile == "" {
		home, _ := os.UserHomeDir()
		identityFile = filepath.Join(home, ".ssh", DefaultSSHKeyName)
	}

	newHostBlock := fmt.Sprintf("Host %s\n    HostName %s\n    User %s\n    IdentityFile %s\n    IdentitiesOnly yes\n    StrictHostKeyChecking accept-new", host, hostname, user, identityFile)

	var existingContent string
	if data, err := os.ReadFile(sshConfigPath); err == nil {
		existingContent = string(data)
	}

	var outLines []string
	inTargetBlock := false
	foundBlock := false

	lines := strings.Split(existingContent, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Host ") || strings.HasPrefix(trimmed, "Host\t") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 && fields[1] == host {
				inTargetBlock = true
				foundBlock = true
				outLines = append(outLines, newHostBlock)
				continue
			} else {
				inTargetBlock = false
			}
		}

		if !inTargetBlock {
			outLines = append(outLines, line)
		}
	}

	if !foundBlock {
		if len(outLines) > 0 && strings.TrimSpace(outLines[len(outLines)-1]) != "" {
			outLines = append(outLines, "")
		}
		outLines = append(outLines, newHostBlock, "")
	}

	result := strings.Join(outLines, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}

	if err := os.WriteFile(sshConfigPath, []byte(result), 0600); err != nil {
		return fmt.Errorf("failed to write ssh config: %w", err)
	}
	_ = os.Chmod(sshConfigPath, 0600)

	return nil
}

// IdentityModel manages Stage 3: Developer Identity, Ed25519 Keypair, and Server Claim.
type IdentityModel struct {
	parent       *WizardModel
	nameInput    textinput.Model
	uidInput     textinput.Model
	serverInput  textinput.Model
	focusedIndex int // 0: Name, 1: UID, 2: ServerURL

	keyPath     string
	pubKey      string
	fingerprint string
	serverURL   string

	submitting    bool
	claimResponse *client.ClaimResponse
	claimFunc     IdentityClaimFunc

	spinner spinner.Model
	width   int
}

// NewIdentityModel initializes a new IdentityModel attached to the root WizardModel.
func NewIdentityModel(parent *WizardModel) *IdentityModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorPurple)

	// Display Name input
	nameTi := textinput.New()
	nameTi.Placeholder = "Enter your full name (e.g. Grace Hopper)"
	nameTi.CharLimit = 64
	nameTi.Width = 48
	nameTi.Prompt = "Display Name ❯ "
	nameTi.PromptStyle = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	nameTi.TextStyle = lipgloss.NewStyle().Foreground(ColorCyan)
	nameTi.PlaceholderStyle = lipgloss.NewStyle().Foreground(ColorGray)
	nameTi.Focus()

	// Desired UID input
	uidTi := textinput.New()
	uidTi.Placeholder = "Enter desired username / UID (e.g. ghopper)"
	uidTi.CharLimit = 32
	uidTi.Width = 48
	uidTi.Prompt = "Desired UID  ❯ "
	uidTi.PromptStyle = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	uidTi.TextStyle = lipgloss.NewStyle().Foreground(ColorCyan)
	uidTi.PlaceholderStyle = lipgloss.NewStyle().Foreground(ColorGray)

	// Server Base URL input
	serverTi := textinput.New()
	serverTi.Placeholder = "https://api.dev.manova.space or http://localhost:8080"
	serverTi.CharLimit = 128
	serverTi.Width = 48
	serverTi.Prompt = "Server URL   ❯ "
	serverTi.PromptStyle = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	serverTi.TextStyle = lipgloss.NewStyle().Foreground(ColorCyan)
	serverTi.PlaceholderStyle = lipgloss.NewStyle().Foreground(ColorGray)

	defaultServer := os.Getenv("ORBIT_SERVER")
	if defaultServer == "" {
		defaultServer = DefaultServerURL
	}
	serverTi.SetValue(defaultServer)

	w := MinTerminalWidth
	if parent != nil && parent.Width > 0 {
		w = parent.Width
	}

	home, _ := os.UserHomeDir()
	defaultKeyPath := filepath.Join(home, ".ssh", DefaultSSHKeyName)

	m := &IdentityModel{
		parent:       parent,
		nameInput:    nameTi,
		uidInput:     uidTi,
		serverInput:  serverTi,
		focusedIndex: 0,
		keyPath:      defaultKeyPath,
		serverURL:    defaultServer,
		spinner:      sp,
		width:        w,
	}

	// Pre-fill profile from session if available
	if parent != nil && parent.Session != nil {
		if parent.Session.DisplayName != "" {
			nameTi.SetValue(parent.Session.DisplayName)
		}
		if parent.Session.UID != "" {
			uidTi.SetValue(parent.Session.UID)
		}
		if parent.Session.SSHPublicKey != "" {
			m.pubKey = parent.Session.SSHPublicKey
			m.fingerprint = PublicKeyFingerprint(m.pubKey)
		}
	}

	return m
}

// Init activates the spinner and focuses the first input.
func (m *IdentityModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.nameInput.Focus())
}

// FocusedIndex returns the currently focused form field index (0, 1, or 2).
func (m *IdentityModel) FocusedIndex() int {
	return m.focusedIndex
}

// SetFocusedIndex changes the active form field focus.
func (m *IdentityModel) SetFocusedIndex(idx int) {
	m.focusedIndex = (idx%3 + 3) % 3
	m.nameInput.Blur()
	m.uidInput.Blur()
	m.serverInput.Blur()

	switch m.focusedIndex {
	case 0:
		m.nameInput.Focus()
	case 1:
		m.uidInput.Focus()
	case 2:
		m.serverInput.Focus()
	}
}

// DisplayName returns the trimmed value of the Display Name input.
func (m *IdentityModel) DisplayName() string {
	return strings.TrimSpace(m.nameInput.Value())
}

// SetDisplayName sets the value of the Display Name input.
func (m *IdentityModel) SetDisplayName(name string) {
	m.nameInput.SetValue(strings.TrimSpace(name))
}

// DesiredUID returns the trimmed value of the Desired UID input.
func (m *IdentityModel) DesiredUID() string {
	return strings.TrimSpace(m.uidInput.Value())
}

// SetDesiredUID sets the value of the Desired UID input.
func (m *IdentityModel) SetDesiredUID(uid string) {
	m.uidInput.SetValue(strings.TrimSpace(uid))
}

// ServerURL returns the configured server base URL.
func (m *IdentityModel) ServerURL() string {
	val := strings.TrimSpace(m.serverInput.Value())
	if val == "" {
		return m.serverURL
	}
	return val
}

// SetServerURL updates the server base URL.
func (m *IdentityModel) SetServerURL(url string) {
	m.serverURL = strings.TrimSpace(url)
	m.serverInput.SetValue(m.serverURL)
}

// SSHKeyPath returns the target path for the SSH private key.
func (m *IdentityModel) SSHKeyPath() string {
	return m.keyPath
}

// SetSSHKeyPath configures a custom SSH key path and reloads keypair if exists.
func (m *IdentityModel) SetSSHKeyPath(path string) {
	m.keyPath = path
	if pub, err := EnsureSSHKeypair(path); err == nil {
		m.pubKey = pub
		m.fingerprint = PublicKeyFingerprint(pub)
	}
}

// SSHPublicKey returns the loaded or generated OpenSSH public key.
func (m *IdentityModel) SSHPublicKey() string {
	return m.pubKey
}

// KeyFingerprint returns the SHA256 public key fingerprint.
func (m *IdentityModel) KeyFingerprint() string {
	return m.fingerprint
}

// SetClaimFunc injects a custom claim submission runner (useful for testing).
func (m *IdentityModel) SetClaimFunc(fn IdentityClaimFunc) {
	m.claimFunc = fn
}

// SetSubmitting updates the submitting progress state.
func (m *IdentityModel) SetSubmitting(submitting bool) {
	m.submitting = submitting
}

// IsSubmitting returns true if a claim submission is currently in-flight.
func (m *IdentityModel) IsSubmitting() bool {
	return m.submitting
}

// RunClaim triggers asynchronous cryptographic key generation and server claim submission.
func (m *IdentityModel) RunClaim() tea.Cmd {
	m.submitting = true
	if m.parent != nil {
		m.parent.ErrorMsg = ""
	}

	keyPath := m.keyPath
	if keyPath == "" {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, ".ssh", DefaultSSHKeyName)
		m.keyPath = keyPath
	}

	pubKey, err := EnsureSSHKeypair(keyPath)
	if err != nil {
		m.submitting = false
		if m.parent != nil {
			m.parent.SetError(fmt.Sprintf("SSH keypair generation failed: %v", err))
		}
		return nil
	}
	m.pubKey = pubKey
	m.fingerprint = PublicKeyFingerprint(pubKey)

	// Inject Host block into SSH config
	_ = ConfigureSSHHost("", DefaultSSHHost, DefaultSSHHost, DefaultSSHUser, keyPath)

	token := ""
	if m.parent != nil && m.parent.Session != nil {
		token = m.parent.Session.InviteToken
	}
	if token == "" && m.parent != nil && m.parent.Options.PreSetToken != "" {
		token = m.parent.Options.PreSetToken
	}

	dispName := m.DisplayName()
	uid := m.DesiredUID()
	serverURL := m.ServerURL()
	if serverURL == "" {
		serverURL = DefaultServerURL
	}

	claimReq := client.ClaimRequest{
		InviteToken:  token,
		DesiredUID:   uid,
		DisplayName:  dispName,
		SSHPublicKey: pubKey,
	}

	claimFn := m.claimFunc

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var resp *client.ClaimResponse
		var claimErr error

		if claimFn != nil {
			resp, claimErr = claimFn(ctx, serverURL, claimReq)
		} else {
			c := client.NewClient(serverURL)
			resp, claimErr = c.ClaimToken(ctx, claimReq)
		}

		return ClaimFinishedMsg{
			Response: resp,
			Err:      claimErr,
		}
	}
}

// Update processes Bubble Tea messages, text input keystrokes, and claim dispatch.
func (m *IdentityModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case ClaimFinishedMsg:
		m.submitting = false
		if msg.Err != nil {
			if m.parent != nil {
				m.parent.SetError(fmt.Sprintf("Claim submission failed: %v", msg.Err))
			}
			return m, nil
		}

		m.claimResponse = msg.Response
		if m.parent != nil {
			m.parent.ErrorMsg = ""
			if m.parent.Session != nil {
				if msg.Response.User.UID != "" {
					m.parent.Session.UID = msg.Response.User.UID
				}
				if msg.Response.User.Email != "" {
					m.parent.Session.Email = msg.Response.User.Email
				}
				if msg.Response.User.DisplayName != "" {
					m.parent.Session.DisplayName = msg.Response.User.DisplayName
				}
				if msg.Response.Credentials.ForgejoMCPToken != "" {
					m.parent.Session.ForgejoToken = msg.Response.Credentials.ForgejoMCPToken
				}
				if msg.Response.Credentials.WireGuardConfig != "" {
					m.parent.Session.WireGuardConfig = msg.Response.Credentials.WireGuardConfig
				}
				if m.pubKey != "" {
					m.parent.Session.SSHPublicKey = m.pubKey
				}
				if m.parent.Session.Metadata == nil {
					m.parent.Session.Metadata = make(map[string]string)
				}
				if msg.Response.Workspace.GitRemoteBase != "" {
					m.parent.Session.Metadata["git_remote_base"] = msg.Response.Workspace.GitRemoteBase
				}

				m.parent.Session.CurrentStage = session.StageClaimSubmitted
				if m.parent.SessionManager != nil {
					_ = m.parent.SessionManager.SaveCheckpoint(m.parent.Session)
				}
			}
			m.parent.SetStage(session.StageWorkspace)
		}
		return m, nil

	case tea.KeyMsg:
		// Retry shortcut when in error state
		if (msg.String() == "r" || msg.String() == "R") && m.parent != nil && m.parent.ErrorMsg != "" {
			return m, m.RunClaim()
		}

		switch msg.Type {
		case tea.KeyTab, tea.KeyDown:
			m.SetFocusedIndex(m.focusedIndex + 1)
			return m, nil

		case tea.KeyShiftTab, tea.KeyUp:
			m.SetFocusedIndex(m.focusedIndex - 1)
			return m, nil

		case tea.KeyEnter:
			if !m.submitting {
				return m, m.RunClaim()
			}
			return m, nil
		}
	}

	if !m.submitting {
		var cmd tea.Cmd
		switch m.focusedIndex {
		case 0:
			m.nameInput, cmd = m.nameInput.Update(msg)
		case 1:
			m.uidInput, cmd = m.uidInput.Update(msg)
		case 2:
			m.serverInput, cmd = m.serverInput.Update(msg)
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func maskTokenPreview(tok string) string {
	trimmed := strings.TrimSpace(tok)
	if trimmed == "" {
		return "none"
	}
	if len(trimmed) <= 8 {
		return trimmed + "..."
	}
	return trimmed[:8] + "..."
}

// View renders the Identity and Server Claim Registration stage card.
func (m *IdentityModel) View() string {
	title := TitleStyle.Render("✦ Developer Identity & Platform Registration")
	subtitle := SubduedStyle.Render("Provision cryptographic Ed25519 keypair and register developer claim.")

	tokenVal := ""
	if m.parent != nil && m.parent.Session != nil {
		tokenVal = m.parent.Session.InviteToken
	}
	if tokenVal == "" && m.parent != nil && m.parent.Options.PreSetToken != "" {
		tokenVal = m.parent.Options.PreSetToken
	}

	maskedToken := maskTokenPreview(tokenVal)

	fp := m.fingerprint
	if fp == "" {
		fp = "Auto-generating on claim submission..."
	}

	// Status info block
	tokenLine := fmt.Sprintf("  %s  %s", SubduedStyle.Render("Claim Token:"), InfoStyle.Render(maskedToken))
	keyLine := fmt.Sprintf("  %s      %s (%s)", SubduedStyle.Render("SSH Key:"), InfoStyle.Render(m.keyPath), SubduedStyle.Render(fp))

	nameBorder := ColorDarkGray
	uidBorder := ColorDarkGray
	serverBorder := ColorDarkGray

	switch m.focusedIndex {
	case 0:
		nameBorder = ColorPurple
	case 1:
		uidBorder = ColorPurple
	case 2:
		serverBorder = ColorPurple
	}

	nameBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(nameBorder).
		Padding(0, 1).
		Render(m.nameInput.View())

	uidBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(uidBorder).
		Padding(0, 1).
		Render(m.uidInput.View())

	serverBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(serverBorder).
		Padding(0, 1).
		Render(m.serverInput.View())

	var actionSection string
	if m.submitting {
		actionSection = lipgloss.JoinVertical(
			lipgloss.Center,
			"",
			fmt.Sprintf("%s %s", m.spinner.View(), InfoStyle.Render("Submitting cryptographic claim to Orbit Server...")),
			"",
		)
	} else {
		actionCard := ActiveCardBoxStyle.Render(
			fmt.Sprintf("%s %s", KeyStyle.Render("[Enter]"), lipgloss.NewStyle().Foreground(ColorWhite).Render("Submit Claim & Register Key")),
		)
		hint := SubduedStyle.Render("Press [Tab] / [Shift+Tab] to navigate form fields, [Enter] to submit claim.")
		actionSection = lipgloss.JoinVertical(
			lipgloss.Center,
			"",
			actionCard,
			"",
			hint,
		)
	}

	formSection := lipgloss.JoinVertical(
		lipgloss.Left,
		tokenLine,
		keyLine,
		"",
		SubduedStyle.Render("Developer Profile:"),
		nameBox,
		"",
		uidBox,
		"",
		SubduedStyle.Render("Orbit API Server Base URL:"),
		serverBox,
		actionSection,
	)

	w := m.width
	if m.parent != nil && m.parent.Width > 0 {
		w = m.parent.Width
	}

	cardWidth := 74
	if w > 20 {
		cardWidth = w - 8
		if cardWidth > 82 {
			cardWidth = 82
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
			formSection,
		))

	if w > 0 {
		return lipgloss.NewStyle().
			Width(w).
			Align(lipgloss.Center).
			Render(box)
	}
	return box
}
