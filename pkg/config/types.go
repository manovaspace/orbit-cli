package config

import "time"

// Source identifies where a configuration parameter was resolved from.
type Source string

const (
	SourceDefault  Source = "default"
	SourceUserFile Source = "user-config"
	SourceWorkFile Source = "workspace-config"
	SourceEnv      Source = "env"
	SourceFlag     Source = "flag"
)

type ServerConfig struct {
	URL     string        `yaml:"url" json:"url"`
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
}

type StaffConfig struct {
	URL string `yaml:"url" json:"url"`
}

type AssetsConfig struct {
	Bucket   string `yaml:"bucket" json:"bucket"`
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	AutoPull bool   `yaml:"auto_pull" json:"auto_pull"`
}

type DefaultsConfig struct {
	Scope      string `yaml:"scope" json:"scope"`
	ExpiryDays int    `yaml:"expiry_days" json:"expiry_days"`
}

type UIConfig struct {
	Color  bool   `yaml:"color" json:"color"`
	Output string `yaml:"output" json:"output"` // "table", "json", "yaml"
}

type Config struct {
	Version  int                    `yaml:"version" json:"version"`
	Server   ServerConfig           `yaml:"server" json:"server"`
	Staff    StaffConfig            `yaml:"staff" json:"staff"`
	Assets   AssetsConfig           `yaml:"assets" json:"assets"`
	Defaults DefaultsConfig         `yaml:"defaults" json:"defaults"`
	UI       UIConfig               `yaml:"ui" json:"ui"`
	Custom   map[string]interface{} `yaml:"custom,omitempty" json:"custom,omitempty"`
}

// ConfigEntry wraps an active setting with its origin metadata.
type ConfigEntry struct {
	Key       string `json:"key" yaml:"key"`
	Value     string `json:"value" yaml:"value"`
	Type      string `json:"type" yaml:"type"`
	Source    Source `json:"source" yaml:"source"`
	SourceRef string `json:"source_ref,omitempty" yaml:"source_ref,omitempty"`
}

func (e ConfigEntry) String() string {
	return e.Value
}

type ResolveOptions struct {
	ConfigPath       string
	ServerFlag       string
	StaffURLFlag     string
	AssetsBucketFlag string
	ScopeFlag        string
}

type ResolvedConfig struct {
	Config  *Config
	Entries []ConfigEntry
}

func (r *ResolvedConfig) ListEntries() []ConfigEntry {
	return r.Entries
}


