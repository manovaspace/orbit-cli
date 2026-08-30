package onboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func testSecret() []byte {
	return []byte("test-onboard-gateway-secret-key-32bytes-min!")
}

func setupTestServer(t *testing.T, prov provisioner.Provisioner, store *invite.Store) (*Server, []byte) {
	t.Helper()
	secret := testSecret()
	if prov == nil {
		prov = provisioner.NewDevProvisioner()
	}

	cfg := ServerConfig{
		Secret:           secret,
		Provisioner:      prov,
		InviteStore:      store,
		RateLimit:        100,
		RateInterval:     time.Minute,
		IdempotencyTTL:   time.Hour,
		DisableRateLimit: false,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return srv, secret
}

func TestHealthEndpoint(t *testing.T) {
	prov := provisioner.NewMockProvisioner()
	srv, _ := setupTestServer(t, prov, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Healthy check
	resp, err := http.Get(ts.URL + "/v1/onboard/health")
	if err != nil {
		t.Fatalf("GET /v1/onboard/health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" || body["provisioner"] != "healthy" {
		t.Errorf("unexpected body: %+v", body)
	}

	// Alias check /healthz
	resp2, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 on /healthz, got %d", resp2.StatusCode)
	}

	// Degraded check
	prov.HealthFunc = func(ctx context.Context) error {
		return errors.New("lldap unavailable")
	}

	resp3, err := http.Get(ts.URL + "/v1/onboard/health")
	if err != nil {
		t.Fatalf("GET /v1/onboard/health failed: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 for degraded health, got %d", resp3.StatusCode)
	}
}

func TestClaim_Success(t *testing.T) {
	srv, secret := setupTestServer(t, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tokenStr, claims, err := invite.GenerateToken(invite.InviteRequest{
		Email:       "charlie@manova.space",
		DisplayName: "Charlie Day",
		Scope:       "core",
		TTL:         24 * time.Hour,
	}, secret)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claimReq := provisioner.ClaimRequest{
		InviteToken:        tokenStr,
		DesiredUID:         "charlie",
		SSHPublicKey:       "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5... charlie@device",
		MachineFingerprint: "fingerprint-abc",
	}

	reqBody, _ := json.Marshal(claimReq)
	req, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idemp-key-111")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST claim failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var claimResp provisioner.ClaimResponse
	if err := json.NewDecoder(resp.Body).Decode(&claimResp); err != nil {
		t.Fatalf("failed to decode claim response: %v", err)
	}

	if claimResp.Status != "success" {
		t.Errorf("expected status 'success', got %q", claimResp.Status)
	}
	if claimResp.IdempotentReplay {
		t.Errorf("expected IdempotentReplay false on initial claim")
	}
	if claimResp.User.UID != "charlie" {
		t.Errorf("expected UID charlie, got %s", claimResp.User.UID)
	}
	if claimResp.User.Email != claims.Email {
		t.Errorf("expected email %s, got %s", claims.Email, claimResp.User.Email)
	}
	if claimResp.Credentials.ForgejoMCPToken == "" {
		t.Errorf("expected non-empty ForgejoMCPToken")
	}
	if claimResp.Credentials.WireGuardConfig == "" {
		t.Errorf("expected non-empty WireGuardConfig")
	}
}

func TestClaim_IdempotentReplay(t *testing.T) {
	mockProv := provisioner.NewMockProvisioner()
	srv, secret := setupTestServer(t, mockProv, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tokenStr, _, err := invite.GenerateToken(invite.InviteRequest{
		Email:       "dana@manova.space",
		DisplayName: "Dana Scully",
		Scope:       "core",
		TTL:         24 * time.Hour,
	}, secret)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claimReq := provisioner.ClaimRequest{
		InviteToken:        tokenStr,
		DesiredUID:         "dana",
		SSHPublicKey:       "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5... dana@device",
		MachineFingerprint: "fingerprint-xyz",
	}

	idempotencyKey := "idemp-dana-unique-key-12345"
	reqBody, _ := json.Marshal(claimReq)

	// First attempt
	req1, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewReader(reqBody))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", idempotencyKey)

	client := &http.Client{}
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp1.StatusCode)
	}

	var claimResp1 provisioner.ClaimResponse
	_ = json.NewDecoder(resp1.Body).Decode(&claimResp1)
	if claimResp1.IdempotentReplay {
		t.Errorf("expected first response IdempotentReplay == false")
	}

	if len(mockProv.ProvisionCalls) != 1 {
		t.Fatalf("expected exactly 1 provision call, got %d", len(mockProv.ProvisionCalls))
	}

	// Second attempt (Replay with same Idempotency-Key)
	req2, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewReader(reqBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", idempotencyKey)

	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 on replay, got %d", resp2.StatusCode)
	}

	var claimResp2 provisioner.ClaimResponse
	_ = json.NewDecoder(resp2.Body).Decode(&claimResp2)
	if !claimResp2.IdempotentReplay {
		t.Errorf("expected second response IdempotentReplay == true")
	}

	if claimResp2.Credentials.ForgejoMCPToken != claimResp1.Credentials.ForgejoMCPToken {
		t.Errorf("expected identical credentials on replay: %s vs %s",
			claimResp2.Credentials.ForgejoMCPToken, claimResp1.Credentials.ForgejoMCPToken)
	}

	// Verify provisioner was NOT called a second time
	if len(mockProv.ProvisionCalls) != 1 {
		t.Errorf("expected provisioner to NOT be re-executed on idempotency replay, got call count %d", len(mockProv.ProvisionCalls))
	}
}

func TestClaim_InvalidOrExpiredToken(t *testing.T) {
	srv, secret := setupTestServer(t, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{}

	// 1. Malformed token
	badReq, _ := json.Marshal(provisioner.ClaimRequest{
		InviteToken: "not-a-valid-token",
		DesiredUID:  "bad",
	})
	r1, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewReader(badReq))
	r1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(r1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusBadRequest && resp1.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 400 or 401 for malformed token, got %d", resp1.StatusCode)
	}

	// 2. Expired token
	expiredToken, _, _ := invite.GenerateToken(invite.InviteRequest{
		Email: "expired@manova.space",
		TTL:   -1 * time.Hour,
	}, secret)

	expReq, _ := json.Marshal(provisioner.ClaimRequest{
		InviteToken: expiredToken,
		DesiredUID:  "expired",
	})
	r2, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewReader(expReq))
	r2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(r2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for expired token, got %d", resp2.StatusCode)
	}

	// 3. Bad signature (signed with different key)
	otherSecret := []byte("different-secret-key-32bytes-long!")
	badSigToken, _, _ := invite.GenerateToken(invite.InviteRequest{
		Email: "badsig@manova.space",
		TTL:   24 * time.Hour,
	}, otherSecret)

	badSigReq, _ := json.Marshal(provisioner.ClaimRequest{
		InviteToken: badSigToken,
		DesiredUID:  "badsig",
	})
	r3, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewReader(badSigReq))
	r3.Header.Set("Content-Type", "application/json")
	resp3, err := client.Do(r3)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for bad signature, got %d", resp3.StatusCode)
	}
}

