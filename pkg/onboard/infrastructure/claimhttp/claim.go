// Package claimhttp talks to the onboarding edge daemon over HTTP: it submits
// invitation claims for the CLI and speaks the Orbit API for server-side
// provisioners.
package claimhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
)

// Client submits onboarding claims to the edge daemon.
type Client struct {
	HTTP *http.Client
	// Timeout is used when HTTP is nil.
	Timeout time.Duration
}

// New builds a Client with the CLI's 15s claim timeout.
func New() *Client {
	return &Client{Timeout: 15 * time.Second}
}

// Claim posts one claim request with an idempotency key and returns the
// provisioned identity.
func (c *Client) Claim(ctx context.Context, edgeURL, idempotencyKey string, req domain.ClaimRequest) (*domain.ClaimResponse, error) {
	url := strings.TrimRight(edgeURL, "/") + "/api/v1/onboard/claim"

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Failed("failed to serialize claim request"))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqData))
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Failed("failed to create HTTP request"))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := c.client().Do(httpReq)
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Network(fmt.Sprintf("failed to reach onboarding edge at %s", url)))
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierr.Wrap(err, clierr.Network("failed to read response body"))
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
			Code  int    `json:"code"`
		}
		message := ""
		if json.Unmarshal(bodyBytes, &errResp) == nil && errResp.Error != "" {
			message = fmt.Sprintf("claim rejected (%d): %s", resp.StatusCode, errResp.Error)
		} else {
			message = fmt.Sprintf("claim rejected with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
		}
		return nil, statusError(resp.StatusCode, message)
	}

	var claimResp domain.ClaimResponse
	if err := json.Unmarshal(bodyBytes, &claimResp); err != nil {
		return nil, clierr.Wrap(err, clierr.Failed("failed to parse claim response"))
	}

	return &claimResp, nil
}

// Rollback asks the edge daemon to release the resources provisioned for uid.
// The rollback is best effort: callers report failure but never block on it.
func (c *Client) Rollback(ctx context.Context, edgeURL, uid string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/v1/onboard/rollback/%s", strings.TrimRight(edgeURL, "/"), uid)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("failed to create rollback request"))
	}
	resp, err := c.client().Do(httpReq)
	if err != nil {
		return clierr.Wrap(err, clierr.Network(fmt.Sprintf("failed to reach onboarding edge at %s", url)))
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return statusError(resp.StatusCode, fmt.Sprintf("rollback rejected with HTTP %d", resp.StatusCode))
	}
	return nil
}

// client returns the configured HTTP client with the CLI timeout.
func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// statusError maps an HTTP rejection to the closest CLI exit code.
func statusError(status int, message string) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return clierr.Auth(message)
	case http.StatusBadRequest:
		return clierr.Usage(message)
	case http.StatusConflict:
		return clierr.Failed(message)
	default:
		return clierr.Network(message)
	}
}
