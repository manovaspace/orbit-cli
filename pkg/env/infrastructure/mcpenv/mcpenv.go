// Package mcpenv implements the workspace MCP environment port with the
// workspace bootstrapper.
package mcpenv

import (
	"github.com/manovaspace/orbit-cli/pkg/migrate/infrastructure/workspace"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Setup ensures .cursor/mcp.env exists.
type Setup struct{}

// New builds a Setup.
func New() *Setup { return &Setup{} }

// Ensure creates or copies .cursor/mcp.env under workspaceRoot.
func (s *Setup) Ensure(workspaceRoot string) error {
	return workspace.New(runner.Exec{}).SetupMCPEnvironment(workspaceRoot)
}
