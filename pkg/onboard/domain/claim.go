package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors a provisioner may return. The edge server maps them to HTTP
// status codes and the CLI keeps their meaning in error messages.
var (
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrProvisionFailed   = errors.New("provisioning failed")
	ErrRollbackFailed    = errors.New("rollback failed")
	ErrInvalidRequest    = errors.New("invalid claim request")
	ErrServiceDegraded   = errors.New("provisioner service degraded")
)

// ClaimRequest contains the details an onboarding developer submits.
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

// User is the identity provisioned in the directory service.
type User struct {
	UID         string   `json:"uid"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Groups      []string `json:"groups"`
}

// Credentials are the tokens and VPN profile provisioned for a developer.
type Credentials struct {
	ForgejoUsername string `json:"forgejo_username"`
	ForgejoMCPToken string `json:"forgejo_mcp_token"`
	WireGuardConfig string `json:"wireguard_config"`
}

// WorkspaceInfo is the git remote base and default checkout scope.
type WorkspaceInfo struct {
	GitRemoteBase        string `json:"git_remote_base"`
	DefaultManifestScope string `json:"default_manifest_scope"`
}

// ClaimResponse is the successful provisioning result.
type ClaimResponse struct {
	Status           string        `json:"status"`
	IdempotentReplay bool          `json:"idempotent_replay"`
	User             User          `json:"user"`
	Credentials      Credentials   `json:"credentials"`
	Workspace        WorkspaceInfo `json:"workspace"`
}

// SanitizeUID lowercases raw and keeps only characters a directory UID may
// contain.
func SanitizeUID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var sb strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// MachineFingerprint identifies one machine for claim idempotency.
func MachineFingerprint(hostname, goos, goarch string) string {
	if hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%s-%s", hostname, goos, goarch)
}
