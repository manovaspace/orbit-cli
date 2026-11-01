// Package osfile implements the configuration store with the os package.
package osfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/config/domain"
	"gopkg.in/yaml.v3"
)

// Store reads and writes the configuration file on the real filesystem.
type Store struct{}

// New builds a Store.
func New() *Store { return &Store{} }

// Env reads the process environment.
type Env struct{}

// NewEnv builds an Env.
func NewEnv() Env { return Env{} }

// Getenv returns the process variable key.
func (Env) Getenv(key string) string { return os.Getenv(key) }

// DefaultConfigPath returns the user configuration path, honouring
// ORBIT_CONFIG and XDG_CONFIG_HOME.
func DefaultConfigPath() string {
	if p := strings.TrimSpace(os.Getenv("ORBIT_CONFIG")); p != "" {
		return p
	}
	configDir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".config", "orbit", "config.yaml")
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "orbit", "config.yaml")
}

// DefaultPath implements application.Store.
func (s *Store) DefaultPath() string { return DefaultConfigPath() }

func resolvePath(path string) string {
	if path == "" {
		return DefaultConfigPath()
	}
	return path
}

// Read returns the file bytes and tightens wider permissions to 0600.
func (s *Store) Read(path string) ([]byte, error) {
	path = resolvePath(path)

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	// Auto-tighten permissions if wider than 0600
	if info.Mode().Perm()&0077 != 0 || info.Mode().Perm() > 0600 {
		_ = os.Chmod(path, 0600)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}
	return data, nil
}

// Load reads and parses the configuration file.
func (s *Store) Load(path string) (*domain.Config, error) {
	data, err := s.Read(path)
	if err != nil {
		return nil, err
	}

	cfg := domain.DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config yaml: %w", err)
	}
	if cfg.Custom == nil {
		cfg.Custom = make(map[string]interface{})
	}
	return cfg, nil
}

// Save writes cfg with mode 0600.
func (s *Store) Save(path string, cfg *domain.Config) error {
	path = resolvePath(path)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to encode config yaml: %w", err)
	}

	return atomicWriteFile(path, data, 0600)
}

// Exists reports whether path exists.
func (s *Store) Exists(path string) (bool, error) {
	path = resolvePath(path)
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-config-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %q: %w", dir, err)
	}
	tmpPath := tmpFile.Name()

	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to chmod temp file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to atomic rename file to %q: %w", path, err)
	}

	return nil
}

func unwrapMappingNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			mapNode := &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}
			node.Content = append(node.Content, mapNode)
			return mapNode
		}
		if node.Content[0].Kind != yaml.MappingNode {
			node.Content[0] = &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}
		}
		return node.Content[0]
	}
	if node.Kind == yaml.MappingNode {
		return node
	}
	return nil
}

func setScalarValue(node *yaml.Node, value string) {
	node.Kind = yaml.ScalarNode
	node.Value = value
	node.Content = nil

	lower := strings.ToLower(value)
	if lower == "true" || lower == "false" {
		node.Tag = "!!bool"
		node.Value = lower
		node.Style = 0
	} else if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		node.Tag = "!!int"
		node.Style = 0
	} else {
		node.Tag = "!!str"
	}
}

func modifyNode(node *yaml.Node, parts []string, value string) error {
	if node == nil {
		return errors.New("node is nil")
	}
	if len(parts) == 0 {
		return errors.New("path cannot be empty")
	}

	current := unwrapMappingNode(node)
	if current == nil {
		return errors.New("root node must be a document or mapping node")
	}

	for i, part := range parts {
		isLeaf := (i == len(parts)-1)

		var targetValNode *yaml.Node
		for j := 0; j < len(current.Content); j += 2 {
			keyNode := current.Content[j]
			if keyNode.Value == part {
				targetValNode = current.Content[j+1]
				break
			}
		}

		if isLeaf {
			if targetValNode != nil {
				setScalarValue(targetValNode, value)
			} else {
				keyNode := &yaml.Node{
					Kind:  yaml.ScalarNode,
					Tag:   "!!str",
					Value: part,
				}
				valNode := &yaml.Node{}
				setScalarValue(valNode, value)
				current.Content = append(current.Content, keyNode, valNode)
			}
			return nil
		}

		// Intermediate node
		if targetValNode == nil {
			keyNode := &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: part,
			}
			valNode := &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}
			current.Content = append(current.Content, keyNode, valNode)
			current = valNode
		} else {
			if targetValNode.Kind != yaml.MappingNode {
				targetValNode.Kind = yaml.MappingNode
				targetValNode.Tag = "!!map"
				targetValNode.Value = ""
				targetValNode.Content = nil
			}
			current = targetValNode
		}
	}

	return nil
}

func deleteNode(node *yaml.Node, parts []string) error {
	if node == nil {
		return errors.New("node is nil")
	}
	if len(parts) == 0 {
		return errors.New("path cannot be empty")
	}

	current := unwrapMappingNode(node)
	if current == nil {
		return errors.New("root node must be a document or mapping node")
	}

	deleteNodeHelper(current, parts)
	return nil
}

func deleteNodeHelper(parent *yaml.Node, parts []string) {
	if parent == nil || parent.Kind != yaml.MappingNode || len(parts) == 0 {
		return
	}

	targetKey := parts[0]
	for i := 0; i < len(parent.Content); i += 2 {
		keyNode := parent.Content[i]
		if keyNode.Value == targetKey {
			valNode := parent.Content[i+1]
			if len(parts) == 1 {
				// Leaf reached: remove key and value nodes
				parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
				return
			}
			if valNode.Kind == yaml.MappingNode {
				deleteNodeHelper(valNode, parts[1:])
			}
			return
		}
	}
}

// SetInFile updates one key while preserving comments and layout.
func (s *Store) SetInFile(filePath, path, value string) error {
	filePath = resolvePath(filePath)

	parts := domain.NormalizePath(path)
	if len(parts) == 0 {
		return errors.New("invalid empty path")
	}

	var doc yaml.Node
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			data = []byte(domain.DefaultConfigTemplate)
		} else {
			return fmt.Errorf("failed to read config file %q: %w", filePath, err)
		}
	}

	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse yaml in %q: %w", filePath, err)
	}

	if err := modifyNode(&doc, parts, value); err != nil {
		return fmt.Errorf("failed to modify yaml node: %w", err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return fmt.Errorf("failed to encode yaml node: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("failed to close yaml encoder: %w", err)
	}

	return atomicWriteFile(filePath, buf.Bytes(), 0600)
}

// UnsetInFile removes one key while preserving comments and layout.
func (s *Store) UnsetInFile(filePath, path string) error {
	filePath = resolvePath(filePath)

	parts := domain.NormalizePath(path)
	if len(parts) == 0 {
		return errors.New("invalid empty path")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read config file %q: %w", filePath, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse yaml in %q: %w", filePath, err)
	}

	if err := deleteNode(&doc, parts); err != nil {
		return fmt.Errorf("failed to delete yaml node: %w", err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return fmt.Errorf("failed to encode yaml node: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("failed to close yaml encoder: %w", err)
	}

	return atomicWriteFile(filePath, buf.Bytes(), 0600)
}
