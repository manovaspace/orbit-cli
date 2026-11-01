// Package staffapi implements the staff directory port over the staff HTTP client.
package staffapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/staff/domain"
)

// Directory calls orbit-staff through the HMAC client.
type Directory struct {
	Client *client.StaffClient
}

// New builds a Directory over c.
func New(c *client.StaffClient) *Directory {
	return &Directory{Client: c}
}

// Create POSTs a new staff member.
func (d *Directory) Create(ctx context.Context, in domain.CreateInput) (*domain.CreateResult, error) {
	res, err := d.Client.Create(ctx, client.StaffCreateInput{
		UID:             in.UID,
		DisplayName:     in.DisplayName,
		FirstName:       in.FirstName,
		LastName:        in.LastName,
		PersonalForward: in.PersonalForward,
		Groups:          in.Groups,
		IdempotencyKey:  in.IdempotencyKey,
	})
	if err != nil {
		return nil, wrapError(err)
	}
	return &domain.CreateResult{
		Member:       member(res.StaffMember),
		LDAPPassword: res.LDAPPassword,
		Idempotent:   res.Idempotent,
	}, nil
}

// List returns every staff member.
func (d *Directory) List(ctx context.Context) ([]domain.Member, error) {
	members, err := d.Client.List(ctx)
	if err != nil {
		return nil, wrapError(err)
	}
	out := make([]domain.Member, 0, len(members))
	for _, m := range members {
		out = append(out, member(m))
	}
	return out, nil
}

// Get returns one staff member.
func (d *Directory) Get(ctx context.Context, uid string) (*domain.Member, error) {
	m, err := d.Client.Get(ctx, uid)
	if err != nil {
		return nil, wrapError(err)
	}
	out := member(*m)
	return &out, nil
}

// Update PATCHes a staff member.
func (d *Directory) Update(ctx context.Context, uid string, in domain.UpdateInput) (*domain.Member, error) {
	m, err := d.Client.Update(ctx, uid, client.StaffUpdateInput{
		DisplayName:     in.DisplayName,
		FirstName:       in.FirstName,
		LastName:        in.LastName,
		PersonalForward: in.PersonalForward,
		Groups:          in.Groups,
	})
	if err != nil {
		return nil, wrapError(err)
	}
	out := member(*m)
	return &out, nil
}

// Disable deactivates a staff member.
func (d *Directory) Disable(ctx context.Context, uid string) error {
	return wrapError(d.Client.Disable(ctx, uid))
}

// Enable reactivates a staff member.
func (d *Directory) Enable(ctx context.Context, uid string) error {
	return wrapError(d.Client.Enable(ctx, uid))
}

// Delete removes a staff member.
func (d *Directory) Delete(ctx context.Context, uid string) error {
	return wrapError(d.Client.Delete(ctx, uid))
}

// ResetPassword rotates the shared LLDAP password.
func (d *Directory) ResetPassword(ctx context.Context, uid string, ldap, mailbox bool) (*domain.ResetResult, error) {
	res, err := d.Client.ResetPassword(ctx, uid, ldap, mailbox)
	if err != nil {
		return nil, wrapError(err)
	}
	return &domain.ResetResult{LDAPPassword: res.LDAPPassword, Warning: res.Warning}, nil
}

func member(m client.StaffMember) domain.Member {
	return domain.Member{
		UID:             m.UID,
		DisplayName:     m.DisplayName,
		FirstName:       m.FirstName,
		LastName:        m.LastName,
		Mail:            m.Mail,
		PersonalForward: m.PersonalForward,
		Groups:          m.Groups,
		Status:          m.Status,
	}
}

// wrapError maps transport failures to the network exit code and rejected
// credentials to the auth code; API-level errors keep their message.
func wrapError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
			return clierr.Wrap(err, clierr.Auth(apiErr.Message))
		}
		return err
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return clierr.Wrap(err, clierr.Network(err.Error()))
	}
	return err
}
