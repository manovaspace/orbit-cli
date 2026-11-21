package domain

import "fmt"

// EvaluateOpenCode checks the agent runtime used by the workspace.
func EvaluateOpenCode(output string, probeErr error) DiagnosticResult {
	result := DiagnosticResult{Category: CategoryDevTools, Name: "opencode"}
	if probeErr != nil {
		result.Status = StatusWarning
		result.Message = "opencode not installed"
		result.FixSuggestion = "Install opencode: curl -fsSL https://opencode.ai/install | bash"
		return result
	}
	version, err := ParseSemver(output, "opencode")
	if err != nil {
		result.Status = StatusUnverified
		result.Message = "opencode version unverified: cannot parse version probe"
		return result
	}
	result.Status = StatusOK
	result.Message = fmt.Sprintf("opencode v%s installed", version)
	return result
}
