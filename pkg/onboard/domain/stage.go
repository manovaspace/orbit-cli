// Package domain holds the pure onboarding model: the checkpointed session,
// its lifecycle stages, the claim payloads and the invocation rules. It performs
// no I/O and imports no adapter.
package domain

// Stage is one checkpointed step of an onboarding session.
type Stage string

// Lifecycle stages. StageComplete and StageCompleted share the same persisted
// value so older checkpoints keep resuming correctly.
const (
	StageInit              Stage = "init"
	StageWelcome           Stage = "welcome"
	StageDoctor            Stage = "doctor"
	StageDoctorPassed      Stage = "doctor_passed"
	StageIdentity          Stage = "identity"
	StageKeypairReady      Stage = "keypair_ready"
	StageTokenClaimed      Stage = "token_claimed"
	StageClaimSubmitted    Stage = "claim_submitted"
	StageNetworkConfigured Stage = "network_configured"
	StageWorkspace         Stage = "workspace"
	StageReposCloned       Stage = "repos_cloned"
	StageEnvironment       Stage = "environment"
	StageEnvironmentReady  Stage = "environment_ready"
	StageMCPConfigured     Stage = "mcp_configured"
	StageStack             Stage = "stack"
	StageStackReady        Stage = "stack_ready"
	StageDevStackReady     Stage = "dev_stack_ready"
	StageComplete          Stage = "completed"
	StageCompleted         Stage = "completed"
)

// stageOrder ranks the checkpointed stages so a saved session can skip the
// steps it already finished.
var stageOrder = map[Stage]int{
	StageInit:              0,
	StageDoctorPassed:      1,
	StageKeypairReady:      2,
	StageTokenClaimed:      3,
	StageNetworkConfigured: 4,
	StageReposCloned:       5,
	StageMCPConfigured:     6,
	StageDevStackReady:     7,
	StageCompleted:         8,
}

// IsStageCompleted reports whether current has reached target.
func IsStageCompleted(current, target Stage) bool {
	return stageOrder[current] >= stageOrder[target]
}

// Step is one headless onboarding step in execution order.
type Step struct {
	Stage Stage
	Title string
}

// Steps lists the headless steps in the order the flow runs them.
var Steps = []Step{
	{Stage: StageDoctorPassed, Title: "Pre-Flight System Diagnostics"},
	{Stage: StageKeypairReady, Title: "SSH Developer Keypair"},
	{Stage: StageTokenClaimed, Title: "Identity & Provisioning Claim"},
	{Stage: StageNetworkConfigured, Title: "Network & Local TLS Setup"},
	{Stage: StageReposCloned, Title: "Workspace & Repositories"},
	{Stage: StageMCPConfigured, Title: "Cursor MCP & IDE Integration"},
	{Stage: StageDevStackReady, Title: "Local Development Stack"},
}
