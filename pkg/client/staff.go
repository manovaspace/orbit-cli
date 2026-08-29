package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/staffhmac"
)

const defaultStaffURL = "https://staff.dev.manova.space"

// StaffClient talks to orbit-staff with owner HMAC signing.
type StaffClient struct {
	baseURL    string
	secret     string
	httpClient *http.Client
}

// StaffMember is a staff directory record (snake_case JSON).
type StaffMember struct {
	UID             string   `json:"uid"`
	DisplayName     string   `json:"display_name"`
	Mail            string   `json:"mail"`
	PersonalForward string   `json:"personal_forward"`
	Groups          []string `json:"groups"`
	Status          string   `json:"status"`
}

// StaffCreateInput is the create request body plus optional idempotency key.
type StaffCreateInput struct {
	UID             string   `json:"uid"`
	DisplayName     string   `json:"display_name"`
	PersonalForward string   `json:"personal_forward"`
	Groups          []string `json:"groups"`
	TOTP            bool     `json:"totp"`
	IdempotencyKey  string   `json:"-"`
}

// StaffCreateResult is the create response including one-time secrets.
type StaffCreateResult struct {
	StaffMember
	LDAPPassword string `json:"ldap_password,omitempty"`
	MailPassword string `json:"mail_password,omitempty"`
	OTPAuth      string `json:"otpauth,omitempty"`
	Idempotent   bool   `json:"idempotent"`
}

// StaffUpdateInput is the PATCH body.
type StaffUpdateInput struct {
	DisplayName     string   `json:"display_name"`
	PersonalForward string   `json:"personal_forward"`
	Groups          []string `json:"groups"`
}

// StaffResetResult holds rotated passwords.
type StaffResetResult struct {
	LDAPPassword string `json:"ldap_password"`
	MailPassword string `json:"mail_password"`
}

// NewStaffClient builds a client for the staff control plane.
func NewStaffClient(baseURL, secret string) *StaffClient {
	clean := strings.TrimSpace(baseURL)
	if clean == "" {
		clean = defaultStaffURL
	}
	clean = strings.TrimRight(clean, "/")
	if !strings.HasPrefix(clean, "http://") && !strings.HasPrefix(clean, "https://") {
		clean = "https://" + clean
	}
	return &StaffClient{
		baseURL:    clean,
		secret:     secret,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// Create POSTs /api/v1/staff with HMAC and Idempotency-Key.
func (c *StaffClient) Create(ctx context.Context, in StaffCreateInput) (*StaffCreateResult, error) {
	headers := map[string]string{}
	if k := strings.TrimSpace(in.IdempotencyKey); k != "" {
		headers["Idempotency-Key"] = k
	}
	var out StaffCreateResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/staff", in, &out, headers); err != nil {
		return nil, err
	}
	return &out, nil
}

// List GETs /api/v1/staff.
func (c *StaffClient) List(ctx context.Context) ([]StaffMember, error) {
	var out []StaffMember
	if err := c.do(ctx, http.MethodGet, "/api/v1/staff", nil, &out, nil); err != nil {
		return nil, err
	}
	return out, nil
}

// Get GETs /api/v1/staff/{uid}.
func (c *StaffClient) Get(ctx context.Context, uid string) (*StaffMember, error) {
	var out StaffMember
	if err := c.do(ctx, http.MethodGet, staffPath(uid), nil, &out, nil); err != nil {
		return nil, err
	}
	return &out, nil
}

// Update PATCHes /api/v1/staff/{uid}.
func (c *StaffClient) Update(ctx context.Context, uid string, in StaffUpdateInput) (*StaffMember, error) {
	var out StaffMember
	if err := c.do(ctx, http.MethodPatch, staffPath(uid), in, &out, nil); err != nil {
		return nil, err
	}
	return &out, nil
}

// Disable POSTs /api/v1/staff/{uid}/disable.
func (c *StaffClient) Disable(ctx context.Context, uid string) error {
	return c.do(ctx, http.MethodPost, staffPath(uid)+"/disable", nil, nil, nil)
}

// Enable POSTs /api/v1/staff/{uid}/enable.
func (c *StaffClient) Enable(ctx context.Context, uid string) error {
	return c.do(ctx, http.MethodPost, staffPath(uid)+"/enable", nil, nil, nil)
}

// Delete DELETEs /api/v1/staff/{uid}.
func (c *StaffClient) Delete(ctx context.Context, uid string) error {
	return c.do(ctx, http.MethodDelete, staffPath(uid), nil, nil, nil)
}

// ResetPassword POSTs reset-password with targets ldap/mailbox.
func (c *StaffClient) ResetPassword(ctx context.Context, uid string, ldap, mailbox bool) (*StaffResetResult, error) {
	targets := make([]string, 0, 2)
	if ldap {
		targets = append(targets, "ldap")
	}
	if mailbox {
		targets = append(targets, "mailbox")
	}
	if len(targets) == 0 {
		targets = []string{"ldap", "mailbox"}
	}
	body := map[string][]string{"targets": targets}
	var out StaffResetResult
	if err := c.do(ctx, http.MethodPost, staffPath(uid)+"/reset-password", body, &out, nil); err != nil {
		return nil, err
	}
	return &out, nil
}

func staffPath(uid string) string {
	return "/api/v1/staff/" + strings.TrimSpace(uid)
}

func (c *StaffClient) do(ctx context.Context, method, path string, reqBody, respBody any, extra map[string]string) error {
	var body []byte
	var err error
	if reqBody != nil {
		body, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
	}
	ts := time.Now().Unix()
	sig := staffhmac.Sign(c.secret, ts, method, path, body)

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Orbit-Signature", sig)
	for k, v := range extra {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return &APIError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(respBytes))}
	}
	if respBody == nil || resp.StatusCode == http.StatusNoContent || len(respBytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBytes, respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
