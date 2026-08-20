package manifest

// WorkspaceManifest represents the root configuration of workspace.yaml.
type WorkspaceManifest struct {
	Version   string                 `yaml:"version" json:"version"`
	Workspace string                 `yaml:"workspace" json:"workspace"`
	Remotes   RemotesConfig          `yaml:"remotes" json:"remotes"`
	Groups    map[string]GroupConfig `yaml:"groups" json:"groups"`
}

// RemotesConfig maps remote aliases (e.g. "forgejo", "github_manovaspace") to base URLs.
type RemotesConfig map[string]string

// GroupDefaults defines default values inherited by repositories in a group or client.
type GroupDefaults struct {
	Remote        string `yaml:"remote" json:"remote"`
	DefaultBranch string `yaml:"default_branch" json:"default_branch"`
}

// GroupConfig defines a group of repositories or client clusters.
type GroupConfig struct {
	Path          string                  `yaml:"path" json:"path"`
	Description   string                  `yaml:"description" json:"description"`
	Repo          string                  `yaml:"repo" json:"repo"`
	Remote        string                  `yaml:"remote" json:"remote"`
	RemoteURL     string                  `yaml:"remote_url" json:"remote_url"`
	DefaultBranch string                  `yaml:"default_branch" json:"default_branch"`
	Required      bool                    `yaml:"required" json:"required"`
	Defaults      GroupDefaults           `yaml:"defaults" json:"defaults"`
	Repositories  []RepoConfig            `yaml:"repositories" json:"repositories"`
	Clients       map[string]ClientConfig `yaml:"clients" json:"clients"`
}

// ClientConfig defines a specific client cluster containing repositories.
type ClientConfig struct {
	Path          string        `yaml:"path" json:"path"`
	Description   string        `yaml:"description" json:"description"`
	Defaults      GroupDefaults `yaml:"defaults" json:"defaults"`
	Repositories  []RepoConfig  `yaml:"repositories" json:"repositories"`
}

// RepoConfig defines a single repository within a group or client.
type RepoConfig struct {
	Name          string `yaml:"name" json:"name"`
	Path          string `yaml:"path" json:"path"`
	Repo          string `yaml:"repo" json:"repo"`
	Remote        string `yaml:"remote" json:"remote"`
	RemoteURL     string `yaml:"remote_url" json:"remote_url"`
	DefaultBranch string `yaml:"default_branch" json:"default_branch"`
	Required      bool   `yaml:"required" json:"required"`
}

// RepoTarget represents a fully resolved repository target ready for cloning or syncing.
type RepoTarget struct {
	Name          string `json:"name" yaml:"name"`
	Path          string `json:"path" yaml:"path"`
	RemoteURL     string `json:"remote_url" yaml:"remote_url"`
	DefaultBranch string `json:"default_branch" yaml:"default_branch"`
	Required      bool   `json:"required" yaml:"required"`
	Scope         string `json:"scope" yaml:"scope"`
}
