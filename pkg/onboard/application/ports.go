// Package application implements the onboarding use cases over the ports the
// CLI adapters satisfy. It never runs external commands, touches the network or
// styles output: every side effect enters through a port and every human line
// goes through the injected writer and UI helpers.
package application

import (
	"context"
	"io"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
	workspacedomain "github.com/manovaspace/orbit-cli/pkg/workspace/domain"
)

// Provisioner claims an invitation token and rolls provisioned resources back.
type Provisioner interface {
	Claim(ctx context.Context, edgeURL, idempotencyKey string, req domain.ClaimRequest) (*domain.ClaimResponse, error)
	Rollback(ctx context.Context, edgeURL, uid string) error
}

// Sessions is the checkpoint store of onboarding sessions.
type Sessions = domain.SessionStore

// Diagnostics runs the pre-flight doctor checks and its healing recipes.
type Diagnostics interface {
	Run() *doctor.DoctorReport
	// HealableCount reports how many results have a registered healer.
	HealableCount(results []doctor.DiagnosticResult) int
	Heal(ctx context.Context, results []doctor.DiagnosticResult, progress func(name, status string)) error
}

// SSH detects or generates the developer keypair.
type SSH interface {
	EnsureKey(dir, email string) (string, error)
}

// Environment is the local machine and filesystem the flow provisions.
type Environment interface {
	HomeDir() (string, error)
	Hostname() string
	// Platform returns runtime.GOOS and runtime.GOARCH.
	Platform() (goos, goarch string)
	Getenv(key string) string
	LookupHost(name string) ([]string, error)
	// TrustDevCA trusts the local Caddy root CA. present is false when caddy is
	// not installed; trusted is false when trust needs more privileges.
	TrustDevCA(ctx context.Context) (present, trusted bool)

	EnsureWorkspaceDirs(root string) error
	WriteWireGuard(config string) (path string, err error)
	ConfigureMCPEnv(root, token, uid string) error
	ConfigureShell(home string) error

	Exists(path string) bool
	IsDir(path string) bool
	Remove(path string) error
}

// Layout resolves the workspace root, manifest and orbit-infra directory.
type Layout interface {
	WorkspaceRoot(override string) string
	ManifestPath(root, flag string) string
	InfraDir(root string) string
}

// Repos clones the core manifest repositories and pulls their assets.
type Repos interface {
	// CloneCore resolves the core repositories, reports how many there are with
	// onStart and each result with onResult. It returns the repository paths
	// relative to root so their assets can be pulled, and an error when the
	// manifest is missing, unreadable or has no core targets.
	CloneCore(ctx context.Context, root, manifestPath string, onStart func(count int), onResult func(workspacedomain.CloneResult)) (rels []string, err error)
	PullAssets(ctx context.Context, root string, rels []string) error
}

// Stack starts the local development stack.
type Stack interface {
	Start(ctx context.Context, infraDir string) error
}

// Bundle writes the sanitized diagnostic archive.
type Bundle interface {
	Write(workspaceRoot string, session *domain.SessionState, customPath string) (string, error)
}

// Prompts asks the developer a question. The suffix is already styled.
type Prompts interface {
	YesNo(question, defaultSuffix string, defaultYes bool) bool
	String(question, defaultSuffix, defaultValue string) string
}

// UI carries the decoration cmd/orbit renders. Keeping it injected lets this
// package write the same lines without importing a styling library.
type UI struct {
	Arrow string
	OK    string
	Warn  string
	Error string
	Info  string

	Title     func(string) string
	Header    func(string) string
	Bold      func(string) string
	Success   func(string) string
	Warning   func(string) string
	ErrorText func(string) string
	InfoText  func(string) string
	Subtle    func(string) string
	Code      func(string) string
	// Card renders title and content inside the bordered summary box.
	Card func(title, content string) string
}

// Service is the onboarding use-case layer.
type Service struct {
	Provisioner Provisioner
	Sessions    Sessions
	Diagnostics Diagnostics
	SSH         SSH
	Environment Environment
	Layout      Layout
	Repos       Repos
	Stack       Stack
	Bundle      Bundle
	Prompts     Prompts

	Out io.Writer
	UI  UI

	// Now defaults to time.Now; tests pin it to keep event timestamps stable.
	Now func() time.Time
	// TTY reports whether stdin is a terminal and StdinIsOS reports whether the
	// command reads the process stdin; the flow uses both to decide whether to
	// ask questions.
	TTY       bool
	StdinIsOS bool
}
