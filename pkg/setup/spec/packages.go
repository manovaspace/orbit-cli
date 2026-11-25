package spec

import (
	_ "embed"

	"gopkg.in/yaml.v3"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// Package is one installable tool recipe. Brew and Apt name the package manager entries;
// AptRepo names an apt source; Script is an installer URL; Detect is the binary that marks a
// script-installed package as present.
type Package struct {
	Brew    string `yaml:"brew" json:"brew"`
	Apt     string `yaml:"apt" json:"apt"`
	AptRepo string `yaml:"apt_repo" json:"apt_repo"`
	Script  string `yaml:"script" json:"script"`
	Detect  string `yaml:"detect" json:"detect"`
}

//go:embed packages.yaml
var packagesYAML []byte

// Packages returns the embedded package recipes keyed by name.
func Packages() (map[string]Package, error) {
	var pkgs map[string]Package
	if err := yaml.Unmarshal(packagesYAML, &pkgs); err != nil {
		return nil, clierr.Wrap(err, clierr.Failed("embedded packages.yaml is malformed"))
	}
	return pkgs, nil
}
