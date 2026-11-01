// Package layout resolves workspace locations. The CLI owns the resolution
// rules, so they are injected instead of duplicated here.
package layout

// Layout adapts the command's workspace locators to the onboarding flow.
type Layout struct {
	// Root resolves the workspace root from an optional override.
	Root func(override string) string
	// Manifest resolves the manifest path from the workspace root and --manifest.
	Manifest func(root, flag string) string
	// Infra resolves the orbit-infra checkout inside the workspace.
	Infra func(root string) string
}

// WorkspaceRoot resolves the root of the workspace.
func (l Layout) WorkspaceRoot(override string) string {
	if l.Root == nil {
		return override
	}
	return l.Root(override)
}

// ManifestPath resolves the workspace.yaml path.
func (l Layout) ManifestPath(root, flag string) string {
	if l.Manifest == nil {
		return flag
	}
	return l.Manifest(root, flag)
}

// InfraDir resolves the orbit-infra checkout directory.
func (l Layout) InfraDir(root string) string {
	if l.Infra == nil {
		return ""
	}
	return l.Infra(root)
}
