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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
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
	reqBody := `{"email":"alirezaopmc@gmail.com"}`
	resp, err := http.Post(ts.URL+"/api/v1/admin/challenge", "application/json", strings.NewReader(reqBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("challenge request failed: status %d, err %v", resp.StatusCode, err)
	}

	if len(mockMailer.sentChallenges) != 1 {
		t.Fatalf("expected 1 sent challenge email, got %d", len(mockMailer.sentChallenges))
	}
	otp := mockMailer.sentChallenges[0].OTPCode

	// 2. Verify with invalid code
	badVerify := `{"email":"alirezaopmc@gmail.com","code":"000000"}`
	vResp, err := http.Post(ts.URL+"/api/v1/admin/verify", "application/json", strings.NewReader(badVerify))
	if err != nil || vResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad OTP, got %d", vResp.StatusCode)
	}

	// 3. Verify with valid code
	goodVerify := fmt.Sprintf(`{"email":"alirezaopmc@gmail.com","code":"%s"}`, otp)
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
	if verifyResp.Email != "alirezaopmc@gmail.com" {
		t.Errorf("expected email alirezaopmc@gmail.com, got %q", verifyResp.Email)
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

	validPaths := []string{"/", "/install", "/install.sh"}
	for _, path := range validPaths {
		t.Run("Valid_"+path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET %s: expected status 200, got %d", path, resp.StatusCode)
			}

			contentType := resp.Header.Get("Content-Type")
			if contentType != "text/x-shellscript; charset=utf-8" {
				t.Errorf("GET %s: expected Content-Type 'text/x-shellscript; charset=utf-8', got %q", path, contentType)
			}

			cacheControl := resp.Header.Get("Cache-Control")
			if cacheControl != "no-cache, no-store, must-revalidate" {
				t.Errorf("GET %s: expected Cache-Control 'no-cache, no-store, must-revalidate', got %q", path, cacheControl)
			}

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read response body: %v", err)
			}
			body := string(bodyBytes)
			if !strings.Contains(body, "Do you want to proceed with the installation") {
				t.Errorf("GET %s: body missing confirmation prompt text", path)
			}
		})
	}

	invalidPaths := []string{"/unmapped", "/unknown", "/install.sh/extra", "/foo/bar"}
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

func init() {
	// Clean up any test artifacts if necessary
	_ = os.Getenv("ENV")
}
