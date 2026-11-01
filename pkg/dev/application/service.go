// Package application implements the local dev stack use cases over command,
// filesystem and readiness ports, keeping the CLI commands thin.
package application

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/dev/domain"
)

// Commands runs the infrastructure scripts and docker compose. Stream is used
// where the process writes an unbounded amount of output (logs -f).
type Commands interface {
	Run(ctx context.Context, dir, name string, args ...string) error
	Stream(ctx context.Context, dir, name string, args ...string) error
}

// Files locates the infrastructure checkout and checks paths.
type Files interface {
	InfraDir(workspaceRoot string) string
	Exists(path string) bool
	IsDir(path string) bool
}

// Probe checks whether a TCP service is accepting connections.
type Probe interface {
	Wait(ctx context.Context, host string, port int, timeout time.Duration) bool
}

// UI carries the decoration cmd/orbit already renders.
type UI struct {
	OK    string
	Warn  string
	Info  string
	Arrow string

	Title    func(string) string
	Bold     func(string) string
	Subtle   func(string) string
	Success  func(string) string
	Warning  func(string) string
	InfoText func(string) string
	Card     func(string) string
}

// Service is the local dev use-case layer.
type Service struct {
	Commands Commands
	Files    Files
	Probe    Probe
	Out      io.Writer
	ErrOut   io.Writer
	UI       UI
}

// UpInput is one `orbit dev up` invocation.
type UpInput struct {
	WorkspaceRoot string
	Modules       []string
	Wait          bool
	Timeout       time.Duration
}

// UpResult is what `orbit dev up` produced.
type UpResult struct {
	WorkspaceRoot string                 `json:"workspaceRoot"`
	InfraDir      string                 `json:"infraDir"`
	Command       Command                `json:"command"`
	Modules       []string               `json:"modules,omitempty"`
	Services      []domain.ServiceStatus `json:"services"`
}

// ActionResult is what `dev down`, `dev tier2` and `dev caddy` produced.
type ActionResult struct {
	WorkspaceRoot string  `json:"workspaceRoot"`
	InfraDir      string  `json:"infraDir"`
	Action        string  `json:"action"`
	Command       Command `json:"command"`
}

// Command is the external command a dev action ran.
type Command struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

// String renders the command line.
func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// PortalResult is what `orbit dev portal` produced.
type PortalResult struct {
	URL       string            `json:"url"`
	Endpoints []domain.Endpoint `json:"endpoints"`
}

// LogsInput is one `orbit dev logs` invocation. Plan resolves the command
// without streaming, so `--json` can describe it.
type LogsInput struct {
	WorkspaceRoot string
	Services      []string
	Plan          bool
}

// LogsResult is what `orbit dev logs` resolved.
type LogsResult struct {
	WorkspaceRoot string   `json:"workspaceRoot"`
	InfraDir      string   `json:"infraDir"`
	Command       Command  `json:"command"`
	Services      []string `json:"services,omitempty"`
	Streaming     bool     `json:"streaming"`
}

// Up starts the local stack and reports service readiness.
func (s *Service) Up(ctx context.Context, in UpInput) (UpResult, error) {
	result := UpResult{WorkspaceRoot: in.WorkspaceRoot, Modules: in.Modules, Services: []domain.ServiceStatus{}}
	infraDir := s.Files.InfraDir(in.WorkspaceRoot)
	result.InfraDir = infraDir

	fmt.Fprintln(s.out(), s.title("Starting Orbit Dev Stack..."))

	if err := s.ensureInfra(infraDir); err != nil {
		return result, err
	}

	startScript := filepath.Join(infraDir, "scripts", "start-dev-stack.sh")
	composeScript := filepath.Join(infraDir, "compose.sh")
	switch {
	case s.Files.Exists(startScript):
		result.Command = Command{Name: startScript, Args: in.Modules}
		if err := s.Commands.Stream(ctx, infraDir, startScript, in.Modules...); err != nil {
			return result, err
		}
	case s.Files.Exists(composeScript):
		args := append([]string{"up", "-d"}, in.Modules...)
		result.Command = Command{Name: composeScript, Args: args}
		if err := s.Commands.Stream(ctx, infraDir, composeScript, args...); err != nil {
			return result, err
		}
	default:
		args := append([]string{"compose", "up", "-d"}, in.Modules...)
		result.Command = Command{Name: "docker", Args: args}
		if err := s.Commands.Stream(ctx, infraDir, "docker", args...); err != nil {
			return result, err
		}
	}

	var statuses []domain.ServiceStatus
	if in.Wait {
		fmt.Fprintln(s.out(), s.info(fmt.Sprintf("Probing service readiness (timeout: %s)...", in.Timeout)))
		statuses = s.probeServices(ctx, in.Timeout)
	} else {
		statuses = s.probeServices(ctx, 500*time.Millisecond)
	}
	result.Services = statuses
	s.renderEndpointsBanner(statuses)
	return result, nil
}

