// Package disabledprovisioner provides the production Provisioner for orbit-server
// when development defaults are off. Every provisioning call is refused; nothing
// is created in LLDAP, Forgejo or WireGuard.
package disabledprovisioner

import (
	"context"
	"errors"

	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
)

// ErrUnavailable is returned by every provisioning call of the disabled provisioner.
var ErrUnavailable = errors.New("provisioning is not available on this server")

// Provisioner refuses all provisioning requests. It implements domain.Provisioner.
type Provisioner struct{}

var _ domain.Provisioner = (*Provisioner)(nil)

// New returns a Provisioner that refuses all provisioning requests.
func New() *Provisioner {
	return &Provisioner{}
}

// Provision always returns ErrUnavailable.
func (*Provisioner) Provision(_ context.Context, _ domain.ClaimRequest) (*domain.ClaimResponse, error) {
	return nil, ErrUnavailable
}

// Rollback always returns ErrUnavailable.
func (*Provisioner) Rollback(_ context.Context, _ string) error {
	return ErrUnavailable
}

// Health reports healthy: the daemon is up and provisioning is intentionally
// disabled, so orchestrator health probes must not mark the edge as degraded.
func (*Provisioner) Health(_ context.Context) error {
	return nil
}
