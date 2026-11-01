// Package application implements the doctor diagnostics and healing use cases
// over probe, host, filesystem and port ports.
package application

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

// ProbeResult is the combined output of one external probe.
type ProbeResult struct {
	Output   string
	ExitCode int
}

// Probe runs external diagnostic programs. The caller supplies the timeout so
// short probes stay short.
type Probe interface {
	Run(ctx context.Context, timeout time.Duration, name string, args ...string) (ProbeResult, error)
}

// FileInfo is the subset of file metadata the checks need.
type FileInfo struct {
	IsDir bool
}

// Host is the local machine the checks inspect: host readiness, paths,
// environment and files.
type Host interface {
	Report() host.Report
	LookPath(name string) (string, error)
	Getwd() (string, error)
	ReadFile(path string) ([]byte, error)
	Stat(path string) (FileInfo, error)
	Lstat(path string) (FileInfo, error)
	Getenv(name string) string
}

// Ports probes TCP listener availability.
type Ports interface {
	Scan(start, end int) map[int]bool
}

// Manifest loads workspace.yaml for the workspace integrity check.
type Manifest interface {
	Load(path string) (*manifest.WorkspaceManifest, error)
}

// ExtraCheck appends diagnostics owned by another command group, such as the
// R2 asset check. remote selects remote probes and fix authorizes repairs.
type ExtraCheck func(ctx context.Context, report *domain.DoctorReport, remote, fix bool)

// Options selects remote probes, host-key acceptance, repairs and the rerun
// semantics the JSON path has always used for asset repairs.
type Options struct {
	Remote         bool
	AcceptHostKeys bool
	Fix            bool
	JSON           bool
	// WorkspaceRoot and ManifestPath locate workspace integrity checks.
	WorkspaceRoot string
	ManifestPath  string
}

// Result is one diagnostics run.
type Result struct {
	Report *domain.DoctorReport
}

// FixResult is the outcome of an explicit --fix run.
type FixResult struct {
	Report      *domain.DoctorReport
	Ran         bool
	HealResults []domain.HealResult
}

// Service is the diagnostics use-case layer.
type Service struct {
	Probe    Probe
	Host     Host
	Ports    Ports
	Manifest Manifest
	Healers  *domain.HealerRegistry
	Extra    ExtraCheck

	Out io.Writer
	UI  UI
}

// Diagnose runs the selected checks. It never heals: only Heal with an
// explicit Fix option runs recipes.
func (s *Service) Diagnose(ctx context.Context, opts Options) Result {
	registry := domain.NewRegistry()
	registry.Register(hostCheck{host: s.Host})
	registry.Register(gitCheck{probe: s.Probe})
	registry.Register(toolchainCheck{probe: s.Probe, host: s.Host})
	if opts.Remote {
		registry.Register(dockerCheck{probe: s.Probe, host: s.Host})
		registry.Register(sshCheck{probe: s.Probe, host: s.Host, remote: true, acceptHostKeys: opts.AcceptHostKeys})
		registry.Register(portsCheck{ports: s.Ports})
	} else {
		registry.Register(staticCheck{name: "docker-local", results: []domain.DiagnosticResult{
			{Category: domain.CategoryContainer, Name: "Docker CLI", Status: domain.StatusUnverified, Message: "Daemon connectivity unverified (local mode)"},
		}})
		registry.Register(sshCheck{probe: s.Probe, host: s.Host})
		registry.Register(staticCheck{name: "ports-local", results: []domain.DiagnosticResult{
			{Category: domain.CategoryPorts, Name: "Listener probe", Status: domain.StatusUnverified, Message: "Listener availability unverified (local mode)"},
			{Category: domain.CategoryNetwork, Name: "Remote probes", Status: domain.StatusUnverified, Message: "Remote connectivity unverified; select --remote to probe"},
		}})
	}
	registry.Register(optionalToolsCheck{probe: s.Probe})
	if opts.ManifestPath != "" {
		registry.Register(workspaceCheck{host: s.Host, manifest: s.Manifest, root: opts.WorkspaceRoot, path: opts.ManifestPath})
	}

	report := registry.Run(ctx)
	if s.Extra != nil {
		s.Extra(ctx, report, opts.Remote, opts.Fix)
	}
	return Result{Report: report}
}

// Heal runs the matching recipes after an explicit --fix, then re-runs
// diagnostics. Without Options.Fix it does nothing.
func (s *Service) Heal(ctx context.Context, report *domain.DoctorReport, opts Options) (FixResult, error) {
	result := FixResult{Report: report}
	if !opts.Fix || s.Healers == nil {
		return result, nil
	}
	healers := s.Healers.FindHealers(report.Results)
	if len(healers) == 0 {
		return result, nil
	}

	fmt.Fprintf(s.out(), "\n%s\n", s.header("── Auto-Healing Toolchains & Dependencies ─────────────────"))
	result.Ran = true
	result.HealResults, _ = RunHealers(ctx, healers, s.progress)

	rerun := opts
	// The JSON path historically re-ran asset diagnostics with repairs
	// enabled; the human path re-ran them read-only.
	rerun.Fix = opts.JSON
	result.Report = s.Diagnose(ctx, rerun).Report
	return result, nil
}

// RunHealers executes healers in order with error isolation and reports each
// step through progress.
func RunHealers(ctx context.Context, healers []domain.Healer, progress func(name, status string)) ([]domain.HealResult, error) {
	if progress == nil {
		progress = func(string, string) {}
	}
	var results []domain.HealResult
	for _, healer := range healers {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		start := time.Now()
		progress(healer.Name(), "Starting auto-heal...")
		err := healer.Heal(ctx, func(step string) { progress(healer.Name(), step) })
		duration := time.Since(start)
		if err != nil {
			progress(healer.Name(), "Failed: "+err.Error())
			results = append(results, domain.HealResult{
				HealerName: healer.Name(),
				Success:    false,
				Message:    "Failed to heal " + healer.Name() + ": " + err.Error(),
				Error:      err,
				Duration:   duration,
			})
			continue
		}
		progress(healer.Name(), "Completed successfully")
		results = append(results, domain.HealResult{
			HealerName: healer.Name(),
			Success:    true,
			Message:    "Successfully installed/configured " + healer.Name(),
			Duration:   duration,
		})
	}
	return results, nil
}

func (s *Service) progress(name, status string) {
	out := s.out()
	switch {
	case status == "Completed successfully":
		fmt.Fprintf(out, "  %s  %-24s %s\n", s.UI.OK, s.bold(name), s.success("Installed and configured successfully"))
	case len(status) >= 7 && status[:7] == "Failed:":
		fmt.Fprintf(out, "  %s  %-24s %s\n", s.UI.Error, s.bold(name), s.errorText(status))
	default:
		fmt.Fprintf(out, "  %s  %-24s %s\n", s.UI.Arrow, s.bold(name), s.subtle(status))
	}
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}

func (s *Service) bold(text string) string {
	if s.UI.Bold == nil {
		return text
	}
	return s.UI.Bold(text)
}

func (s *Service) header(text string) string {
	if s.UI.Header == nil {
		return text
	}
	return s.UI.Header(text)
}

func (s *Service) success(text string) string {
	if s.UI.Success == nil {
		return text
	}
	return s.UI.Success(text)
}

func (s *Service) errorText(text string) string {
	if s.UI.ErrorText == nil {
		return text
	}
	return s.UI.ErrorText(text)
}

func (s *Service) subtle(text string) string {
	if s.UI.Subtle == nil {
		return text
	}
	return s.UI.Subtle(text)
}