// Down stops the local stack.
func (s *Service) Down(ctx context.Context, workspaceRoot string) (ActionResult, error) {
	result := ActionResult{WorkspaceRoot: workspaceRoot, Action: "down"}
	infraDir := s.Files.InfraDir(workspaceRoot)
	result.InfraDir = infraDir

	fmt.Fprintln(s.out(), s.title("Stopping Orbit Dev Stack..."))
	if err := s.ensureInfra(infraDir); err != nil {
		return result, err
	}

	composeScript := filepath.Join(infraDir, "compose.sh")
	if s.Files.Exists(composeScript) {
		result.Command = Command{Name: composeScript, Args: []string{"down"}}
		return result, s.Commands.Run(ctx, infraDir, composeScript, "down")
	}
	result.Command = Command{Name: "docker", Args: []string{"compose", "down"}}
	return result, s.Commands.Run(ctx, infraDir, "docker", "compose", "down")
}

// Tier2 starts the Tier 2 services.
func (s *Service) Tier2(ctx context.Context, workspaceRoot string, services []string) (ActionResult, error) {
	result := ActionResult{WorkspaceRoot: workspaceRoot, Action: "tier2"}
	infraDir := s.Files.InfraDir(workspaceRoot)
	result.InfraDir = infraDir

	fmt.Fprintln(s.out(), s.title("Starting Orbit Tier 2 Services..."))

	tier2Script := filepath.Join(infraDir, "scripts", "start-tier2.sh")
	if !s.Files.Exists(tier2Script) {
		return result, clierr.Failed(fmt.Sprintf("start-tier2.sh script not found at %s", tier2Script))
	}
	result.Command = Command{Name: tier2Script, Args: services}
	return result, s.Commands.Run(ctx, infraDir, tier2Script, services...)
}

// Caddy reloads, restarts or follows the local Caddy proxy.
func (s *Service) Caddy(ctx context.Context, workspaceRoot, action string) (ActionResult, error) {
	parsed, err := domain.ParseCaddyAction(action)
	if err != nil {
		return ActionResult{}, clierr.Usage(err.Error())
	}

	result := ActionResult{WorkspaceRoot: workspaceRoot, Action: string(parsed)}
	infraDir := s.Files.InfraDir(workspaceRoot)
	result.InfraDir = infraDir

	switch parsed {
	case domain.CaddyReload:
		fmt.Fprintln(s.out(), s.title("Reloading Caddy configuration..."))
		if err := s.ensureInfra(infraDir); err != nil {
			return result, err
		}
		composeScript := filepath.Join(infraDir, "compose.sh")
		args := []string{"exec", "caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile"}
		if s.Files.Exists(composeScript) {
			result.Command = Command{Name: composeScript, Args: args}
			return result, s.Commands.Run(ctx, infraDir, composeScript, args...)
		}
		result.Command = Command{Name: "docker", Args: append([]string{"compose"}, args...)}
		return result, s.Commands.Run(ctx, infraDir, "docker", append([]string{"compose"}, args...)...)
	case domain.CaddyRestart:
		fmt.Fprintln(s.out(), s.title("Restarting Caddy container..."))
		if err := s.ensureInfra(infraDir); err != nil {
			return result, err
		}
		result.Command = Command{Name: "docker", Args: []string{"compose", "restart", "caddy"}}
		return result, s.Commands.Run(ctx, infraDir, "docker", "compose", "restart", "caddy")
	default:
		if err := s.ensureInfra(infraDir); err != nil {
			return result, err
		}
		result.Command = Command{Name: "docker", Args: []string{"compose", "logs", "-f", "caddy"}}
		return result, s.Commands.Stream(ctx, infraDir, "docker", "compose", "logs", "-f", "caddy")
	}
}

