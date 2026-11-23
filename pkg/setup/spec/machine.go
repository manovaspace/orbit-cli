// Package spec holds the pure declaration types for orbit setup: the per-machine file,
// each repo's orbit.yaml, the workspace root spec and the embedded package recipes.
package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// Machine is the per-machine setup file (machine.yaml).
type Machine struct {
	Workspace   string   `yaml:"workspace" json:"workspace"`
	Projects    []string `yaml:"projects" json:"projects"`
	Services    string   `yaml:"services" json:"services"`
	GitProtocol string   `yaml:"git_protocol" json:"git_protocol"`
	Device      string   `yaml:"device" json:"device"`
}

// DefaultMachinePath returns $XDG_CONFIG_HOME/orbit/machine.yaml, else ~/.config/orbit/machine.yaml.
func DefaultMachinePath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "orbit", "machine.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "orbit", "machine.yaml")
}

// LoadMachine reads the machine file at path. The bool reports whether the file existed.
// A missing file yields the defaults; a malformed file is a usage error naming the path.
func LoadMachine(path string) (Machine, bool, error) {
	m := machineDefaults()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, false, nil
	}
	if err != nil {
		return Machine{}, true, clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot read machine file %s", path)))
	}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Machine{}, true, clierr.Wrap(err, clierr.Usage(fmt.Sprintf("machine file %s is malformed", path)))
	}
	ws, err := expandHome(m.Workspace)
	if err != nil {
		return Machine{}, true, err
	}
	m.Workspace = ws
	if m.Services == "" {
		m.Services = "native"
	}
	if m.GitProtocol == "" {
		m.GitProtocol = "https"
	}
	if m.Device == "" {
		m.Device = defaultDevice()
	}
	return m, true, nil
}

// SaveMachine writes m to path atomically: directory 0700, file 0600, temp file then rename.
// An invalid machine is refused.
func SaveMachine(path string, m Machine) error {
	if err := m.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot encode machine file"))
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot create %s", dir)))
	}
	tmp, err := os.CreateTemp(dir, ".machine-*.yaml")
	if err != nil {
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot create temp file in %s", dir)))
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return clierr.Wrap(err, clierr.Failed("cannot set machine file mode"))
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return clierr.Wrap(err, clierr.Failed("cannot write machine file"))
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return clierr.Wrap(err, clierr.Failed("cannot sync machine file"))
	}
	if err := tmp.Close(); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot close machine file"))
	}
	if err := os.Rename(tmpName, path); err != nil {
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot replace %s", path)))
	}
	return nil
}

// Validate checks the enumerated fields and that the workspace is absolute after ~ expansion.
func (m Machine) Validate() error {
	switch m.Services {
	case "native", "container", "external":
	default:
		return clierr.Usage(fmt.Sprintf("services %q is invalid: want native, container or external", m.Services))
	}
	switch m.GitProtocol {
	case "https", "ssh":
	default:
		return clierr.Usage(fmt.Sprintf("git_protocol %q is invalid: want https or ssh", m.GitProtocol))
	}
	ws, err := expandHome(m.Workspace)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(ws) {
		return clierr.Usage(fmt.Sprintf("workspace %q must be an absolute path", m.Workspace))
	}
	return nil
}

func machineDefaults() Machine {
	home, _ := os.UserHomeDir()
	return Machine{
		Workspace:   filepath.Join(home, "Dev", "Manova"),
		Services:    "native",
		GitProtocol: "https",
		Device:      defaultDevice(),
	}
}

func defaultDevice() string {
	host, _ := os.Hostname()
	return strings.ToLower(strings.SplitN(host, ".", 2)[0])
}

// expandHome replaces a leading "~/" (or a bare "~") with the user's home directory.
func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", clierr.Wrap(err, clierr.Failed("cannot resolve home directory"))
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}
