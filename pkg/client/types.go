package client

import (
	"fmt"
	"time"
)

// ChallengeRequest represents a request payload to initiate an owner OTP challenge.
type ChallengeRequest struct {
	Email string `json:"email"`
}

// ChallengeResponse represents the server response after initiating an owner OTP challenge.
type ChallengeResponse struct {
	Status    string    `json:"status"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
	Message   string    `json:"message,omitempty"`
}

// VerifyRequest represents a request payload to verify an owner challenge OTP code.
type VerifyRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	DisplayName string `json:"display_name,omitempty"`
}

// VerifyResponse represents the server response after successful owner OTP verification.
type VerifyResponse struct {
	Status         string    `json:"status"`
	Email          string    `json:"email"`
	DisplayName    string    `json:"display_name,omitempty"`
	KeyFingerprint string    `json:"key_fingerprint"`
	VerifiedAt     time.Time `json:"verified_at"`
	Message        string    `json:"message,omitempty"`
}

// AdminStatusResponse represents the owner verification and vault status returned by admin status endpoint.
type AdminStatusResponse struct {
	Verified         bool   `json:"verified"`
	Email            string `json:"email,omitempty"`
	DisplayName      string `json:"display_name,omitempty"`
	VerifiedAt       string `json:"verified_at,omitempty"`
	KeyFingerprint   string `json:"key_fingerprint,omitempty"`
	VaultLocation    string `json:"vault_location,omitempty"`
	VaultPermissions string `json:"vault_permissions,omitempty"`
	PermissionsValid bool   `json:"permissions_valid"`
	MailHost         string `json:"mail_host,omitempty"`
}

// RotateSecretResponse represents the response after rotating root signing secret.
type RotateSecretResponse struct {
	Status         string    `json:"status"`
	Email          string    `json:"email"`
	KeyFingerprint string    `json:"key_fingerprint"`
	RotatedAt      time.Time `json:"rotated_at"`
	Message        string    `json:"message,omitempty"`
}

// ClaimRequest contains developer information and credentials submitted during onboarding claim.
type ClaimRequest struct {
	InviteToken        string            `json:"invite_token"`
	DesiredUID         string            `json:"desired_uid,omitempty"`
	Email              string            `json:"email,omitempty"`
	DisplayName        string            `json:"display_name,omitempty"`
	SSHPublicKey       string            `json:"ssh_public_key"`
	MachineFingerprint string            `json:"machine_fingerprint,omitempty"`
	Scope              string            `json:"scope,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// User models provisioned identity data.
type User struct {
	UID         string   `json:"uid"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Groups      []string `json:"groups"`
}

// Credentials models developer tokens and VPN profile.
type Credentials struct {
	ForgejoUsername string `json:"forgejo_username"`
	ForgejoMCPToken string `json:"forgejo_mcp_token"`
	WireGuardConfig string `json:"wireguard_config"`
}

// WorkspaceInfo models git base URL and default manifest scopes.
type WorkspaceInfo struct {
	GitRemoteBase        string `json:"git_remote_base"`
	DefaultManifestScope string `json:"default_manifest_scope"`
}

// ClaimResponse represents the provisioning response from the onboarding endpoint.
type ClaimResponse struct {
	Status           string        `json:"status"`
	IdempotentReplay bool          `json:"idempotent_replay"`
	User             User          `json:"user"`
	Credentials      Credentials   `json:"credentials"`
	Workspace        WorkspaceInfo `json:"workspace"`
}

// RollbackRequest represents a request to undo provisioning for a UID.
type RollbackRequest struct {
	UID string `json:"uid"`
}

// RollbackResponse represents the response of a rollback operation.
type RollbackResponse struct {
	Status  string `json:"status"`
	UID     string `json:"uid"`
	Message string `json:"message,omitempty"`
}

// HealthResponse represents the health check response payload.
type HealthResponse struct {
	Status      string `json:"status"`
	Provisioner string `json:"provisioner,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
	Error       string `json:"error,omitempty"`
	Code        int    `json:"code,omitempty"`
}

// APIError represents structured error responses returned by Orbit API services.
type APIError struct {
	StatusCode        int    `json:"code"`
	Message           string `json:"error"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

// Error formats the APIError into a human-readable string.
func (e *APIError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("api error (%d): %s", e.StatusCode, e.Message)
	}
	return e.Message
}