func TestClaim_RevokedToken(t *testing.T) {
	tempDir := t.TempDir()
	storeFile := filepath.Join(tempDir, "invites.json")
	store, err := invite.NewStore(storeFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	srv, secret := setupTestServer(t, nil, store)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tokenStr, claims, err := invite.GenerateToken(invite.InviteRequest{
		Email: "revoked@manova.space",
		TTL:   24 * time.Hour,
	}, secret)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Save and revoke in store
	_ = store.SaveInvite(&invite.InviteRecord{
		ID:        claims.ID,
		Email:     claims.Email,
		Token:     tokenStr,
		Revoked:   false,
		IssuedAt:  claims.IssuedAt,
		ExpiresAt: claims.ExpiresAt,
	})
	_, err = store.RevokeInvite(claims.ID)
	if err != nil {
		t.Fatalf("RevokeInvite failed: %v", err)
	}

	claimReq := provisioner.ClaimRequest{
		InviteToken: tokenStr,
		DesiredUID:  "revokeduser",
	}
	reqBody, _ := json.Marshal(claimReq)
	req, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 403 Forbidden or 401 Unauthorized for revoked token, got %d", resp.StatusCode)
	}
}

func TestClaim_RateLimiting(t *testing.T) {
	prov := provisioner.NewMockProvisioner()
	secret := testSecret()

	cfg := ServerConfig{
		Secret:           secret,
		Provisioner:      prov,
		RateLimit:        2, // Allow only 2 requests per interval
		RateInterval:     time.Minute,
		IdempotencyTTL:   time.Hour,
		DisableRateLimit: false,
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{}

	tokenStr, _, _ := invite.GenerateToken(invite.InviteRequest{
		Email: "ratelimit@manova.space",
		TTL:   time.Hour,
	}, secret)

	claimReq, _ := json.Marshal(provisioner.ClaimRequest{
		InviteToken: tokenStr,
		DesiredUID:  "ratelimited",
	})

	sendReq := func() int {
		r, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewReader(claimReq))
		r.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(r)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// 1st request -> 200
	if code := sendReq(); code != http.StatusOK {
		t.Errorf("1st request expected 200, got %d", code)
	}
	// 2nd request -> 200
	if code := sendReq(); code != http.StatusOK {
		t.Errorf("2nd request expected 200, got %d", code)
	}
	// 3rd request -> 429 Too Many Requests
	if code := sendReq(); code != http.StatusTooManyRequests {
		t.Errorf("3rd request expected 429 Too Many Requests, got %d", code)
	}
}

func TestClaim_ProvisionerFailure(t *testing.T) {
	mockProv := provisioner.NewMockProvisioner()
	mockProv.ProvisionFunc = func(ctx context.Context, req provisioner.ClaimRequest) (*provisioner.ClaimResponse, error) {
		return nil, errors.New("upstream directory service unavailable")
	}

	srv, secret := setupTestServer(t, mockProv, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tokenStr, _, _ := invite.GenerateToken(invite.InviteRequest{
		Email: "failtest@manova.space",
		TTL:   time.Hour,
	}, secret)

	claimReq, _ := json.Marshal(provisioner.ClaimRequest{
		InviteToken: tokenStr,
		DesiredUID:  "failtest",
	})

	req, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewReader(claimReq))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500 for provisioner failure, got %d", resp.StatusCode)
	}
}

