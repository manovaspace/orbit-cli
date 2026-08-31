package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout     = 15 * time.Second
	defaultMaxRetries  = 2
	defaultBackoff     = 100 * time.Millisecond
	defaultUserAgent   = "orbit-cli/1.0"
	defaultServerBase  = "http://localhost:8080"
)

// Option configures a Client instance.
type Option func(*Client)

// WithHTTPClient sets a custom underlying http.Client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithTimeout configures the default HTTP client request timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout > 0 {
			c.timeout = timeout
			if c.httpClient != nil {
				c.httpClient.Timeout = timeout
			}
		}
	}
}

// WithHeader adds a custom default header to all outbound requests.
func WithHeader(key, value string) Option {
	return func(c *Client) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.headers[key] = value
	}
}

// WithHeaders sets multiple default headers.
func WithHeaders(headers map[string]string) Option {
	return func(c *Client) {
		c.mu.Lock()
		defer c.mu.Unlock()
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

// WithBearerToken configures the Authorization header with a Bearer token.
func WithBearerToken(token string) Option {
	return func(c *Client) {
		if strings.TrimSpace(token) != "" {
			WithHeader("Authorization", "Bearer "+strings.TrimSpace(token))(c)
		}
	}
}

// WithRetry configures max retry attempts and backoff duration for transient errors.
func WithRetry(maxRetries int, backoff time.Duration) Option {
	return func(c *Client) {
		if maxRetries >= 0 {
			c.maxRetries = maxRetries
		}
		if backoff > 0 {
			c.retryBackoff = backoff
		}
	}
}

// WithMaxRetries configures the maximum retry attempts for transient errors.
func WithMaxRetries(maxRetries int) Option {
	return func(c *Client) {
		if maxRetries >= 0 {
			c.maxRetries = maxRetries
		}
	}
}

// WithRetryBackoff configures the backoff duration between retries.
func WithRetryBackoff(backoff time.Duration) Option {
	return func(c *Client) {
		if backoff > 0 {
			c.retryBackoff = backoff
		}
	}
}

// Client provides an HTTP client to communicate with the Orbit API Server.
type Client struct {
	baseURL      string
	httpClient   *http.Client
	timeout      time.Duration
	headers      map[string]string
	maxRetries   int
	retryBackoff time.Duration
	mu           sync.RWMutex
}

// NewClient creates and initializes a new Client for communicating with the Orbit API server.
func NewClient(baseURL string, opts ...Option) *Client {
	cleanBase := strings.TrimSpace(baseURL)
	if cleanBase == "" {
		cleanBase = defaultServerBase
	}
	cleanBase = strings.TrimRight(cleanBase, "/")
	if !strings.HasPrefix(cleanBase, "http://") && !strings.HasPrefix(cleanBase, "https://") {
		cleanBase = "http://" + cleanBase
	}

	c := &Client{
		baseURL:      cleanBase,
		timeout:      defaultTimeout,
		httpClient:   &http.Client{Timeout: defaultTimeout},
		headers:      make(map[string]string),
		maxRetries:   defaultMaxRetries,
		retryBackoff: defaultBackoff,
	}

	c.headers["User-Agent"] = defaultUserAgent

	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}

	return c
}

// BaseURL returns the configured base URL of the client.
func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// SetBaseURL updates the configured base URL.
func (c *Client) SetBaseURL(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	clean := strings.TrimSpace(url)
	if clean == "" {
		clean = defaultServerBase
	}
	clean = strings.TrimRight(clean, "/")
	if !strings.HasPrefix(clean, "http://") && !strings.HasPrefix(clean, "https://") {
		clean = "http://" + clean
	}
	c.baseURL = clean
}

// InitiateOwnerChallenge dispatches an OTP challenge to the given owner email address.
func (c *Client) InitiateOwnerChallenge(ctx context.Context, email string) (*ChallengeResponse, error) {
	normEmail := strings.TrimSpace(email)
	if normEmail == "" {
		return nil, errors.New("email cannot be empty")
	}

	reqBody := ChallengeRequest{Email: normEmail}
	var respBody ChallengeResponse
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/admin/challenge", reqBody, &respBody, nil); err != nil {
		return nil, err
	}
	return &respBody, nil
}

// VerifyOwnerChallenge verifies an owner OTP challenge code and seals the owner identity.
func (c *Client) VerifyOwnerChallenge(ctx context.Context, email, code string) (*VerifyResponse, error) {
	normEmail := strings.TrimSpace(email)
	normCode := strings.TrimSpace(code)

	if normEmail == "" {
		return nil, errors.New("email cannot be empty")
	}
	if normCode == "" {
		return nil, errors.New("verification code cannot be empty")
	}

	reqBody := VerifyRequest{
		Email: normEmail,
		Code:  normCode,
	}
	var respBody VerifyResponse
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/admin/verify", reqBody, &respBody, nil); err != nil {
		return nil, err
	}
	return &respBody, nil
}

// CreateAdminGrant registers an 8-digit admin grant on the server using master secret authentication.
func (c *Client) CreateAdminGrant(ctx context.Context, email, role, code, masterSecret string, ttl time.Duration) (*CreateGrantResponse, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	if normEmail == "" {
		return nil, errors.New("email cannot be empty")
	}

	reqBody := CreateGrantRequest{
		Email:      normEmail,
		Role:       role,
		Code:       code,
		TTLSeconds: int(ttl.Seconds()),
	}

	var respBody CreateGrantResponse
	headers := map[string]string{
		"Authorization": "Bearer " + strings.TrimSpace(masterSecret),
	}

	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/admin/grants", reqBody, &respBody, headers); err != nil {
		return nil, err
	}
	return &respBody, nil
}

