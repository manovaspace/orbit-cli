package domain

import "context"

// Provisioner provides the server-side identity, git and network provisioning
// used by the onboarding edge daemon and the testbed mock.
type Provisioner interface {
	Provision(ctx context.Context, req ClaimRequest) (*ClaimResponse, error)
	Rollback(ctx context.Context, uid string) error
	Health(ctx context.Context) error
}
