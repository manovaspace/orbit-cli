package manifest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads and parses a workspace manifest YAML file from the specified path.
func Load(path string) (*WorkspaceManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	manifest, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest from %s: %w", path, err)
	}

	return manifest, nil
}

// Parse parses raw YAML bytes into a WorkspaceManifest struct.
func Parse(data []byte) (*WorkspaceManifest, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("manifest is empty")
	}

	var m WorkspaceManifest
	if err := yaml.Unmarshal(trimmed, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal workspace YAML: %w", err)
	}

	return &m, nil
}

// AllRepos returns all repositories defined across all groups and clients in the manifest.
func (m *WorkspaceManifest) AllRepos() []RepoTarget {
	var targets []RepoTarget

	if m == nil || m.Groups == nil {
		return targets
	}

	// Sort group keys for deterministic output
	groupKeys := make([]string, 0, len(m.Groups))
	for k := range m.Groups {
		groupKeys = append(groupKeys, k)
	}
	sort.Strings(groupKeys)

	for _, groupKey := range groupKeys {
		group := m.Groups[groupKey]

		// 1. Group with direct repositories
		if len(group.Repositories) > 0 {
			for _, repo := range group.Repositories {
				target := m.buildRepoTarget(repo, group.Path, group.Defaults, group.Remote, group.DefaultBranch, groupKey)
				targets = append(targets, target)
			}
			continue
		}

		// 2. Group with nested clients
		if len(group.Clients) > 0 {
			clientKeys := make([]string, 0, len(group.Clients))
			for ck := range group.Clients {
				clientKeys = append(clientKeys, ck)
			}
			sort.Strings(clientKeys)

			for _, clientKey := range clientKeys {
				client := group.Clients[clientKey]

				clientBasePath := client.Path
				if clientBasePath == "" {
					if group.Path != "" {
						clientBasePath = filepath.Join(group.Path, clientKey)
					} else {
						clientBasePath = clientKey
					}
				}

				scope := groupKey + "/" + clientKey

				// Inherit client defaults falling back to group defaults
				mergedDefaults := client.Defaults
				if mergedDefaults.Remote == "" {
					mergedDefaults.Remote = group.Defaults.Remote
				}
				if mergedDefaults.Remote == "" {
					mergedDefaults.Remote = group.Remote
				}
				if mergedDefaults.DefaultBranch == "" {
					mergedDefaults.DefaultBranch = group.Defaults.DefaultBranch
				}
				if mergedDefaults.DefaultBranch == "" {
					mergedDefaults.DefaultBranch = group.DefaultBranch
				}

				for _, repo := range client.Repositories {
					target := m.buildRepoTarget(repo, clientBasePath, mergedDefaults, group.Remote, group.DefaultBranch, scope)
					targets = append(targets, target)
				}
			}
			continue
		}

		// 3. Single-repository group (e.g. handbook)
		repoPath := group.Path
		if repoPath == "" {
			repoPath = groupKey
		}

		remoteName := group.Remote
		if remoteName == "" {
			remoteName = group.Defaults.Remote
		}

		branch := group.DefaultBranch
		if branch == "" {
			branch = group.Defaults.DefaultBranch
		}
		if branch == "" {
			branch = "main"
		}

		target := RepoTarget{
			Name:          groupKey,
			Path:          filepath.Clean(repoPath),
			RemoteURL:     m.resolveRemoteURL(remoteName, groupKey, group.Repo, group.RemoteURL),
			DefaultBranch: branch,
			Required:      group.Required,
			Scope:         groupKey,
		}
		targets = append(targets, target)
	}

	return targets
}

func (m *WorkspaceManifest) buildRepoTarget(repo RepoConfig, basePath string, defaults GroupDefaults, fallbackRemote, fallbackBranch, scope string) RepoTarget {
	var repoPath string
	if repo.Path != "" {
		if filepath.IsAbs(repo.Path) || (basePath != "" && strings.HasPrefix(repo.Path, basePath+"/")) || basePath == "" {
			repoPath = repo.Path
		} else {
			repoPath = filepath.Join(basePath, repo.Path)
		}
	} else {
		if basePath != "" {
			repoPath = filepath.Join(basePath, repo.Name)
		} else {
			repoPath = repo.Name
		}
	}

	remoteName := repo.Remote
	if remoteName == "" {
		remoteName = defaults.Remote
	}
	if remoteName == "" {
		remoteName = fallbackRemote
	}

	branch := repo.DefaultBranch
	if branch == "" {
		branch = defaults.DefaultBranch
	}
	if branch == "" {
		branch = fallbackBranch
	}
	if branch == "" {
		branch = "main"
	}

	return RepoTarget{
		Name:          repo.Name,
		Path:          filepath.Clean(repoPath),
		RemoteURL:     m.resolveRemoteURL(remoteName, repo.Name, repo.Repo, repo.RemoteURL),
		DefaultBranch: branch,
		Required:      repo.Required,
		Scope:         scope,
	}
}

