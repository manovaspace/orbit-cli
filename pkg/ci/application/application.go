// Package application implements the CI runner and secret use cases over a
// command port and a file port, keeping the CLI commands thin.
package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/ci/domain"
	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

// Command runs an external program and returns its standard output. stdin is
// written to the process when non-empty; secret values travel this way so they
// never appear in argv.
type Command interface {
	Run(ctx context.Context, stdin, dir, name string, args ...string) (string, error)
}

// Files reads the secret source and per-repository deploy keys.
type Files interface {
	ReadFile(path string) ([]byte, error)
	// Perm returns the unix permission bits of path.
	Perm(path string) (uint32, error)
}

// Service is the CI use-case layer.
type Service struct {
	Commands Command
	Files    Files
	Out      io.Writer
}

// SetResult is what `ci runner vps|github` changed.
type SetResult struct {
	Mode         string   `json:"mode"`
	Repositories []string `json:"repositories"`
}

// SyncRepo is one repository whose secrets were set.
type SyncRepo struct {
	Name    string `json:"name"`
	Secrets int    `json:"secrets"`
}

// SyncResult is what `ci secrets sync` produced.
type SyncResult struct {
	Repositories []SyncRepo `json:"repositories"`
}

// PrivateRepos returns owner/name for the GitHub repositories in repos whose
// visibility is PRIVATE. Non-GitHub remotes are skipped.
func (s *Service) PrivateRepos(ctx context.Context, repos []manifest.RepoTarget) ([]string, error) {
	var private []string
	for _, repo := range repos {
		slug, ok := domain.GitHubSlug(repo.RemoteURL)
		if !ok {
			continue
		}
		out, err := s.run(ctx, "", "gh", "repo", "view", slug, "--json", "visibility", "--jq", ".visibility")
		if err != nil {
			return nil, fmt.Errorf("repo visibility for %s: %w", slug, err)
		}
		if strings.TrimSpace(out) == "PRIVATE" {
			private = append(private, slug)
		}
	}
	return private, nil
}

// SetRunner switches where CI runs for repos. "vps" points CI_RUNNER at the
// organization's self-hosted pool and refuses to switch when no online runner
// carries the label; "github" removes the variable (missing is fine) so the
// workflow falls back to ubuntu-latest. Neither mode touches the runner host:
// the pool is always on.
func (s *Service) SetRunner(ctx context.Context, mode domain.RunnerMode, repos []string) error {
	switch mode {
	case domain.ModeVPS:
		// Nothing to pin: skip the pool check rather than fail on an empty
		// organization name.
		if len(repos) > 0 {
			org, ok := domain.Organization(repos)
			if !ok {
				return clierr.Usage("runner vps needs repositories from a single GitHub organization")
			}
			runners, err := s.runners(ctx, org)
			if err != nil {
				return err
			}
			if !domain.OnlineWithLabel(runners, domain.RunnerLabelVPS) {
				return fmt.Errorf("no online self-hosted runner with label %q in %s: register the pool before pinning CI_RUNNER", domain.RunnerLabelVPS, org)
			}
		}
		for _, repo := range repos {
			if _, err := s.run(ctx, "", "gh", "variable", "set", "CI_RUNNER", "--body", "vps", "-R", repo); err != nil {
				return fmt.Errorf("set CI_RUNNER for %s: %w", repo, err)
			}
		}
	case domain.ModeGitHub:
		for _, repo := range repos {
			if _, err := s.run(ctx, "", "gh", "variable", "delete", "CI_RUNNER", "-R", repo); err != nil {
				if !domain.IsMissingVariable(err) {
					return fmt.Errorf("delete CI_RUNNER for %s: %w", repo, err)
				}
			}
		}
	default:
		return clierr.Usage(fmt.Sprintf("unknown runner mode %q", mode))
	}

	fmt.Fprintf(s.out(), "CI runner set to %s for %d private repositories.\n", mode, len(repos))
	return nil
}

// RunnerStatus reports the organization runners and which repositories still
// pin CI_RUNNER.
func (s *Service) RunnerStatus(ctx context.Context, repos []string) (domain.Status, error) {
	status := domain.Status{Runners: []domain.Runner{}, Vars: map[string]string{}}
	if org, ok := domain.Organization(repos); ok {
		runners, err := s.runners(ctx, org)
		if err != nil {
			return status, err
		}
		status.Runners = runners
	}
	for _, repo := range repos {
		value, err := s.run(ctx, "", "gh", "variable", "get", "CI_RUNNER", "-R", repo, "--json", "value", "--jq", ".value")
		if err != nil {
			continue
		}
		status.Vars[repo] = strings.TrimSpace(value)
	}
	return status, nil
}

