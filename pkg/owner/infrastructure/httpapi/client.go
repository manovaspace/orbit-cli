// Package httpapi implements the owner server port over the Orbit HTTP client.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/owner/domain"
)

// Server talks to the Orbit auth service.
type Server struct {
	Client *client.Client
}

// New builds a Server for baseURL.
func New(baseURL string) *Server {
	return &Server{Client: client.NewClient(baseURL)}
}

// InitiateChallenge dispatches an OTP challenge to the owner email.
func (s *Server) InitiateChallenge(ctx context.Context, email string) (domain.ChallengeDispatch, error) {
	resp, err := s.Client.InitiateOwnerChallenge(ctx, email)
	if err != nil {
		return domain.ChallengeDispatch{}, wrapError(err)
	}
	return domain.ChallengeDispatch{Email: resp.Email}, nil
}

// VerifyChallenge submits the OTP and reports the server's verdict.
func (s *Server) VerifyChallenge(ctx context.Context, email, code string) (domain.Verification, error) {
	resp, err := s.Client.VerifyOwnerChallenge(ctx, email, code)
	if err != nil {
		return domain.Verification{}, wrapError(err)
	}
	return domain.Verification{Status: resp.Status}, nil
}

// CreateGrant registers an 8-digit admin grant on the server.
func (s *Server) CreateGrant(ctx context.Context, email, role, code, masterSecret string, ttl time.Duration) (domain.GrantRegistration, error) {
	resp, err := s.Client.CreateAdminGrant(ctx, email, role, code, masterSecret, ttl)
	if err != nil {
		return domain.GrantRegistration{}, wrapError(err)
	}
	return domain.GrantRegistration{ExpiresAt: resp.ExpiresAt}, nil
}

// wrapError maps transport failures to the network exit code and rejected
// credentials to the auth code.
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