func TestClaim_InvalidJSON(t *testing.T) {
	srv, _ := setupTestServer(t, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/onboard/claim", bytes.NewBufferString("{not valid json"))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for malformed json, got %d", resp.StatusCode)
	}
}

func TestServer_StartAndShutdown(t *testing.T) {
	prov := provisioner.NewDevProvisioner()
	secret := testSecret()

	srv, err := NewServer(ServerConfig{
		Addr:             "127.0.0.1:0",
		Secret:           secret,
		Provisioner:      prov,
		DisableRateLimit: true,
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown on inactive server should not error, got: %v", err)
	}
}

type mockMailer struct {
	sentInvites    []invite.EmailData
	sentChallenges []invite.OwnerChallengeEmailData
	sendInviteErr  error
	sendOwnerErr   error
}

func (m *mockMailer) SendInvite(ctx context.Context, to string, data invite.EmailData) error {
	if m.sendInviteErr != nil {
		return m.sendInviteErr
	}
	m.sentInvites = append(m.sentInvites, data)
	return nil
}

func (m *mockMailer) SendOwnerChallenge(ctx context.Context, to string, data invite.OwnerChallengeEmailData) error {
	if m.sendOwnerErr != nil {
		return m.sendOwnerErr
	}
	m.sentChallenges = append(m.sentChallenges, data)
	return nil
}

func TestServer_AdminChallengeAndVerify(t *testing.T) {
	mockMailer := &mockMailer{}
	cm := owner.NewChallengeManager()
	srv, err := NewServer(ServerConfig{
		Secret:           []byte("test-secret-12345678901234567890"),
		ChallengeManager: cm,
		Mailer:           mockMailer,
		DisableRateLimit: true,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Request challenge
	reqBody := `{"email":"admin@example.com"}`
	resp, err := http.Post(ts.URL+"/api/v1/admin/challenge", "application/json", strings.NewReader(reqBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("challenge request failed: status %d, err %v", resp.StatusCode, err)
	}

	if len(mockMailer.sentChallenges) != 1 {
		t.Fatalf("expected 1 sent challenge email, got %d", len(mockMailer.sentChallenges))
	}
	otp := mockMailer.sentChallenges[0].OTPCode

	// 2. Verify with invalid code
	badVerify := `{"email":"admin@example.com","code":"000000"}`
	vResp, err := http.Post(ts.URL+"/api/v1/admin/verify", "application/json", strings.NewReader(badVerify))
	if err != nil || vResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad OTP, got %d", vResp.StatusCode)
	}

	// 3. Verify with valid code
	goodVerify := fmt.Sprintf(`{"email":"admin@example.com","code":"%s"}`, otp)
	vResp, err = http.Post(ts.URL+"/api/v1/admin/verify", "application/json", strings.NewReader(goodVerify))
	if err != nil || vResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for valid OTP, got %d", vResp.StatusCode)
	}

	var verifyResp client.VerifyResponse
	if err := json.NewDecoder(vResp.Body).Decode(&verifyResp); err != nil {
		t.Fatalf("failed to decode verify response: %v", err)
	}
	if verifyResp.Status != "verified" {
		t.Errorf("expected status 'verified', got %q", verifyResp.Status)
	}
	if verifyResp.Email != "admin@example.com" {
		t.Errorf("expected email admin@example.com, got %q", verifyResp.Email)
	}
	if verifyResp.KeyFingerprint == "" {
		t.Errorf("expected non-empty key fingerprint")
	}
}

func TestServer_AdminChallengeAndVerify_Aliases(t *testing.T) {
	mockMailer := &mockMailer{}
	cm := owner.NewChallengeManager()
	srv, err := NewServer(ServerConfig{
		Secret:           []byte("test-secret-12345678901234567890"),
		ChallengeManager: cm,
		Mailer:           mockMailer,
		DisableRateLimit: true,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Request challenge via system alias
	reqBody := `{"email":"admin@manova.space"}`
	resp, err := http.Post(ts.URL+"/api/v1/system/ownership/challenge", "application/json", strings.NewReader(reqBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("system challenge request failed: status %d, err %v", resp.StatusCode, err)
	}

	if len(mockMailer.sentChallenges) != 1 {
		t.Fatalf("expected 1 sent challenge email, got %d", len(mockMailer.sentChallenges))
	}
	otp := mockMailer.sentChallenges[0].OTPCode

	// 2. Verify via system alias
	goodVerify := fmt.Sprintf(`{"email":"admin@manova.space","code":"%s","display_name":"Admin"}`, otp)
	vResp, err := http.Post(ts.URL+"/api/v1/system/ownership/verify", "application/json", strings.NewReader(goodVerify))
	if err != nil || vResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for valid system OTP, got %d", vResp.StatusCode)
	}
}

func TestServer_AdminChallenge_Errors(t *testing.T) {
	mockMailer := &mockMailer{}
	cm := owner.NewChallengeManager()
	srv, err := NewServer(ServerConfig{
		Secret:           []byte("test-secret-12345678901234567890"),
		ChallengeManager: cm,
		Mailer:           mockMailer,
		DisableRateLimit: true,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Malformed JSON
	resp, err := http.Post(ts.URL+"/api/v1/admin/challenge", "application/json", strings.NewReader("{invalid-json"))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d", resp.StatusCode)
	}

	// 2. Empty email
	resp, err = http.Post(ts.URL+"/api/v1/admin/challenge", "application/json", strings.NewReader(`{"email":""}`))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty email, got %d", resp.StatusCode)
	}

	// 3. Invalid email format
	resp, err = http.Post(ts.URL+"/api/v1/admin/challenge", "application/json", strings.NewReader(`{"email":"notanemail"}`))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid email format, got %d", resp.StatusCode)
	}

	// 4. Mailer error
	mockMailer.sendOwnerErr = errors.New("smtp connection refused")
	resp, err = http.Post(ts.URL+"/api/v1/admin/challenge", "application/json", strings.NewReader(`{"email":"admin@manova.space"}`))
	if err != nil || resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 for mailer error, got %d", resp.StatusCode)
	}
}

func TestServer_AdminVerify_Errors(t *testing.T) {
	mockMailer := &mockMailer{}
	cm := owner.NewChallengeManager()
	srv, err := NewServer(ServerConfig{
		Secret:           []byte("test-secret-12345678901234567890"),
		ChallengeManager: cm,
		Mailer:           mockMailer,
		DisableRateLimit: true,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Malformed JSON
	resp, err := http.Post(ts.URL+"/api/v1/admin/verify", "application/json", strings.NewReader("{invalid-json"))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d", resp.StatusCode)
	}

	// 2. Empty email
	resp, err = http.Post(ts.URL+"/api/v1/admin/verify", "application/json", strings.NewReader(`{"email":"","code":"123456"}`))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty email, got %d", resp.StatusCode)
	}

	// 3. Empty code
	resp, err = http.Post(ts.URL+"/api/v1/admin/verify", "application/json", strings.NewReader(`{"email":"admin@manova.space","code":""}`))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty code, got %d", resp.StatusCode)
	}

	// 4. Challenge not found
	resp, err = http.Post(ts.URL+"/api/v1/admin/verify", "application/json", strings.NewReader(`{"email":"unknown@manova.space","code":"123456"}`))
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for non-existent challenge, got %d", resp.StatusCode)
	}
}