// ResolveScope filters and resolves repositories based on a given scope identifier.
// Supported scopes:
// - "core" or "": returns all repositories marked required: true
// - "all" or "*": returns all repositories in the manifest
// - "orbit", "manovaspace", etc.: returns all repositories under that group
// - "clients/<name>": returns repositories for a specific client cluster
// - "clients": returns all repositories under all clients
// - "<name>": matches client cluster shorthand or individual repository name
func (m *WorkspaceManifest) ResolveScope(scope string) []RepoTarget {
	all := m.AllRepos()
	s := strings.TrimSpace(strings.ToLower(scope))

	// 1. Core scope (or empty scope)
	if s == "" || s == "core" {
		var core []RepoTarget
		for _, r := range all {
			if r.Required {
				core = append(core, r)
			}
		}
		return core
	}

	// 2. All scope
	if s == "all" || s == "*" {
		return all
	}

	// 3. Exact scope match (e.g. "orbit", "manovaspace", "clients/fryto")
	var matched []RepoTarget
	for _, r := range all {
		if strings.EqualFold(r.Scope, s) {
			matched = append(matched, r)
		}
	}
	if len(matched) > 0 {
		return matched
	}

	// 4. Group prefix match (e.g. "clients" matching "clients/fryto", "clients/jtash")
	for _, r := range all {
		if strings.HasPrefix(strings.ToLower(r.Scope), s+"/") {
			matched = append(matched, r)
		}
	}
	if len(matched) > 0 {
		return matched
	}

	// 5. Client path or shorthand match (e.g. "clients/demo" matching path prefix, "fryto" matching "clients/fryto")
	for _, r := range all {
		if strings.EqualFold(r.Scope, "clients/"+s) ||
			strings.HasSuffix(strings.ToLower(r.Scope), "/"+s) ||
			strings.HasPrefix(strings.ToLower(r.Path), s+"/") ||
			strings.EqualFold(r.Path, s) {
			matched = append(matched, r)
		}
	}
	if len(matched) > 0 {
		return matched
	}

	// 6. Individual repository name match (e.g. "orbit-auth", "ts")
	for _, r := range all {
		if strings.EqualFold(r.Name, s) {
			matched = append(matched, r)
		}
	}

	return matched
}

// ResolveRepos resolves repositories for a scope, returning an error if no repositories match.
func (m *WorkspaceManifest) ResolveRepos(scope string) ([]RepoTarget, error) {
	targets := m.ResolveScope(scope)
	if len(targets) == 0 && scope != "all" && scope != "*" {
		return nil, fmt.Errorf("no repositories found for scope %q", scope)
	}
	return targets, nil
}

func (m *WorkspaceManifest) resolveRemoteURL(remoteName, repoName, repoField, explicitURL string) string {
	if explicitURL != "" {
		return explicitURL
	}

	gitName := repoField
	if gitName == "" {
		gitName = repoName
	}
	if gitName != "" && !strings.HasSuffix(gitName, ".git") {
		gitName += ".git"
	}

	if m.Remotes == nil {
		if strings.Contains(remoteName, "://") || strings.HasPrefix(remoteName, "git@") {
			return joinRemoteURL(remoteName, gitName)
		}
		return gitName
	}

	baseURL, ok := m.Remotes[remoteName]
	if !ok || baseURL == "" {
		if strings.Contains(remoteName, "://") || strings.HasPrefix(remoteName, "git@") {
			baseURL = remoteName
		} else {
			return gitName
		}
	}

	return joinRemoteURL(baseURL, gitName)
}

func joinRemoteURL(baseURL, gitName string) string {
	if strings.HasSuffix(baseURL, ":") {
		return baseURL + gitName
	}
	trimmed := strings.TrimRight(baseURL, "/")
	return trimmed + "/" + gitName
}
