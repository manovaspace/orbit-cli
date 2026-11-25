package manifest

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// ProjectConfig names a set of repositories by their workspace path. "*" selects every repository.
type ProjectConfig struct {
	Description string   `yaml:"description" json:"description"`
	Repos       []string `yaml:"repos" json:"repos"`
}

// UnmarshalYAML accepts either the shorthand sequence form (`falak: [clients/manova/falak]`)
// or the mapping form (`falak: {description: ..., repos: [...]}`).
func (p *ProjectConfig) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		var repos []string
		if err := value.Decode(&repos); err != nil {
			return err
		}
		*p = ProjectConfig{Repos: repos}
		return nil
	case yaml.MappingNode:
		type plain ProjectConfig
		var decoded plain
		if err := value.Decode(&decoded); err != nil {
			return err
		}
		*p = ProjectConfig(decoded)
		return nil
	case yaml.ScalarNode:
		if value.Tag == "!!null" {
			*p = ProjectConfig{}
			return nil
		}
	}
	return fmt.Errorf("project must be a sequence of repo paths or a mapping with description and repos (line %d)", value.Line)
}

// ProjectNames returns the project names in the manifest, sorted.
func (m *WorkspaceManifest) ProjectNames() []string {
	if m == nil {
		return nil
	}
	names := make([]string, 0, len(m.Projects))
	for name := range m.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ProjectRepos resolves a project to its repositories, sorted by Path and de-duplicated.
// The name "*" or a "*" entry in the project's repos selects every repository.
// An unknown project or repo path is a usage error.
func (m *WorkspaceManifest) ProjectRepos(name string) ([]RepoTarget, error) {
	all := m.AllRepos()
	if name == "*" {
		return sortedUniqueRepos(all), nil
	}
	if m == nil {
		return nil, clierr.Usage(fmt.Sprintf("unknown project %q", name))
	}
	project, ok := m.Projects[name]
	if !ok {
		return nil, clierr.Usage(fmt.Sprintf("unknown project %q", name))
	}

	for _, entry := range project.Repos {
		if entry == "*" {
			return sortedUniqueRepos(all), nil
		}
	}

	byPath := make(map[string]RepoTarget, len(all))
	for _, target := range all {
		byPath[target.Path] = target
	}
	selected := make([]RepoTarget, 0, len(project.Repos))
	for _, entry := range project.Repos {
		target, ok := byPath[entry]
		if !ok {
			return nil, clierr.Usage(fmt.Sprintf("project %q: unknown repo path %q", name, entry))
		}
		selected = append(selected, target)
	}
	return sortedUniqueRepos(selected), nil
}

func sortedUniqueRepos(targets []RepoTarget) []RepoTarget {
	sorted := make([]RepoTarget, len(targets))
	copy(sorted, targets)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	out := make([]RepoTarget, 0, len(sorted))
	seen := make(map[string]bool, len(sorted))
	for _, target := range sorted {
		if seen[target.Path] {
			continue
		}
		seen[target.Path] = true
		out = append(out, target)
	}
	return out
}
