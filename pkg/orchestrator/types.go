package orchestrator

// RepoStatus represents the git status of a repository in the workspace.
type RepoStatus struct {
	Name          string `json:"name" yaml:"name"`
	Path          string `json:"path" yaml:"path"`
	CurrentBranch string `json:"current_branch" yaml:"current_branch"`
	IsClean       bool   `json:"is_clean" yaml:"is_clean"`
	AheadCount    int    `json:"ahead_count" yaml:"ahead_count"`
	BehindCount   int    `json:"behind_count" yaml:"behind_count"`
	ModifiedCount int    `json:"modified_count" yaml:"modified_count"`
	Error         string `json:"error,omitempty" yaml:"error,omitempty"`
}

// CloneResult captures the outcome of cloning a repository target.
type CloneResult struct {
	Name          string `json:"name" yaml:"name"`
	Path          string `json:"path" yaml:"path"`
	Success       bool   `json:"success" yaml:"success"`
	AlreadyExists bool   `json:"already_exists" yaml:"already_exists"`
	Error         string `json:"error,omitempty" yaml:"error,omitempty"`
}

// SyncResult captures the outcome of synchronizing a repository target with remote.
type SyncResult struct {
	Name          string `json:"name" yaml:"name"`
	Path          string `json:"path" yaml:"path"`
	Success       bool   `json:"success" yaml:"success"`
	FastForwarded bool   `json:"fast_forwarded" yaml:"fast_forwarded"`
	SkippedReason string `json:"skipped_reason,omitempty" yaml:"skipped_reason,omitempty"`
	Error         string `json:"error,omitempty" yaml:"error,omitempty"`
	AssetError    string `json:"asset_error,omitempty" yaml:"asset_error,omitempty"`
}
