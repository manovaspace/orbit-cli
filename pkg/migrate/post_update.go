package migrate

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/alias"
	"github.com/manovaspace/orbit-cli/pkg/worker"
)

const (
	// DefaultPostUpdateStateFile is the default user-level state file path for post-update migrations.
	DefaultPostUpdateStateFile = "~/.manova/state.json"
)

// PostUpdateContext encapsulates runtime context and I/O streams for post-update migrations.
type PostUpdateContext struct {
	Interactive bool
	In          io.Reader
	Out         io.Writer
	ExecPath    string
	PrevVersion string
	NewVersion  string
	StatePath   string // Optional custom state file path for testing or custom environments.
}

// PostUpdateMigration defines a single versioned micro-migration applied after a binary update.
type PostUpdateMigration struct {
	ID          string                                                             `json:"id" yaml:"id"`
	Description string                                                             `json:"description" yaml:"description"`
	Run         func(ctx *PostUpdateContext) (applied bool, msg string, err error) `json:"-" yaml:"-"`
}

// GetPostUpdateMigrations returns the canonical ordered list of post-update micro-migrations.
func GetPostUpdateMigrations() []PostUpdateMigration {
	return []PostUpdateMigration{
		{
			ID:          "001_upgrade_systemd_worker",
			Description: "Upgrade and restart systemd background worker",
			Run:         UpgradeSystemdWorker,
		},
		{
			ID:          "002_ensure_shell_completion",
			Description: "Ensure shell completion hooks are installed in user shell profile",
			Run:         EnsureShellCompletion,
		},
		{
			ID:          "003_prompt_m_alias",
			Description: "Prompt user to configure 'm' shortcut alias for manova",
			Run:         PromptMAlias,
		},
	}
}

// UpgradeSystemdWorker upgrades and restarts the systemd user service worker daemon if on Linux and functional.
func UpgradeSystemdWorker(ctx *PostUpdateContext) (bool, string, error) {
	if runtime.GOOS != "linux" || !worker.IsSystemdFunctional() {
		return true, "systemd worker not applicable or functional on this environment; skipped", nil
	}

	execPath := ctx.ExecPath
	if execPath == "" {
		if exe, err := os.Executable(); err == nil && exe != "" {
			execPath = exe
		} else {
			execPath = "manova"
		}
	}

	mode, err := worker.StartDaemon(execPath)
	if err != nil {
		return false, "", fmt.Errorf("failed to restart worker daemon: %w", err)
	}

	return true, fmt.Sprintf("systemd worker daemon restarted (%s)", mode), nil
}

// EnsureShellCompletion ensures shell completion hooks are installed in the user's RC file.
func EnsureShellCompletion(ctx *PostUpdateContext) (bool, string, error) {
	rcPath, err := alias.InstallShellCompletion(false)
	if err != nil {
		return false, "", fmt.Errorf("failed to install shell completion: %w", err)
	}
	if rcPath == "" {
		return true, "no shell profile detected; skipped", nil
	}
	return true, fmt.Sprintf("shell completion configured in %s", rcPath), nil
}

// PromptMAlias prompts the user in interactive mode to configure the 'm' shortcut alias if not taken.
func PromptMAlias(ctx *PostUpdateContext) (bool, string, error) {
	if !ctx.Interactive {
		return false, "non-interactive session; alias prompt skipped", nil
	}

	taken, reason := alias.IsCommandTaken("m")
	if taken {
		return true, fmt.Sprintf("shortcut 'm' already in use (%s); skipped", reason), nil
	}

	in := ctx.In
	if in == nil {
		in = os.Stdin
	}
	out := ctx.Out
	if out == nil {
		out = os.Stdout
	}

	fmt.Fprintf(out, "? Set 'm' as a short shell alias for 'manova'? [Y/n] ")
	scanner := bufio.NewScanner(in)
	if scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.ToLower(strings.TrimSpace(line))
		if trimmed == "n" || trimmed == "no" {
			return true, "Shortcut 'm' declined by user", nil
		}

		rcPath, err := alias.AddShellAlias("m", "manova")
		if err != nil {
			return false, "", err
		}
		return true, fmt.Sprintf("Added alias m=\"manova\" to %s", filepath.Base(rcPath)), nil
	}
	if err := scanner.Err(); err != nil {
		return false, "", fmt.Errorf("failed to read response: %w", err)
	}

	return true, "no input received; skipped", nil
}

// resolvePostUpdateStatePath expands ~ and resolves the state.json path.
func resolvePostUpdateStatePath(path string) string {
	if path == "" {
		path = DefaultPostUpdateStateFile
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// RunPostUpdateMigrations executes all pending post-update micro-migrations, persisting state in state.json.
func RunPostUpdateMigrations(ctx *PostUpdateContext) ([]MigrationResult, error) {
	if ctx == nil {
		ctx = &PostUpdateContext{
			Interactive: false,
			In:          os.Stdin,
			Out:         os.Stdout,
		}
	}
	if ctx.In == nil {
		ctx.In = os.Stdin
	}
	if ctx.Out == nil {
		ctx.Out = os.Stdout
	}

	statePath := resolvePostUpdateStatePath(ctx.StatePath)
	engine := NewEngine("", statePath)

	state, err := engine.LoadState()
	if err != nil {
		return nil, fmt.Errorf("failed to load post-update migration state: %w", err)
	}

	appliedMap := make(map[string]bool, len(state.Applied))
	for _, r := range state.Applied {
		appliedMap[r.ID] = true
	}

	migrations := GetPostUpdateMigrations()
	var results []MigrationResult

	for _, m := range migrations {
		if appliedMap[m.ID] {
			continue
		}

		res := MigrationResult{
			ID:          m.ID,
			Description: m.Description,
		}

		if m.Run != nil {
			applied, msg, err := m.Run(ctx)
			if err != nil {
				res.Success = false
				res.Error = err.Error()
				results = append(results, res)
				return results, fmt.Errorf("post-update migration %s failed: %w", m.ID, err)
			}

			if !applied {
				res.Success = true
				res.Skipped = true
				results = append(results, res)
				continue
			}

			desc := m.Description
			if msg != "" {
				desc = msg
			}

			res.Success = true
			res.Skipped = false
			res.Description = desc
			results = append(results, res)

			state.Applied = append(state.Applied, AppliedMigrationRecord{
				ID:          m.ID,
				AppliedAt:   time.Now().UTC(),
				Description: desc,
			})
			appliedMap[m.ID] = true

			if err := engine.SaveState(state); err != nil {
				return results, fmt.Errorf("failed to save migration state after %s: %w", m.ID, err)
			}
		}
	}

	if ctx.NewVersion != "" {
		state.Version = ctx.NewVersion
		_ = engine.SaveState(state)
	}

	return results, nil
}