func TestServer_InstallScript(t *testing.T) {
	prov := provisioner.NewDevProvisioner()
	srv, _ := setupTestServer(t, prov, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Valid root route GET /
	t.Run("Valid_Root", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatalf("GET / failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /: expected status 200, got %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/x-shellscript; charset=utf-8" {
			t.Errorf("GET /: expected Content-Type 'text/x-shellscript; charset=utf-8', got %q", contentType)
		}

		cacheControl := resp.Header.Get("Cache-Control")
		if cacheControl != "no-cache, no-store, must-revalidate" {
			t.Errorf("GET /: expected Cache-Control 'no-cache, no-store, must-revalidate', got %q", cacheControl)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}
		body := string(bodyBytes)
		if !strings.Contains(body, "Do you want to proceed with the installation") {
			t.Errorf("GET /: body missing confirmation prompt text")
		}
	})

	t.Run("Browser_HTML", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET / (browser) failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET / (browser): expected status 200, got %d", resp.StatusCode)
		}
		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/html; charset=utf-8" {
			t.Errorf("GET / (browser): expected Content-Type text/html, got %q", contentType)
		}
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}
		body := string(bodyBytes)
		if !strings.Contains(body, "copy-btn") || !strings.Contains(body, "curl -fsSL orbit.manova.space") {
			t.Errorf("GET / (browser): body missing copy-button landing page")
		}
		if strings.HasPrefix(strings.TrimSpace(body), "#!/") {
			t.Errorf("GET / (browser): got shellscript instead of HTML")
		}
	})

	t.Run("Curl_HTML_Accept_Still_Script", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/html")
		req.Header.Set("User-Agent", "curl/8.5.0")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET / (curl) failed: %v", err)
		}
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/x-shellscript; charset=utf-8" {
			t.Errorf("GET / (curl+html): expected shellscript, got %q", contentType)
		}
	})

	// 2. Unmapped routes (including /install and /install.sh) return 404
	invalidPaths := []string{"/install", "/install.sh", "/unmapped", "/unknown", "/install.sh/extra", "/foo/bar"}
	for _, path := range invalidPaths {
		t.Run("Invalid_"+path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s: expected status 404, got %d", path, resp.StatusCode)
			}
		})
	}
}

