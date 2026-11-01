// Package domain holds the pull-request types and rules that need no I/O.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Repo identifies a repository on a forge.
type Repo struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// String renders the repository as "owner/name".
func (r Repo) String() string {
	return r.Owner + "/" + r.Name
}

// MarshalJSON renders a repository as the "owner/name" string.
func (r Repo) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// RepoInfo is a local checkout's view of its repository.
type RepoInfo struct {
	Repo
	CurrentBranch string `json:"currentBranch"`
	RemoteURL     string `json:"remoteURL"`
	IsGitHub      bool   `json:"isGitHub"`
}

// MarshalJSON renders the embedded repository as "owner/name" and keeps the
// checkout fields, because Repo's marshaller would otherwise take over the
// promoted method and drop them.
func (r RepoInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Repo          string `json:"repo"`
		CurrentBranch string `json:"currentBranch"`
		RemoteURL     string `json:"remoteURL"`
		IsGitHub      bool   `json:"isGitHub"`
	}{
		Repo:          r.String(),
		CurrentBranch: r.CurrentBranch,
		RemoteURL:     r.RemoteURL,
		IsGitHub:      r.IsGitHub,
	})
}

// ParseRemoteURL extracts the repository owner and name from the git remote URL
// formats in use:
//   - SSH URL: ssh://git@git.internal.manova.space/manova/handbook.git
//   - SSH with port: ssh://git@git.internal.manova.space:2222/manova/handbook.git
//   - SCP-like: git@git.internal.manova.space:manova/handbook.git
//   - HTTPS: https://git.internal.manova.space/manova/handbook.git
func ParseRemoteURL(rawURL string) (owner, repo string, err error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", "", errors.New("empty remote URL")
	}

	var pathPart string
	if strings.Contains(rawURL, "://") {
		u, err := url.Parse(rawURL)
		if err != nil {
			return "", "", fmt.Errorf("failed to parse remote URL %q: %w", rawURL, err)
		}
		pathPart = u.Path
	} else if idx := strings.Index(rawURL, ":"); idx != -1 {
		// SCP-style syntax: git@host:owner/repo.git
		pathPart = rawURL[idx+1:]
	} else {
		// Local file path or simple path
		pathPart = rawURL
	}

	pathPart = strings.Trim(pathPart, "/")
	pathPart = strings.TrimSuffix(pathPart, ".git")

	parts := strings.Split(pathPart, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repository remote URL format %q: expected owner/repo", rawURL)
	}

	owner = parts[len(parts)-2]
	repo = parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("invalid repository remote URL %q: owner or repo is empty", rawURL)
	}

	return owner, repo, nil
}
