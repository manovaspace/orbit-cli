package domain

import (
	"fmt"
	"strings"
)

// EvaluateRTK checks the RTK install used for condensed command output.
func EvaluateRTK(output string, probeErr error) DiagnosticResult {
	result := DiagnosticResult{Category: CategoryDevTools, Name: "RTK"}
	if probeErr != nil {
		result.Status = StatusWarning
		result.Message = "RTK not installed"
		result.FixSuggestion = "Install RTK and add it to PATH; agents use it to condense command output."
		return result
	}
	version, err := ParseSemver(output, "rtk")
	if err != nil {
		result.Status = StatusUnverified
		result.Message = "RTK version unverified: cannot parse version probe"
		return result
	}
	result.Status = StatusOK
	result.Message = fmt.Sprintf("RTK v%s installed", version)
	return result
}

// EvaluateRTKHook reads `rtk gain` and warns when the shell/agent hook is
// missing, which silently disables the token savings RTK offers.
func EvaluateRTKHook(output string, probeErr error) DiagnosticResult {
	result := DiagnosticResult{Category: CategoryDevTools, Name: "RTK hook"}
	if probeErr != nil {
		result.Status = StatusUnverified
		result.Message = "RTK hook state unverified: `rtk gain` failed"
		return result
	}
	if strings.Contains(output, "No hook installed") {
		result.Status = StatusWarning
		result.Message = "RTK hook not installed; command output is not being condensed"
		result.FixSuggestion = "rtk init -g"
		return result
	}
	result.Status = StatusOK
	result.Message = "RTK hook installed"
	return result
}

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