// AdminStatus retrieves platform ownership verification status, vault integrity, and mail configuration.
func (c *Client) AdminStatus(ctx context.Context) (*AdminStatusResponse, error) {
	var respBody AdminStatusResponse
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/admin/status", nil, &respBody, nil); err != nil {
		return nil, err
	}
	return &respBody, nil
}

// RotateSecret requests rotation of the platform root master signing secret.
func (c *Client) RotateSecret(ctx context.Context) (*RotateSecretResponse, error) {
	var respBody RotateSecretResponse
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/admin/rotate-secret", nil, &respBody, nil); err != nil {
		return nil, err
	}
	return &respBody, nil
}

// ClaimToken submits an onboarding claim request to provision credentials and workspaces.
func (c *Client) ClaimToken(ctx context.Context, req ClaimRequest) (*ClaimResponse, error) {
	if strings.TrimSpace(req.InviteToken) == "" {
		return nil, errors.New("invite token cannot be empty")
	}

	headers := make(map[string]string)
	idempKey := computeIdempotencyKey(req.InviteToken, req.MachineFingerprint)
	if idempKey != "" {
		headers["Idempotency-Key"] = idempKey
	}

	var respBody ClaimResponse
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/onboard/claim", req, &respBody, headers); err != nil {
		return nil, err
	}
	return &respBody, nil
}

// Rollback requests a rollback of provisioned resources for the given UID.
func (c *Client) Rollback(ctx context.Context, uid string) error {
	cleanUID := strings.TrimSpace(uid)
	if cleanUID == "" {
		return errors.New("uid cannot be empty")
	}

	reqBody := RollbackRequest{UID: cleanUID}
	var respBody RollbackResponse
	return c.doRequest(ctx, http.MethodPost, "/api/v1/onboard/rollback", reqBody, &respBody, nil)
}

// Health checks the health and readiness of the remote server.
func (c *Client) Health(ctx context.Context) error {
	var respBody HealthResponse
	if err := c.doRequest(ctx, http.MethodGet, "/healthz", nil, &respBody, nil); err != nil {
		return err
	}
	if respBody.Status != "" && respBody.Status != "ok" && respBody.Status != "healthy" {
		return fmt.Errorf("server reported unhealthy status: %s (error: %s)", respBody.Status, respBody.Error)
	}
	return nil
}

// doRequest performs an HTTP request with JSON marshaling, retry logic, and error decoding.
func (c *Client) doRequest(ctx context.Context, method, path string, reqBody any, respBody any, extraHeaders map[string]string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var bodyBytes []byte
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to encode request JSON: %w", err)
		}
		bodyBytes = data
	}

	fullURL := c.BaseURL() + path

	maxAttempts := 1 + c.maxRetries
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retryBackoff * time.Duration(1<<(attempt-1))):
			}
		}

		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return fmt.Errorf("failed to create HTTP request: %w", err)
		}

		// Apply default headers
		c.mu.RLock()
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		c.mu.RUnlock()

		// Apply Content-Type if body present
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")

		// Apply extra headers
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ctx.Err()
			}
			// Transient network error, retry
			continue
		}

		respData, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", readErr)
			continue
		}

		// Handle retryable status codes
		if isRetriableStatusCode(resp.StatusCode) && attempt < maxAttempts-1 {
			retryAfter := extractRetryAfter(resp)
			if retryAfter > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(retryAfter):
				}
			}
			lastErr = decodeAPIError(resp.StatusCode, respData)
			continue
		}

		// Handle error responses
		if resp.StatusCode >= 400 {
			return decodeAPIError(resp.StatusCode, respData)
		}

		// Success: unmarshal if requested and body is present
		if respBody != nil && len(respData) > 0 && resp.StatusCode != http.StatusNoContent {
			if err := json.Unmarshal(respData, respBody); err != nil {
				return fmt.Errorf("failed to decode response JSON: %w", err)
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("request failed after retries")
}

func isRetriableStatusCode(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

func extractRetryAfter(resp *http.Response) time.Duration {
	headerVal := resp.Header.Get("Retry-After")
	if headerVal == "" {
		return 0
	}
	sec, err := strconv.Atoi(headerVal)
	if err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return 0
}

func decodeAPIError(statusCode int, body []byte) error {
	var errResp struct {
		Error             string `json:"error"`
		Message           string `json:"message"`
		Code              int    `json:"code"`
		RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil {
		msg := errResp.Error
		if msg == "" {
			msg = errResp.Message
		}
		if msg != "" {
			code := errResp.Code
			if code == 0 {
				code = statusCode
			}
			return &APIError{
				StatusCode:        code,
				Message:           msg,
				RetryAfterSeconds: errResp.RetryAfterSeconds,
			}
		}
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		trimmed = http.StatusText(statusCode)
	}

	return &APIError{
		StatusCode: statusCode,
		Message:    trimmed,
	}
}

func computeIdempotencyKey(token, fingerprint string) string {
	if token == "" && fingerprint == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token + ":" + fingerprint))
	return hex.EncodeToString(sum[:])
}
