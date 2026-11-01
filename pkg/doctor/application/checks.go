package application

import (
	"context"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
)

// Timeouts for diagnostic probes: toolchain versions are slower than the
// optional tool and SSH probes.
const (
	DefaultCommandTimeout = 5 * time.Second
	ShortCommandTimeout   = 3 * time.Second
)

// hostCheck validates the local machine against Orbit host constraints.
type hostCheck struct {
	host Host
}

func (c hostCheck) Name() string { return "host" }

func (c hostCheck) Run(context.Context) []domain.DiagnosticResult {
	return []domain.DiagnosticResult{domain.EvaluateHost(c.host.Report())}
}

// gitCheck checks that the Git CLI is installed.
type gitCheck struct {
	probe Probe
}

func (c gitCheck) Name() string { return "git" }

func (c gitCheck) Run(ctx context.Context) []domain.DiagnosticResult {
	out, err := c.probe.Run(ctx, DefaultCommandTimeout, "git", "version")
	return []domain.DiagnosticResult{domain.EvaluateGitVersion(out.Output, err)}
}

// dockerCheck checks the Docker daemon and Compose v2.
type dockerCheck struct {
	probe Probe
	host  Host
}

func (c dockerCheck) Name() string { return "docker" }

func (c dockerCheck) Run(ctx context.Context) []domain.DiagnosticResult {
	infoOut, infoErr := c.probe.Run(ctx, DefaultCommandTimeout, "docker", "info")
	_, lookErr := c.host.LookPath("docker")
	results := []domain.DiagnosticResult{domain.EvaluateDockerDaemon(infoOut.Output, infoErr, lookErr == nil)}

	composeOut, composeErr := c.probe.Run(ctx, DefaultCommandTimeout, "docker", "compose", "version")
	if composeErr != nil {
		composeOut, composeErr = c.probe.Run(ctx, DefaultCommandTimeout, "docker-compose", "version")
	}
	return append(results, domain.EvaluateDockerCompose(composeOut.Output, composeErr))
}

// sshCheck checks the SSH agent, and with --remote the GitHub SSH endpoint.
type sshCheck struct {
	probe          Probe
	host           Host
	remote         bool
	acceptHostKeys bool
}

func (c sshCheck) Name() string { return "ssh" }

func (c sshCheck) Run(ctx context.Context) []domain.DiagnosticResult {
	var results []domain.DiagnosticResult

	sshAuthSock := c.host.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock == "" {
		results = append(results, domain.DiagnosticResult{
			Category:      domain.CategoryAuth,
			Name:          "SSH Agent",
			Status:        domain.StatusWarning,
			Message:       "SSH agent is not running (SSH_AUTH_SOCK unset)",
			FixSuggestion: "Start SSH agent: eval $(ssh-agent -s) && ssh-add.",
		})
	} else if !c.remote {
		results = append(results, domain.DiagnosticResult{Category: domain.CategoryAuth, Name: "SSH Agent", Status: domain.StatusUnverified, Message: "SSH_AUTH_SOCK set; identities unverified (local mode)"})
	} else {
		addOut, addErr := c.probe.Run(ctx, ShortCommandTimeout, "ssh-add", "-l")
		switch {
		case addErr == nil:
			results = append(results, domain.DiagnosticResult{Category: domain.CategoryAuth, Name: "SSH Agent", Status: domain.StatusOK, Message: "SSH agent active with loaded identities"})
		case strings.Contains(addOut.Output, "The agent has no identities"):
			results = append(results, domain.DiagnosticResult{
				Category:      domain.CategoryAuth,
				Name:          "SSH Agent",
				Status:        domain.StatusWarning,
				Message:       "SSH agent running but no keys loaded",
				FixSuggestion: "Load your SSH key: ssh-add ~/.ssh/id_ed25519.",
			})
		default:
			results = append(results, domain.DiagnosticResult{Category: domain.CategoryAuth, Name: "SSH Agent", Status: domain.StatusOK, Message: "SSH agent socket connected"})
		}
	}

	if !c.remote {
		return results
	}

	hostKeyMode := "yes"
	if c.acceptHostKeys {
		hostKeyMode = "accept-new"
	}
	args := []string{
		"-F", "/dev/null",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=3",
		"-o", "UpdateHostKeys=no",
		"-o", "ControlMaster=no",
		"-o", "StrictHostKeyChecking=" + hostKeyMode,
		"-T",
		"git@github.com",
	}
	out, err := c.probe.Run(ctx, ShortCommandTimeout, "ssh", args...)
	return append(results, domain.EvaluateGitHubSSH(out.Output, err))
}

// portsCheck scans the standard dev port range for unexpected bindings.
type portsCheck struct {
	ports Ports
}

func (c portsCheck) Name() string { return "ports" }

func (c portsCheck) Run(context.Context) []domain.DiagnosticResult {
	return []domain.DiagnosticResult{domain.EvaluatePortScan(c.ports.Scan(10000, 10300))}
}

// optionalToolsCheck reports on Caddy and Typst.
type optionalToolsCheck struct {
	probe Probe
}

func (c optionalToolsCheck) Name() string { return "optional-tools" }

func (c optionalToolsCheck) Run(ctx context.Context) []domain.DiagnosticResult {
	caddyOut, caddyErr := c.probe.Run(ctx, ShortCommandTimeout, "caddy", "version")
	typstOut, typstErr := c.probe.Run(ctx, ShortCommandTimeout, "typst", "--version")
	return []domain.DiagnosticResult{
		domain.EvaluateCaddyVersion(caddyOut.Output, caddyErr),
		domain.EvaluateTypstVersion(typstOut.Output, typstErr),
	}
}

// staticCheck contributes fixed results, used for the explicit "unverified"
// lines local mode prints instead of probing.
type staticCheck struct {
	name    string
	results []domain.DiagnosticResult
}

func (c staticCheck) Name() string { return c.name }

func (c staticCheck) Run(context.Context) []domain.DiagnosticResult {
	return c.results
}
