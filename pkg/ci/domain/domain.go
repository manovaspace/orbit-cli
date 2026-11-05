// Package domain holds the CI runner and secret rules that need no I/O.
package domain

import (
	"fmt"
	"net/url"
	"strings"
)

// DeployKeySecret is the repository secret that carries a per-repo deploy key.
const DeployKeySecret = "DEPLOY_SSH_KEY"

// RunnerLabelVPS is the label private repositories pin through CI_RUNNER=vps.
const RunnerLabelVPS = "vps"

// RunnerMode selects where private repositories run CI.
type RunnerMode string

const (
	// ModeVPS points CI at the self-hosted runner pool.
	ModeVPS RunnerMode = "vps"
	// ModeGitHub removes the override so CI runs on GitHub-hosted runners.
	ModeGitHub RunnerMode = "github"
	// ModeStatus reports the current state without changing it.
	ModeStatus RunnerMode = "status"
)

// ParseRunnerMode validates a `ci runner` mode argument.
func ParseRunnerMode(value string) (RunnerMode, error) {
	switch RunnerMode(value) {
	case ModeVPS:
		return ModeVPS, nil
	case ModeGitHub:
		return ModeGitHub, nil
	case ModeStatus:
		return ModeStatus, nil
	default:
		return "", fmt.Errorf("unknown runner mode %q: use vps, github or status", value)
	}
}

// RunnerLabel is one label of an organization runner.
type RunnerLabel struct {
	Name string `json:"name"`
}

// Runner is one organization-level self-hosted runner.
type Runner struct {
	Name   string        `json:"name"`
	Status string        `json:"status"`
	Busy   bool          `json:"busy"`
	Labels []RunnerLabel `json:"labels"`
}

// HasLabel reports whether the runner carries label.
func (r Runner) HasLabel(label string) bool {
	for _, l := range r.Labels {
		if l.Name == label {
			return true
		}
	}
	return false
}

// OnlineWithLabel reports whether any runner is online and carries label.
func OnlineWithLabel(runners []Runner, label string) bool {
	for _, r := range runners {
		if r.Status == "online" && r.HasLabel(label) {
			return true
		}
	}
	return false
}

// Organization returns the owner shared by every owner/name slug in repos.
func Organization(repos []string) (string, bool) {
	org := ""
	for _, slug := range repos {
		owner, _, ok := strings.Cut(slug, "/")
		if !ok || owner == "" {
			return "", false
		}
		if org == "" {
			org = owner
			continue
		}
		if owner != org {
			return "", false
		}
	}
	return org, org != ""
}

// Status reports the organization runners and the CI_RUNNER variable of each
// repository that has one.
type Status struct {
	Runners []Runner          `json:"runners"`
	Vars    map[string]string `json:"vars"`
}

// GitHubSlug turns a GitHub remote URL into owner/name. Other hosts and
// unparsable values return false.
func GitHubSlug(remote string) (string, bool) {
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

// IsMissingVariable reports whether a `gh variable delete` failure means the
// variable was already absent.
func IsMissingVariable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") || strings.Contains(message, "http 404")
}

// ParseSource parses a KEY=value secret file. Blank lines and lines starting
// with # are ignored.
func ParseSource(raw, path string) (map[string]string, error) {
	source := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s: line %q is not KEY=value", path, line)
		}
		source[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	return source, nil
}
