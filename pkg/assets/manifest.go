package assets

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func ManifestPath(repoRoot string) string {
	return filepath.Join(repoRoot, ManifestFile)
}

func HasManifest(repoRoot string) bool {
	_, err := os.Stat(ManifestPath(repoRoot))
	return err == nil
}

func Load(repoRoot string) (Manifest, error) {
	path := ManifestPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{Version: 1}, nil
		}
		return Manifest{}, fmt.Errorf("read %s: %w", path, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Objects == nil {
		m.Objects = []Object{}
	}
	return m, nil
}

func Save(repoRoot string, m Manifest) error {
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Objects == nil {
		m.Objects = []Object{}
	}
	data, err := yaml.Marshal(&m)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", ManifestFile, err)
	}
	path := ManifestPath(repoRoot)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
