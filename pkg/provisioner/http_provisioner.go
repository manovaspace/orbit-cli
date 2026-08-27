package provisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/client"
)

var _ Provisioner = (*HTTPProvisioner)(nil)

// HTTPProvisioner implements the Provisioner interface by communicating
// with a remote Orbit API / onboarding server over HTTP.
type HTTPProvisioner struct {
	client *client.Client
}

// NewHTTPProvisioner constructs a new HTTPProvisioner wrapping the provided HTTP client.
func NewHTTPProvisioner(c *client.Client) *HTTPProvisioner {
	return &HTTPProvisioner{
		client: c,
	}
}

// NewHTTPProvisionerFromURL constructs a new HTTPProvisioner with an initialized Client for baseURL.
func NewHTTPProvisionerFromURL(baseURL string, opts ...client.Option) *HTTPProvisioner {
	return &HTTPProvisioner{
		client: client.NewClient(baseURL, opts...),
	}
}

// Client returns the underlying Client instance.
func (p *HTTPProvisioner) Client() *client.Client {
	return p.client
}

// Provision sends an onboarding claim request to the remote Orbit API server.
func (p *HTTPProvisioner) Provision(ctx context.Context, req ClaimRequest) (*ClaimResponse, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("%w: nil http client", ErrProvisionFailed)
	}

	cReq := client.ClaimRequest{
		InviteToken:        req.InviteToken,
		DesiredUID:         req.DesiredUID,
		Email:              req.Email,
		DisplayName:        req.DisplayName,
		SSHPublicKey:       req.SSHPublicKey,
		MachineFingerprint: req.MachineFingerprint,
		Scope:              req.Scope,
		Metadata:           req.Metadata,
	}

	resp, err := p.client.ClaimToken(ctx, cReq)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case 409:
				return nil, fmt.Errorf("%w: %s", ErrUserAlreadyExists, apiErr.Message)
			case 400:
				return nil, fmt.Errorf("%w: %s", ErrInvalidRequest, apiErr.Message)
			case 503:
				return nil, fmt.Errorf("%w: %s", ErrServiceDegraded, apiErr.Message)
			default:
				return nil, fmt.Errorf("%w: %s", ErrProvisionFailed, apiErr.Message)
			}
		}
		return nil, fmt.Errorf("%w: %w", ErrProvisionFailed, err)
	}

	return &ClaimResponse{
		Status:           resp.Status,
		IdempotentReplay: resp.IdempotentReplay,
		User: User{
			UID:         resp.User.UID,
			Email:       resp.User.Email,
			DisplayName: resp.User.DisplayName,
			Groups:      resp.User.Groups,
		},
		Credentials: Credentials{
			ForgejoUsername: resp.Credentials.ForgejoUsername,
			ForgejoMCPToken: resp.Credentials.ForgejoMCPToken,
			WireGuardConfig: resp.Credentials.WireGuardConfig,
		},
		Workspace: WorkspaceInfo{
			GitRemoteBase:        resp.Workspace.GitRemoteBase,
			DefaultManifestScope: resp.Workspace.DefaultManifestScope,
		},
	}, nil
}

// Rollback requests a rollback of provisioned resources for the given UID on the remote server.
func (p *HTTPProvisioner) Rollback(ctx context.Context, uid string) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("%w: nil http client", ErrRollbackFailed)
	}

	cleanUID := strings.TrimSpace(uid)
	if cleanUID == "" {
		return fmt.Errorf("%w: missing uid", ErrInvalidRequest)
	}

	if err := p.client.Rollback(ctx, cleanUID); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			return fmt.Errorf("%w: %s", ErrRollbackFailed, apiErr.Message)
		}
		return fmt.Errorf("%w: %w", ErrRollbackFailed, err)
	}

	return nil
}

// Health verifies the health and connectivity to the remote server.
func (p *HTTPProvisioner) Health(ctx context.Context) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("%w: nil http client", ErrServiceDegraded)
	}

	if err := p.client.Health(ctx); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			return fmt.Errorf("%w: %s", ErrServiceDegraded, apiErr.Message)
		}
		return fmt.Errorf("%w: %w", ErrServiceDegraded, err)
	}

	return nil
}