func TestServer_ClaimPathAliases(t *testing.T) {
	prov := provisioner.NewDevProvisioner()
	srv, _ := setupTestServer(t, prov, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for _, path := range []string{"/v1/onboard/claim", "/api/v1/onboard/claim", "/api/v1/dev/onboard/claim"} {
		resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			t.Errorf("POST %s: got 404, alias missing", path)
		}
	}
}

func TestServer_AdminGrant_CreateAndVerify_Success(t *testing.T) {
	secret := []byte("secret-key-32-bytes-long-12345678")
	gm := owner.NewGrantManager()
	srv, err := NewServer(ServerConfig{
		Secret:           secret,
		GrantManager:     gm,
		DisableRateLimit: true,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Create grant via authenticated API
	grantReq := `{"email":"sara@manova.space","role":"admin","code":"8492-0194","ttl_seconds":600}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/admin/grants", strings.NewReader(grantReq))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(secret))

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got status %d, err %v", resp.StatusCode, err)
	}
	resp.Body.Close()

	// 2. Verify grant code from client
	verifyReq := `{"email":"sara@manova.space","code":"8492-0194","display_name":"Sara"}`
	vResp, err := http.Post(ts.URL+"/api/v1/admin/verify", "application/json", strings.NewReader(verifyReq))
	if err != nil || vResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK verifying grant code, got %d, err %v", vResp.StatusCode, err)
	}
	defer vResp.Body.Close()

	var verifyResult client.VerifyResponse
	if err := json.NewDecoder(vResp.Body).Decode(&verifyResult); err != nil {
		t.Fatalf("failed to decode verify response: %v", err)
	}
	if verifyResult.Status != "verified" {
		t.Errorf("expected status verified, got %s", verifyResult.Status)
	}
	if verifyResult.Email != "sara@manova.space" {
		t.Errorf("expected email sara@manova.space, got %s", verifyResult.Email)
	}

	// 3. Replay must be rejected
	replayResp, err := http.Post(ts.URL+"/api/v1/admin/verify", "application/json", strings.NewReader(verifyReq))
	if err != nil || replayResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on replay, got %d", replayResp.StatusCode)
	}
	replayResp.Body.Close()
}

func TestServer_DisablePublicChallenges_RejectsSpam(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		Secret:                  []byte("secret-key-32-bytes-long-12345678"),
		DisablePublicChallenges: true,
		DisableRateLimit:        true,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"email":"owner@manova.space"}`
	resp, err := http.Post(ts.URL+"/api/v1/admin/challenge", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST challenge failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden when public challenges are disabled, got %d", resp.StatusCode)
	}
}

func TestServer_AllowedAdminEmails_EnforcesAllowlist(t *testing.T) {
	mockMailer := &mockMailer{}
	cm := owner.NewChallengeManager()
	srv, err := NewServer(ServerConfig{
		Secret:             []byte("secret-key-32-bytes-long-12345678"),
		ChallengeManager:   cm,
		Mailer:             mockMailer,
		AllowedAdminEmails: []string{"admin@manova.space"},
		DisableRateLimit:   true,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Disallowed email -> 403
	badReq := `{"email":"attacker@spam.local"}`
	bResp, err := http.Post(ts.URL+"/api/v1/admin/challenge", "application/json", strings.NewReader(badReq))
	if err != nil || bResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-allowlisted email, got %d", bResp.StatusCode)
	}
	bResp.Body.Close()

	// 2. Allowlisted email -> 200
	goodReq := `{"email":"admin@manova.space"}`
	gResp, err := http.Post(ts.URL+"/api/v1/admin/challenge", "application/json", strings.NewReader(goodReq))
	if err != nil || gResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for allowlisted email, got %d", gResp.StatusCode)
	}
	gResp.Body.Close()
}

func TestServer_RateLimit_HardenedReverseProxy(t *testing.T) {
	mockMailer := &mockMailer{}
	cm := owner.NewChallengeManager()
	srv, err := NewServer(ServerConfig{
		Secret:           []byte("secret-key-32-bytes-long-12345678"),
		ChallengeManager: cm,
		Mailer:           mockMailer,
		TrustedProxies:   []string{"127.0.0.1/32"},
		RateLimit:        2, // 2 reqs per window
		RateInterval:     time.Minute,
		DisableRateLimit: false,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{}

	// Scenario 1: Trusted proxy (127.0.0.1) forwarding client A (203.0.113.1)
	sendViaTrustedProxy := func(clientIP string) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/admin/challenge", strings.NewReader(`{"email":"admin1@manova.space"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("CF-Connecting-IP", clientIP)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// Client A: 1st request -> 200
	if code := sendViaTrustedProxy("203.0.113.1"); code != http.StatusOK {
		t.Errorf("Client A 1st req expected 200, got %d", code)
	}
	// Client A: 2nd request -> 200
	if code := sendViaTrustedProxy("203.0.113.1"); code != http.StatusOK {
		t.Errorf("Client A 2nd req expected 200, got %d", code)
	}
	// Client A: 3rd request -> 429
	if code := sendViaTrustedProxy("203.0.113.1"); code != http.StatusTooManyRequests {
		t.Errorf("Client A 3rd req expected 429, got %d", code)
	}

	// Client B (203.0.113.2) via same trusted proxy is NOT rate limited yet
	if code := sendViaTrustedProxy("203.0.113.2"); code != http.StatusOK {
		t.Errorf("Client B 1st req expected 200, got %d", code)
	}
}

func TestServer_RateLimit_ChallengeEmailExhaustion(t *testing.T) {
	mockMailer := &mockMailer{}
	cm := owner.NewChallengeManager()
	srv, err := NewServer(ServerConfig{
		Secret:           []byte("secret-key-32-bytes-long-12345678"),
		ChallengeManager: cm,
		Mailer:           mockMailer,
		RateLimit:        100, // High IP limit
		DisableRateLimit: false,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{}

	sendChallenge := func(email string) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/admin/challenge", strings.NewReader(fmt.Sprintf(`{"email":%q}`, email)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("challenge request failed: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	targetEmail := "target@manova.space"
	// Default challenge email limit is 3 requests / 10 minutes
	if code := sendChallenge(targetEmail); code != http.StatusOK {
		t.Errorf("1st email challenge expected 200, got %d", code)
	}
	if code := sendChallenge(targetEmail); code != http.StatusOK {
		t.Errorf("2nd email challenge expected 200, got %d", code)
	}
	if code := sendChallenge(targetEmail); code != http.StatusOK {
		t.Errorf("3rd email challenge expected 200, got %d", code)
	}
	// 4th challenge for same email should trigger 429
	if code := sendChallenge(targetEmail); code != http.StatusTooManyRequests {
		t.Errorf("4th email challenge expected 429 Too Many Requests, got %d", code)
	}

	// Different email should still be allowed
	if code := sendChallenge("different@manova.space"); code != http.StatusOK {
		t.Errorf("different email challenge expected 200, got %d", code)
	}
}

func TestServer_RateLimit_SQLitePersistence(t *testing.T) {
	db := sqlite.NewTestDB(t)
	mockMailer := &mockMailer{}
	cm := owner.NewChallengeManager()

	cfg := ServerConfig{
		Secret:           []byte("secret-key-32-bytes-long-12345678"),
		ChallengeManager: cm,
		Mailer:           mockMailer,
		RateLimitStore:   db.RateLimits(),
		RateLimit:        2,
		RateInterval:     time.Minute,
		DisableRateLimit: false,
	}

	// Server Instance 1
	srv1, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server 1: %v", err)
	}
	ts1 := httptest.NewServer(srv1.Handler())
	defer ts1.Close()

	// Exhaust 2 allowed requests on Instance 1
	r1, err := http.Get(ts1.URL + "/health")
	if err != nil {
		t.Fatalf("r1 failed: %v", err)
	}
	r1.Body.Close()
	r2, err := http.Get(ts1.URL + "/health")
	if err != nil {
		t.Fatalf("r2 failed: %v", err)
	}
	r2.Body.Close()

	// Server Instance 2 sharing the SAME SQLite db
	srv2, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server 2: %v", err)
	}
	ts2 := httptest.NewServer(srv2.Handler())
	defer ts2.Close()

	// Request on Instance 2 from same IP should immediately be 429 Too Many Requests
	r3, err := http.Get(ts2.URL + "/health")
	if err != nil {
		t.Fatalf("request to instance 2 failed: %v", err)
	}
	defer r3.Body.Close()

	if r3.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected status 429 on instance 2 due to persistent sqlite rate limits, got %d", r3.StatusCode)
	}
}