// runners lists the organization's self-hosted runners. The default page size
// is 30; 100 is the API maximum and exceeds any pool this workspace runs.
func (s *Service) runners(ctx context.Context, org string) ([]domain.Runner, error) {
	out, err := s.run(ctx, "", "gh", "api", fmt.Sprintf("orgs/%s/actions/runners?per_page=100", org))
	if err != nil {
		return nil, fmt.Errorf("list runners for %s: %w", org, err)
	}
	var payload struct {
		Runners []domain.Runner `json:"runners"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return nil, fmt.Errorf("decode runners for %s: %w", org, err)
	}
	return payload.Runners, nil
}

// RenderStatus writes the human lines of `ci runner status`.
func (s *Service) RenderStatus(status domain.Status, repos []string) {
	out := s.out()
	for _, runner := range status.Runners {
		state := "idle"
		if runner.Busy {
			state = "busy"
		}
		labels := make([]string, 0, len(runner.Labels))
		for _, label := range runner.Labels {
			labels = append(labels, label.Name)
		}
		fmt.Fprintf(out, "runner %s: %s %s labels=%s\n", runner.Name, runner.Status, state, strings.Join(labels, ","))
	}
	for _, repo := range repos {
		variable := status.Vars[repo]
		if variable == "" {
			variable = "(unset)"
		}
		fmt.Fprintf(out, "  %s CI_RUNNER=%s\n", repo, variable)
	}
}

// Sync selects repos that declare ci_secrets and sets every secret for each.
// wanted selects a single repository by name; an empty wanted selects all.
func (s *Service) Sync(ctx context.Context, repos []manifest.RepoTarget, wanted string, source map[string]string, keysDir string) (SyncResult, error) {
	var selected []manifest.RepoTarget
	for _, repo := range repos {
		if len(repo.CISecrets) == 0 {
			continue
		}
		if wanted != "" && repo.Name != wanted {
			continue
		}
		selected = append(selected, repo)
	}
	if len(selected) == 0 {
		return SyncResult{}, clierr.Usage(fmt.Sprintf("no repository with ci_secrets matches %q", wanted))
	}

	result := SyncResult{Repositories: []SyncRepo{}}
	out := s.out()
	for _, repo := range selected {
		count, err := s.SyncSecrets(ctx, repo, source, keysDir)
		if err != nil {
			return result, err
		}
		result.Repositories = append(result.Repositories, SyncRepo{Name: repo.Name, Secrets: count})
		fmt.Fprintf(out, "%s: set %d secret(s)\n", repo.Name, count)
	}
	return result, nil
}

// SyncSecrets sets every secret listed in repo.CISecrets. Values come from
// source, except DEPLOY_SSH_KEY which is read from keysDir/<repo name>. All
// values are validated before anything is set, so a failure leaves the
// repository untouched. Values are written to stdin, never argv.
func (s *Service) SyncSecrets(ctx context.Context, repo manifest.RepoTarget, source map[string]string, keysDir string) (int, error) {
	slug, ok := domain.GitHubSlug(repo.RemoteURL)
	if !ok {
		return 0, fmt.Errorf("repository %s has no GitHub remote", repo.Name)
	}

	values := make(map[string]string, len(repo.CISecrets))
	for _, name := range repo.CISecrets {
		var value string
		if name == domain.DeployKeySecret {
			key, err := s.Files.ReadFile(filepath.Join(keysDir, repo.Name))
			if err != nil {
				return 0, fmt.Errorf("secret %s for %s: %w", name, repo.Name, err)
			}
			value = string(key)
		} else {
			var found bool
			value, found = source[name]
			if !found || value == "" {
				return 0, fmt.Errorf("secret %s for %s has no value", name, repo.Name)
			}
		}
		values[name] = value
	}

	for _, name := range repo.CISecrets {
		if _, err := s.run(ctx, values[name], "gh", "secret", "set", name, "-R", slug); err != nil {
			return 0, fmt.Errorf("set secret %s for %s: %w", name, slug, err)
		}
	}
	return len(repo.CISecrets), nil
}

// LoadSource reads a KEY=value file. A file readable by group or others is
// refused.
func (s *Service) LoadSource(path string) (map[string]string, error) {
	perm, err := s.Files.Perm(path)
	if err != nil {
		return nil, err
	}
	if perm&0o077 != 0 {
		return nil, fmt.Errorf("%s must not be readable by group or others (chmod 600)", path)
	}
	raw, err := s.Files.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return domain.ParseSource(string(raw), path)
}

func (s *Service) run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	return s.Commands.Run(ctx, stdin, "", name, args...)
}

func (s *Service) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}
	return s.Out
}
