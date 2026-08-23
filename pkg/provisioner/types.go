package provisioner

import (
	"context"
	"errors"
)

var (
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrProvisionFailed   = errors.New("provisioning failed")
	ErrRollbackFailed    = errors.New("rollback failed")
	ErrInvalidRequest    = errors.New("invalid claim request")
	ErrServiceDegraded   = errors.New("provisioner service degraded")
)

// ClaimRequest contains all necessary details submitted by an onboarding developer.
type ClaimRequest struct {
	InviteToken        string            `json:"invite_token"`
	DesiredUID         string            `json:"desired_uid"`
	Email              string            `json:"email,omitempty"`
	DisplayName        string            `json:"display_name,omitempty"`
	SSHPublicKey       string            `json:"ssh_public_key"`
	MachineFingerprint string            `json:"machine_fingerprint,omitempty"`
	Scope              string            `json:"scope,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// User represents identity data provisioned in directory services (e.g. lldap).
type User struct {
	UID         string   `json:"uid"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Groups      []string `json:"groups"`
}

// Credentials contains provisioned Git MCP tokens and WireGuard configuration profiles.
type Credentials struct {
	ForgejoUsername string `json:"forgejo_username"`
	ForgejoMCPToken string `json:"forgejo_mcp_token"`
	WireGuardConfig string `json:"wireguard_config"`
}

// WorkspaceInfo contains Git remote base URL and default checkout scopes.
type WorkspaceInfo struct {
	GitRemoteBase        string `json:"git_remote_base"`
	DefaultManifestScope string `json:"default_manifest_scope"`
}

// ClaimResponse represents the final successful provisioning output returned to the client.
type ClaimResponse struct {
	Status           string        `json:"status"`
	IdempotentReplay bool          `json:"idempotent_replay"`
	User             User          `json:"user"`
	Credentials      Credentials   `json:"credentials"`
	Workspace        WorkspaceInfo `json:"workspace"`
}

// Provisioner provides the interface for identity, git, and network provisioning.
type Provisioner interface {
	Provision(ctx context.Context, req ClaimRequest) (*ClaimResponse, error)
	Rollback(ctx context.Context, uid string) error
	Health(ctx context.Context) error
}
