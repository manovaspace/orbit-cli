// Package mcpenv implements the workspace MCP environment port with
// pkg/migrate.
package mcpenv

import "github.com/manovaspace/orbit-cli/pkg/migrate"

// Setup ensures .cursor/mcp.env exists.
type Setup struct{}

// New builds a Setup.
func New() *Setup { return &Setup{} }

// Ensure creates or copies .cursor/mcp.env under workspaceRoot.
func (s *Setup) Ensure(workspaceRoot string) error {
	return migrate.SetupMCPEnvironment(workspaceRoot)
}
