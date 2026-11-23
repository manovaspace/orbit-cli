package spec

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// RootSpec is the workspace root.yaml: links, copied files and generated files at the workspace root.
type RootSpec struct {
	Version    int               `yaml:"version" json:"version"`
	Links      map[string]string `yaml:"links" json:"links"`
	Files      map[string]string `yaml:"files" json:"files"`
	Generated  map[string]string `yaml:"generated" json:"generated"`
	Generators []string          `yaml:"generators" json:"generators"`
}

// ParseRootSpec parses and validates a root.yaml. Every link and generated key and source, and every
// files key, is a relative path without "..". Files values are content and are not checked as paths.
// Generated values must be a known generator.
func ParseRootSpec(data []byte) (RootSpec, error) {
	var rs RootSpec
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return RootSpec{}, clierr.Wrap(err, clierr.Usage("root spec is malformed"))
	}
	for key, src := range rs.Links {
		if err := checkRelative(key); err != nil {
			return RootSpec{}, clierr.Usage(fmt.Sprintf("root spec key %q: %v", key, err))
		}
		if err := checkRelative(src); err != nil {
			return RootSpec{}, clierr.Usage(fmt.Sprintf("root spec source %q for %q: %v", src, key, err))
		}
	}
	// Files values are file content, not paths: only their keys are checked.
	for key := range rs.Files {
		if err := checkRelative(key); err != nil {
			return RootSpec{}, clierr.Usage(fmt.Sprintf("root spec file %q: %v", key, err))
		}
	}
	for key, src := range rs.Generated {
		if err := checkRelative(key); err != nil {
			return RootSpec{}, clierr.Usage(fmt.Sprintf("root spec key %q: %v", key, err))
		}
		if err := checkRelative(src); err != nil {
			return RootSpec{}, clierr.Usage(fmt.Sprintf("root spec source %q for %q: %v", src, key, err))
		}
	}
	for key, kind := range rs.Generated {
		if kind != "go-work" {
			return RootSpec{}, clierr.Usage(fmt.Sprintf("root spec generated %q: kind %q is invalid: want go-work", key, kind))
		}
	}
	return rs, nil
}
