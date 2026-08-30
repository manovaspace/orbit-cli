package session

import "time"

// Stage represents the lifecycle stage of an onboarding session.
type Stage string

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

// SessionState represents the persistent checkpoint state of an onboarding session.
type SessionState struct {
	ID              string            `json:"id"`
	Email           string            `json:"email"`
	DisplayName     string            `json:"display_name"`
	UID             string            `json:"uid"`
	CurrentStage    Stage             `json:"current_stage"`
	InviteToken     string            `json:"invite_token,omitempty"`
	ClaimToken      string            `json:"claim_token,omitempty"`
	SSHPublicKey    string            `json:"ssh_public_key,omitempty"`
	ForgejoToken    string            `json:"forgejo_token,omitempty"`
	WireGuardConfig string            `json:"wireguard_config,omitempty"`
	WorkspacePath   string            `json:"workspace_path,omitempty"`
	ClonedRepos     []string          `json:"cloned_repos,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// Session is an alias for SessionState for backward compatibility.
type Session = SessionState
