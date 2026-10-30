package ci

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
)

// Exec runs a command and returns its trimmed standard output. Tests inject a
// fake; production uses a real process.
type Exec func(name string, args ...string) (string, error)

// ExecStdin is Exec with a string written to the command's standard input.
// Secret values travel this way so they never appear in argv.
type ExecStdin func(stdin, name string, args ...string) (string, error)

// Status reports the standby runner service state and the CI_RUNNER variable
// of each repository that has one.
type Status struct {
	Service string
	Vars    map[string]string
}

// PrivateRepos returns owner/name for the GitHub repositories in repos whose
// visibility is PRIVATE. Non-GitHub remotes are skipped.
func PrivateRepos(repos []manifest.RepoTarget, run Exec) ([]string, error) {
	var private []string
	for _, repo := range repos {
		slug, ok := githubSlug(repo.RemoteURL)
		if !ok {
			continue
		}
		out, err := run("gh", "repo", "view", slug, "--json", "visibility", "--jq", ".visibility")
		if err != nil {
			return nil, fmt.Errorf("repo visibility for %s: %w", slug, err)
		}
		if strings.TrimSpace(out) == "PRIVATE" {
			private = append(private, slug)
		}
	}
	return private, nil
}

// SetRunner switches the CI runner for repos. "vps" starts the standby runner
// before pointing CI_RUNNER at it; "github" removes the variable (missing is
// fine) and stops the runner afterwards.
func SetRunner(mode string, repos []string, run Exec) error {
	switch mode {
	case "vps":
		if _, err := run("sudo", "systemctl", "start", "gh-ci"); err != nil {
			return fmt.Errorf("start gh-ci: %w", err)
		}
		for _, repo := range repos {
			if _, err := run("gh", "variable", "set", "CI_RUNNER", "--body", "vps", "-R", repo); err != nil {
				return fmt.Errorf("set CI_RUNNER for %s: %w", repo, err)
			}
		}
		return nil
	case "github":
		for _, repo := range repos {
			if _, err := run("gh", "variable", "delete", "CI_RUNNER", "-R", repo); err != nil &&
				!strings.Contains(strings.ToLower(err.Error()), "not found") {
				return fmt.Errorf("delete CI_RUNNER for %s: %w", repo, err)
			}
		}
		if _, err := run("sudo", "systemctl", "stop", "gh-ci"); err != nil {
			return fmt.Errorf("stop gh-ci: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown runner mode %q", mode)
	}
}

// RunnerStatus reports whether the standby runner is active and which
// repositories still pin CI_RUNNER.
func RunnerStatus(repos []string, run Exec) (Status, error) {
	status := Status{Vars: map[string]string{}}
	out, err := run("systemctl", "is-active", "gh-ci")
	if err != nil && strings.TrimSpace(out) == "" {
		return status, fmt.Errorf("gh-ci status: %w", err)
	}
	status.Service = strings.TrimSpace(out)
	for _, repo := range repos {
		value, err := run("gh", "variable", "get", "CI_RUNNER", "-R", repo, "--json", "value", "--jq", ".value")
		if err != nil {
			continue
		}
		status.Vars[repo] = strings.TrimSpace(value)
	}
	return status, nil
}

// githubSlug turns a GitHub remote URL into owner/name. Other hosts and
// unparsable values return false.
func githubSlug(remote string) (string, bool) {
	switch {
	case strings.HasPrefix(remote, "git@"):
		hostPath := strings.TrimPrefix(remote, "git@")
		host, path, ok := strings.Cut(hostPath, ":")
		if !ok || host != "github.com" {
			return "", false
		}
		return strings.TrimSuffix(strings.Trim(path, "/"), ".git"), true
	case strings.HasPrefix(remote, "https://"), strings.HasPrefix(remote, "ssh://"):
		u, err := url.Parse(remote)
		if err != nil || u.Host != "github.com" {
			return "", false
		}
		return strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), true
	default:
		return "", false
	}
}