// Portal reports the local developer URLs.
func (s *Service) Portal(_ context.Context) PortalResult {
	endpoints := domain.Endpoints()
	result := PortalResult{URL: domain.PortalURL, Endpoints: endpoints}

	fmt.Fprintln(s.out(), s.title("Orbit Local Developer Portal"))
	for i, endpoint := range endpoints {
		label := endpoint.Name + ":"
		if endpoint.Name == "Developer Portal" {
			label = "Portal URL:"
		}
		icon := s.UI.Info
		value := s.subtle(endpoint.URL)
		if i == 0 {
			icon = s.UI.OK
			value = s.bold(endpoint.URL)
		}
		fmt.Fprintf(s.out(), "  %s  %-16s%s\n", icon, s.bold(label), value)
	}
	return result
}

// Logs streams container logs. Plan resolves the command without running it.
func (s *Service) Logs(ctx context.Context, in LogsInput) (LogsResult, error) {
	result := LogsResult{WorkspaceRoot: in.WorkspaceRoot, Services: in.Services}
	infraDir := s.Files.InfraDir(in.WorkspaceRoot)
	result.InfraDir = infraDir
	if err := s.ensureInfra(infraDir); err != nil {
		return result, err
	}

	args := []string{"compose", "logs", "-f"}
	args = append(args, in.Services...)
	result.Command = Command{Name: "docker", Args: args}
	result.Streaming = !in.Plan
	if in.Plan {
		return result, nil
	}
	return result, s.Commands.Stream(ctx, infraDir, "docker", args...)
}

func (s *Service) ensureInfra(infraDir string) error {
	if !s.Files.IsDir(infraDir) {
		return clierr.Failed(fmt.Sprintf("orbit-infra directory not found at %s. Ensure orbit-infra is cloned", infraDir))
	}
	return nil
}

func (s *Service) probeServices(ctx context.Context, timeout time.Duration) []domain.ServiceStatus {
	coreServices := domain.CoreServices()
	results := make([]domain.ServiceStatus, len(coreServices))
	var wg sync.WaitGroup

	for i, svc := range coreServices {
		wg.Add(1)
		go func(idx int, core domain.CoreService) {
			defer wg.Done()
			ready := s.wait(ctx, core.Host, core.Port, timeout)
			effectivePort := core.Port
			if !ready && core.FallbackPort > 0 {
				if s.wait(ctx, core.Host, core.FallbackPort, timeout) {
					ready = true
					effectivePort = core.FallbackPort
				}
			}
			msg := "Ready"
			if !ready {
				msg = "Unresponsive / Offline"
			}
			results[idx] = domain.ServiceStatus{
				Name:    core.Name,
				Host:    core.Host,
				Port:    effectivePort,
				URL:     core.URL,
				Ready:   ready,
				Message: msg,
			}
		}(i, svc)
	}

	wg.Wait()
	return results
}

func (s *Service) wait(ctx context.Context, host string, port int, timeout time.Duration) bool {
	return s.Probe.Wait(ctx, host, port, timeout)
}

func (s *Service) renderEndpointsBanner(statuses []domain.ServiceStatus) {
	var sb strings.Builder

	sb.WriteString(s.bold("Orbit Local Developer Endpoints") + "\n\n")

	for _, ep := range domain.Endpoints() {
		fmt.Fprintf(&sb, "  %s %-18s %s\n", s.UI.Arrow, s.bold(ep.Name+":"), s.subtle(ep.URL))
	}

	if len(statuses) > 0 {
		sb.WriteString("\n" + s.bold("Service Readiness:") + "\n")
		for _, status := range statuses {
			icon := s.UI.OK
			statusText := s.success("READY")
			if !status.Ready {
				icon = s.UI.Warn
				statusText = s.warning("NOT READY")
			}
			fmt.Fprintf(&sb, "  %s  %-16s %-12s (:%d)\n", icon, status.Name, statusText, status.Port)
		}
	}

	fmt.Fprintln(s.out(), s.card(sb.String()))
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) title(text string) string {
	if s.UI.Title == nil {
		return text
	}
	return s.UI.Title(text)
}

func (s *Service) bold(text string) string {
	if s.UI.Bold == nil {
		return text
	}
	return s.UI.Bold(text)
}

func (s *Service) subtle(text string) string {
	if s.UI.Subtle == nil {
		return text
	}
	return s.UI.Subtle(text)
}

func (s *Service) info(text string) string {
	if s.UI.InfoText == nil {
		return text
	}
	return s.UI.InfoText(text)
}

func (s *Service) success(text string) string {
	if s.UI.Success == nil {
		return text
	}
	return s.UI.Success(text)
}

func (s *Service) warning(text string) string {
	if s.UI.Warning == nil {
		return text
	}
	return s.UI.Warning(text)
}

func (s *Service) card(text string) string {
	if s.UI.Card == nil {
		return text
	}
	return s.UI.Card(text)
}
