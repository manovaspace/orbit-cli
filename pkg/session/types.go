package session

import "time"

type Stage string

const (
	StageInit              Stage = "init"
	StageDoctorPassed      Stage = "doctor_passed"
	StageKeypairReady      Stage = "keypair_ready"
	StageTokenClaimed      Stage = "token_claimed"
	StageNetworkConfigured Stage = "network_configured"
	StageReposCloned       Stage = "repos_cloned"
	StageMCPConfigured     Stage = "mcp_configured"
	StageDevStackReady     Stage = "dev_stack_ready"
	StageCompleted         Stage = "completed"
)

type Session struct {
	ID              string            `json:"id"`
	Email           string            `json:"email"`
	DisplayName     string            `json:"display_name"`
	UID             string            `json:"uid"`
	CurrentStage    Stage             `json:"current_stage"`
	InviteToken     string            `json:"invite_token,omitempty"`
	SSHPublicKey    string            `json:"ssh_public_key,omitempty"`
	ForgejoToken    string            `json:"forgejo_token,omitempty"`
	WireGuardConfig string            `json:"wireguard_config,omitempty"`
	WorkspacePath   string            `json:"workspace_path,omitempty"`
	ClonedRepos     []string          `json:"cloned_repos,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}
