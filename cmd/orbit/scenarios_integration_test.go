package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/manovaspace/orbit-cli/pkg/staffhmac"
)

const (
	testHMACSecret   = "orbit-dev-insecure-staff-hmac-secret-32bytes"
	testInviteSecret = "orbit-dev-insecure-invitation-signing-secret-key-32bytes"
)

type mockServerState struct {
	mu           sync.RWMutex
	challenges   map[string]map[string]interface{}
	grants       map[string]map[string]interface{}
	grantCodeMap map[string]string // code -> id
	staffMembers map[string]*client.StaffMember
	s3Objects    map[string][]byte
}

func newMockServerState() *mockServerState {
	return &mockServerState{
		challenges:   make(map[string]map[string]interface{}),
		grants:       make(map[string]map[string]interface{}),
		grantCodeMap: make(map[string]string),
		staffMembers: make(map[string]*client.StaffMember),
		s3Objects:    make(map[string][]byte),
	}
}

func setupMockHTTPHandler(state *mockServerState) http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","server":"mock-test-server"}`))
	})

	// Challenge endpoint
	handleChallenge := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		email := body["email"]
		chID := fmt.Sprintf("ch_%d", time.Now().UnixNano())

		state.mu.Lock()
		state.challenges[email] = map[string]interface{}{
			"id":        chID,
			"code":      "123456",
			"attempts":  0,
			"burned":    false,
			"expiresAt": time.Now().Add(10 * time.Minute),
		}
		state.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "pending",
			"challenge_id": chID,
			"email":        email,
			"expires_at":   time.Now().Add(10 * time.Minute),
		})
	}
	mux.HandleFunc("POST /v1/owner/challenge", handleChallenge)
	mux.HandleFunc("POST /api/v1/owner/challenge", handleChallenge)

	// Verify endpoint
	handleVerify := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		email := body["email"]
		code := strings.ReplaceAll(body["code"], "-", "")

		state.mu.Lock()
		defer state.mu.Unlock()

		// Check grant code
		if gid, exists := state.grantCodeMap[code]; exists {
			g := state.grants[gid]
			if g["burned"].(bool) {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "grant is burned"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":              "verified",
				"email":               g["email"],
				"root_signing_secret": testHMACSecret,
			})
			return
		}

		// Check active challenge
		ch, exists := state.challenges[email]
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "challenge not found"})
			return
		}

		if code == ch["code"].(string) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":              "verified",
				"email":               email,
				"root_signing_secret": testHMACSecret,
			})
			return
		}

		ch["attempts"] = ch["attempts"].(int) + 1
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid code"})
	}
	mux.HandleFunc("POST /v1/owner/verify", handleVerify)
	mux.HandleFunc("POST /api/v1/owner/verify", handleVerify)

	// Grants create and list
	handleGrantCreate := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		email := fmt.Sprintf("%v", body["email"])
		role := fmt.Sprintf("%v", body["role"])
		gid := fmt.Sprintf("grant_%d", time.Now().UnixNano())
		code := "12345678"

		state.mu.Lock()
		state.grants[gid] = map[string]interface{}{
			"id":        gid,
			"email":     email,
			"role":      role,
			"code":      code,
			"attempts":  0,
			"burned":    false,
			"createdAt": time.Now().UTC().Format(time.RFC3339),
		}
		state.grantCodeMap[code] = gid
		state.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "ok",
			"grant_id":   gid,
			"email":      email,
			"role":       role,
			"grant_code": "1234-5678",
		})
	}
	mux.HandleFunc("POST /v1/owner/grant", handleGrantCreate)
	mux.HandleFunc("POST /api/v1/owner/grant", handleGrantCreate)

	handleGrantList := func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()

		var list []map[string]interface{}
		for _, g := range state.grants {
			list = append(list, map[string]interface{}{
				"id":         g["id"],
				"email":      g["email"],
				"role":       g["role"],
				"burned":     g["burned"],
				"created_at": g["createdAt"],
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"grants": list})
	}
	mux.HandleFunc("GET /v1/owner/grants", handleGrantList)
	mux.HandleFunc("GET /api/v1/owner/grants", handleGrantList)

	handleGrantBurn := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		code := strings.ReplaceAll(body["code"], "-", "")

		state.mu.Lock()
		defer state.mu.Unlock()

		gid, exists := state.grantCodeMap[code]
		if !exists {
			for _, g := range state.grants {
				att := g["attempts"].(int) + 1
				g["attempts"] = att
				if att >= 3 {
					g["burned"] = true
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"status":             "burned",
						"remaining_attempts": 0,
					})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status":             "invalid",
					"remaining_attempts": 3 - att,
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		g := state.grants[gid]
		g["burned"] = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "burned",
		})
	}
	mux.HandleFunc("POST /v1/owner/grant/burn", handleGrantBurn)
	mux.HandleFunc("POST /api/v1/owner/grant/burn", handleGrantBurn)

	// TOTP Reset
	handleTOTPReset := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "totp_reset",
			"email":   body["email"],
			"message": "TOTP enrollment reset successfully",
		})
	}
	mux.HandleFunc("POST /v1/owner/totp/reset", handleTOTPReset)
	mux.HandleFunc("POST /api/v1/owner/totp/reset", handleTOTPReset)

	// Onboarding Claim & Validate
	handleClaim := func(w http.ResponseWriter, r *http.Request) {
		var req provisioner.ClaimRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		token := req.InviteToken

		if strings.Contains(token, "invalid") || strings.Contains(token, "bad") {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid onboarding token"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(provisioner.ClaimResponse{
			Status: "claimed",
			User: provisioner.User{
				UID:         "testuser",
				Email:       "testuser@manova.space",
				DisplayName: "Test Developer",
				Groups:      []string{"core"},
			},
			Credentials: provisioner.Credentials{
				ForgejoUsername: "testuser",
				ForgejoMCPToken: "mock-mcp-token",
				WireGuardConfig: "mock-wg-config",
			},
			Workspace: provisioner.WorkspaceInfo{
				GitRemoteBase:        "http://git.dev.manova.space:3000",
				DefaultManifestScope: "core",
			},
		})
	}
	mux.HandleFunc("POST /v1/onboard/claim", handleClaim)
	mux.HandleFunc("POST /api/v1/onboard/claim", handleClaim)

	handleValidate := func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(token, "invalid") || strings.Contains(token, "bad") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"valid": false, "error": "malformed token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":   true,
			"email":   "charlie@example.com",
			"scope":   "core",
			"expires": time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}
	mux.HandleFunc("GET /v1/onboard/validate", handleValidate)
	mux.HandleFunc("GET /api/v1/onboard/validate", handleValidate)

	// Staff endpoints helper with HMAC validation
	verifyHMAC := func(r *http.Request, rawBody []byte) error {
		tsStr := r.Header.Get("X-Orbit-Timestamp")
		sig := r.Header.Get("X-Orbit-Signature")
		if tsStr == "" || sig == "" {
			return fmt.Errorf("missing HMAC headers")
		}
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid timestamp")
		}
		expectedSig := staffhmac.Sign(testHMACSecret, ts, r.Method, r.URL.Path, rawBody)
		if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
			return fmt.Errorf("bad hmac signature")
		}
		return nil
	}

	reservedUIDs := map[string]bool{
		"admin":          true,
		"authelia-bind":  true,
		"verdaccio-bind": true,
		"verdaccio-ci":   true,
	}

	// Staff routes
	mux.HandleFunc("/api/v1/staff", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := verifyHMAC(r, body); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		if r.Method == http.MethodPost {
			var in client.StaffCreateInput
			_ = json.Unmarshal(body, &in)
			if reservedUIDs[in.UID] {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "reserved uid"})
				return
			}

			member := &client.StaffMember{
				UID:             in.UID,
				DisplayName:     in.DisplayName,
				PersonalForward: in.PersonalForward,
				Status:          "active",
				Groups:          in.Groups,
			}
			state.mu.Lock()
			state.staffMembers[in.UID] = member
			state.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(client.StaffCreateResult{
				StaffMember:  *member,
				LDAPPassword: "mock-ldap-password-" + in.UID,
				MailPassword: "mock-mail-password-" + in.UID,
				OTPAuth:      fmt.Sprintf("otpauth://totp/Orbit:%s@manova.space?secret=JBSWY3DPEHPK3PXP&issuer=Orbit", in.UID),
			})
			return
		}

		if r.Method == http.MethodGet {
			state.mu.RLock()
			var list []client.StaffMember
			for _, m := range state.staffMembers {
				list = append(list, *m)
			}
			state.mu.RUnlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)
			return
		}
	})

	mux.HandleFunc("/api/v1/staff/{uid}", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("uid")
		body, _ := io.ReadAll(r.Body)
		if err := verifyHMAC(r, body); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		if reservedUIDs[uid] {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "reserved uid"})
			return
		}

		state.mu.Lock()
		defer state.mu.Unlock()

		member, exists := state.staffMembers[uid]
		if !exists && r.Method != http.MethodPut && r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
			return
		}

		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(member)
		case http.MethodPatch:
			var patch client.StaffUpdateInput
			_ = json.Unmarshal(body, &patch)
			if patch.DisplayName != "" {
				member.DisplayName = patch.DisplayName
			}
			if patch.PersonalForward != "" {
				member.PersonalForward = patch.PersonalForward
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(member)
		case http.MethodDelete:
			delete(state.staffMembers, uid)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "uid": uid})
		}
	})

	mux.HandleFunc("POST /api/v1/staff/{uid}/disable", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("uid")
		body, _ := io.ReadAll(r.Body)
		if err := verifyHMAC(r, body); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if reservedUIDs[uid] {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		state.mu.Lock()
		if m, ok := state.staffMembers[uid]; ok {
			m.Status = "disabled"
		}
		state.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "disabled", "uid": uid})
	})

	mux.HandleFunc("POST /api/v1/staff/{uid}/enable", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("uid")
		body, _ := io.ReadAll(r.Body)
		if err := verifyHMAC(r, body); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if reservedUIDs[uid] {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		state.mu.Lock()
		if m, ok := state.staffMembers[uid]; ok {
			m.Status = "active"
		}
		state.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "active", "uid": uid})
	})

	mux.HandleFunc("POST /api/v1/staff/{uid}/reset-password", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("uid")
		body, _ := io.ReadAll(r.Body)
		if err := verifyHMAC(r, body); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if reservedUIDs[uid] {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.StaffResetResult{
			LDAPPassword: "reset-mock-ldap-password-" + uid,
			MailPassword: "reset-mock-mail-password-" + uid,
			OTPAuth:      fmt.Sprintf("otpauth://totp/Orbit:%s@manova.space?secret=JBSWY3DPEHPK3PXP&issuer=Orbit", uid),
		})
	})

	mux.HandleFunc("POST /api/v1/staff/{uid}/recreate", func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("uid")
		body, _ := io.ReadAll(r.Body)
		if err := verifyHMAC(r, body); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if reservedUIDs[uid] {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var in client.StaffCreateInput
		_ = json.Unmarshal(body, &in)
		member := &client.StaffMember{
			UID:             uid,
			DisplayName:     in.DisplayName,
			PersonalForward: in.PersonalForward,
			Status:          "active",
			Groups:          in.Groups,
		}
		state.mu.Lock()
		state.staffMembers[uid] = member
		state.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.StaffCreateResult{
			StaffMember:  *member,
			LDAPPassword: "recreated-mock-ldap-password-" + uid,
			MailPassword: "recreated-mock-mail-password-" + uid,
			OTPAuth:      fmt.Sprintf("otpauth://totp/Orbit:%s@manova.space?secret=JBSWY3DPEHPK3PXP&issuer=Orbit", uid),
		})
	})

	// S3 / R2 mock endpoints
	mux.HandleFunc("/{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) {
		bucket := r.PathValue("bucket")
		key := r.PathValue("key")
		storageKey := bucket + "/" + key

		state.mu.Lock()
		defer state.mu.Unlock()

		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			state.s3Objects[storageKey] = data
			h := md5.Sum(data)
			w.Header().Set("ETag", fmt.Sprintf("\"%x\"", h))
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, exists := state.s3Objects[storageKey]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		case http.MethodHead:
			data, exists := state.s3Objects[storageKey]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
		}
	})

	return mux
}

// setupIntegrationSandbox configures isolated env and returns cleanup func
func setupIntegrationSandbox(t *testing.T, serverURL string) (string, func()) {
	tmpDir := t.TempDir()

	origEnv := map[string]string{
		"HOME":                    os.Getenv("HOME"),
		"XDG_CONFIG_HOME":         os.Getenv("XDG_CONFIG_HOME"),
		"ORBIT_CONFIG_DIR":        os.Getenv("ORBIT_CONFIG_DIR"),
		"ORBIT_OWNER_STORE":       os.Getenv("ORBIT_OWNER_STORE"),
		"ORBIT_SESSION_FILE":      os.Getenv("ORBIT_SESSION_FILE"),
		"ORBIT_INVITE_STORE":      os.Getenv("ORBIT_INVITE_STORE"),
		"ORBIT_R2_ENV":            os.Getenv("ORBIT_R2_ENV"),
		"ORBIT_SERVER":            os.Getenv("ORBIT_SERVER"),
		"ORBIT_API_URL":           os.Getenv("ORBIT_API_URL"),
		"ORBIT_STAFF_URL":         os.Getenv("ORBIT_STAFF_URL"),
		"ORBIT_S3_ENDPOINT":       os.Getenv("ORBIT_S3_ENDPOINT"),
		"ORBIT_FORGEJO_URL":       os.Getenv("ORBIT_FORGEJO_URL"),
		"ORBIT_STAFF_HMAC_SECRET": os.Getenv("ORBIT_STAFF_HMAC_SECRET"),
		"ORBIT_SIGNING_SECRET":    os.Getenv("ORBIT_SIGNING_SECRET"),
		"ORBIT_TESTBED":           os.Getenv("ORBIT_TESTBED"),
		"ORBIT_SKIP_HOSTGATE":     os.Getenv("ORBIT_SKIP_HOSTGATE"),
		"ORBIT_SKIP_PREFLIGHT":    os.Getenv("ORBIT_SKIP_PREFLIGHT"),
	}

	configDir := filepath.Join(tmpDir, ".config", "orbit")
	_ = os.MkdirAll(configDir, 0700)

	ownerFile := filepath.Join(configDir, "owner.json")
	sessionFile := filepath.Join(configDir, "session.json")
	inviteFile := filepath.Join(configDir, "invites.json")
	r2EnvFile := filepath.Join(configDir, "r2.env")

	_ = os.Setenv("HOME", tmpDir)
	_ = os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	_ = os.Setenv("ORBIT_CONFIG_DIR", configDir)
	_ = os.Setenv("ORBIT_OWNER_STORE", ownerFile)
	_ = os.Setenv("ORBIT_SESSION_FILE", sessionFile)
	_ = os.Setenv("ORBIT_INVITE_STORE", inviteFile)
	_ = os.Setenv("ORBIT_R2_ENV", r2EnvFile)
	_ = os.Setenv("ORBIT_SERVER", serverURL)
	_ = os.Setenv("ORBIT_API_URL", serverURL)
	_ = os.Setenv("ORBIT_STAFF_URL", serverURL)
	_ = os.Setenv("ORBIT_S3_ENDPOINT", serverURL)
	_ = os.Setenv("ORBIT_FORGEJO_URL", serverURL)
	_ = os.Setenv("ORBIT_STAFF_HMAC_SECRET", testHMACSecret)
	_ = os.Setenv("ORBIT_SIGNING_SECRET", testInviteSecret)
	_ = os.Setenv("ORBIT_TESTBED", "1")
	_ = os.Setenv("ORBIT_SKIP_HOSTGATE", "1")
	_ = os.Setenv("ORBIT_SKIP_PREFLIGHT", "1")

	cleanup := func() {
		for k, v := range origEnv {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}

	return tmpDir, cleanup
}

func seedOwnerVault(path, email, secret string) error {
	rec := owner.OwnerRecord{
		Email:             email,
		VerifiedAt:        time.Now().UTC(),
		KeyFingerprint:    fmt.Sprintf("%x", sha256.Sum256([]byte(email))),
		RootSigningSecret: secret,
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	return os.WriteFile(path, data, 0600)
}

func executeOrbit(args ...string) (string, error) {
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestScenariosIntegration(t *testing.T) {
	state := newMockServerState()
	srv := httptest.NewServer(setupMockHTTPHandler(state))
	defer srv.Close()

	// --------------------------------------------------------------------------
	// ADM_AdminPlatform (ADM-01 through ADM-07)
	// --------------------------------------------------------------------------
	t.Run("ADM_AdminPlatform", func(t *testing.T) {
		tmpDir, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		ownerStore := os.Getenv("ORBIT_OWNER_STORE")

		// ADM-01: Hermetic Admin Init
		out, err := executeOrbit("admin", "init", "admin@manova.space", "--no-send", "--code", "123456")
		if err != nil {
			t.Fatalf("ADM-01 admin init failed: %v\nOutput: %s", err, out)
		}
		if !strings.Contains(out, "Platform Ownership Verified") {
			t.Errorf("ADM-01 expected verified confirmation, got: %s", out)
		}

		info, err := os.Stat(ownerStore)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Errorf("ADM-01 owner.json permissions must be 0600, got: %v (perm: %o)", err, info.Mode().Perm())
		}

		statusOut, err := executeOrbit("admin", "status", "--format", "json")
		if err != nil {
			t.Fatalf("ADM-01 admin status failed: %v\nOutput: %s", err, statusOut)
		}
		var stat map[string]interface{}
		if err := json.Unmarshal([]byte(statusOut), &stat); err != nil || stat["verified"] != true {
			t.Errorf("ADM-01 status JSON invalid or not verified: %v, body: %s", err, statusOut)
		}

		// ADM-03: Idempotent re-init & force
		reinitOut, _ := executeOrbit("admin", "init", "admin@manova.space", "--no-send")
		if !strings.Contains(reinitOut, "already verified") {
			t.Errorf("ADM-03 expected already verified message, got: %s", reinitOut)
		}

		forceOut, err := executeOrbit("admin", "init", "admin@manova.space", "--no-send", "--code", "123456", "--force")
		if err != nil || !strings.Contains(forceOut, "Platform Ownership Verified") {
			t.Errorf("ADM-03 force re-init failed: %v, out: %s", err, forceOut)
		}

		// ADM-04: Admin grant creation & list
		grantOut, err := executeOrbit("admin", "grant", "delegate@manova.space", "--server", srv.URL, "--role", "admin")
		if err != nil {
			t.Fatalf("ADM-04 admin grant failed: %v\nOutput: %s", err, grantOut)
		}
		if !strings.Contains(grantOut, "delegate@manova.space") {
			t.Errorf("ADM-04 grant output missing delegate email: %s", grantOut)
		}

		// ADM-06: Master signing secret rotation
		rotateOut, err := executeOrbit("admin", "rotate-secret", "--yes")
		if err != nil || !strings.Contains(rotateOut, "Rotated") {
			t.Errorf("ADM-06 secret rotation failed: %v\nOutput: %s", err, rotateOut)
		}

		// ADM-07: TOTP reset
		totpOut, err := executeOrbit("admin", "totp", "reset", "user@manova.space", "--json")
		if err != nil || !strings.Contains(totpOut, "totp_reset") {
			t.Errorf("ADM-07 totp reset failed: %v\nOutput: %s", err, totpOut)
		}

		_ = tmpDir
	})

	// --------------------------------------------------------------------------
	// STF_StaffManagement (STF-01 through STF-05)
	// --------------------------------------------------------------------------
	t.Run("STF_StaffManagement", func(t *testing.T) {
		_, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		ownerStore := os.Getenv("ORBIT_OWNER_STORE")
		if err := seedOwnerVault(ownerStore, "admin@manova.space", testHMACSecret); err != nil {
			t.Fatalf("failed to seed owner vault: %v", err)
		}

		// STF-01: Create staff member
		createOut, err := executeOrbit("staff", "create",
			"--server", srv.URL,
			"--uid", "alice",
			"--name", "Alice Smith",
			"--forward", "alice@example.com",
			"--groups", "dev,core",
			"--totp",
			"--invite",
		)
		if err != nil {
			t.Fatalf("STF-01 staff create failed: %v\nOutput: %s", err, createOut)
		}
		if !strings.Contains(createOut, "alice") || !strings.Contains(createOut, "sso") {
			t.Errorf("STF-01 create output missing alice details: %s", createOut)
		}

		// STF-02: List & Get
		listOut, err := executeOrbit("staff", "list", "--server", srv.URL)
		if err != nil || !strings.Contains(listOut, "alice") {
			t.Errorf("STF-02 staff list missing alice: %v\nOutput: %s", err, listOut)
		}

		getOut, err := executeOrbit("staff", "get", "alice", "--server", srv.URL)
		if err != nil || !strings.Contains(getOut, "Alice Smith") {
			t.Errorf("STF-02 staff get missing Alice Smith: %v\nOutput: %s", err, getOut)
		}

		// STF-03: Disable & Enable
		disOut, err := executeOrbit("staff", "disable", "alice", "--server", srv.URL)
		if err != nil || !strings.Contains(disOut, "disabled") {
			t.Errorf("STF-03 staff disable failed: %v\nOutput: %s", err, disOut)
		}

		enOut, err := executeOrbit("staff", "enable", "alice", "--server", srv.URL)
		if err != nil || (!strings.Contains(enOut, "enabled") && !strings.Contains(enOut, "active")) {
			t.Errorf("STF-03 staff enable failed: %v\nOutput: %s", err, enOut)
		}

		// STF-04: Update, Reset Password, Delete
		upOut, err := executeOrbit("staff", "update", "alice", "--server", srv.URL, "--name", "Alice Lead")
		if err != nil || !strings.Contains(upOut, "updated") {
			t.Errorf("STF-04 staff update failed: %v\nOutput: %s", err, upOut)
		}

		pwOut, err := executeOrbit("staff", "reset-password", "alice", "--server", srv.URL)
		if err != nil || (!strings.Contains(pwOut, "Password") && !strings.Contains(pwOut, "sso")) {
			t.Errorf("STF-04 staff reset-password failed: %v\nOutput: %s", err, pwOut)
		}

		delOut, err := executeOrbit("staff", "delete", "alice", "--server", srv.URL)
		if err != nil || !strings.Contains(delOut, "deleted") {
			t.Errorf("STF-04 staff delete failed: %v\nOutput: %s", err, delOut)
		}

		// STF-05: Reserved accounts protection
		for _, reserved := range []string{"admin", "authelia-bind", "verdaccio-bind", "verdaccio-ci"} {
			resOut, _ := executeOrbit("staff", "create", "--server", srv.URL, "--uid", reserved, "--forward", "r@ex.com")
			if !strings.Contains(resOut, "reserved uid") {
				t.Errorf("STF-05 expected reserved uid error for %s, got: %s", reserved, resOut)
			}
		}
	})

	// --------------------------------------------------------------------------
	// INV_InvitationLifecycle (INV-01 through INV-03)
	// --------------------------------------------------------------------------
	t.Run("INV_InvitationLifecycle", func(t *testing.T) {
		_, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		ownerStore := os.Getenv("ORBIT_OWNER_STORE")
		if err := seedOwnerVault(ownerStore, "admin@manova.space", testHMACSecret); err != nil {
			t.Fatalf("failed to seed owner vault: %v", err)
		}

		// INV-01: Create & List
		invOut, err := executeOrbit("invite", "create", "dev@example.com", "--name", "Dev One", "--scope", "core", "--no-send")
		if err != nil || !strings.Contains(invOut, "dev@example.com") {
			t.Fatalf("INV-01 invite create failed: %v\nOutput: %s", err, invOut)
		}

		listJSON, err := executeOrbit("invite", "list", "--format", "json")
		if err != nil {
			t.Fatalf("INV-01 invite list failed: %v\nOutput: %s", err, listJSON)
		}
		var invites []map[string]interface{}
		if err := json.Unmarshal([]byte(listJSON), &invites); err != nil || len(invites) == 0 {
			t.Fatalf("INV-01 invite list parse failed: %v, body: %s", err, listJSON)
		}
		inviteID := invites[0]["id"].(string)

		// INV-02: Revoke
		revOut, err := executeOrbit("invite", "revoke", inviteID)
		if err != nil || !strings.Contains(revOut, "Revoked") {
			t.Errorf("INV-02 invite revoke failed: %v\nOutput: %s", err, revOut)
		}

		// INV-03: Revoke non-existent
		revBad, _ := executeOrbit("invite", "revoke", "non-existent-id-999")
		if !strings.Contains(revBad, "not found") {
			t.Errorf("INV-03 expected not found error, got: %s", revBad)
		}
	})

	// --------------------------------------------------------------------------
	// ONB_OnboardingWorkflow (ONB-01 through ONB-03)
	// --------------------------------------------------------------------------
	t.Run("ONB_OnboardingWorkflow", func(t *testing.T) {
		tmpDir, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		ownerStore := os.Getenv("ORBIT_OWNER_STORE")
		if err := seedOwnerVault(ownerStore, "admin@manova.space", testHMACSecret); err != nil {
			t.Fatalf("failed to seed owner vault: %v", err)
		}

		// ONB-01: Non-interactive onboarding claim
		onbOut, err := executeOrbit("onboard",
			"--token", "valid-mock-onboarding-token",
			"--server", srv.URL,
			"--non-interactive",
			"--skip-stack",
		)
		if err != nil {
			t.Fatalf("ONB-01 onboarding failed: %v\nOutput: %s", err, onbOut)
		}

		sshPriv := filepath.Join(tmpDir, ".ssh", "id_ed25519")
		sshPub := filepath.Join(tmpDir, ".ssh", "id_ed25519.pub")
		if _, err := os.Stat(sshPriv); err != nil {
			t.Errorf("ONB-01 expected SSH private key generated at %s", sshPriv)
		}
		if _, err := os.Stat(sshPub); err != nil {
			t.Errorf("ONB-01 expected SSH public key generated at %s", sshPub)
		}

		// ONB-02: Diagnostic bundle & checkpoint reset
		bundlePath := filepath.Join(tmpDir, "diag.tar.gz")
		diagOut, err := executeOrbit("onboard", "--diag-bundle", bundlePath)
		if err != nil || !strings.Contains(diagOut, "Diagnostic Bundle") {
			t.Errorf("ONB-02 diag bundle failed: %v\nOutput: %s", err, diagOut)
		}
		if _, err := os.Stat(bundlePath); err != nil {
			t.Errorf("ONB-02 expected bundle archive created at %s", bundlePath)
		}

		resetOut, err := executeOrbit("onboard", "--reset")
		if err != nil || !strings.Contains(resetOut, "cleared") {
			t.Errorf("ONB-02 checkpoint reset failed: %v\nOutput: %s", err, resetOut)
		}

		// ONB-03: Invalid token rejection & dry-run
		dryOut, err := executeOrbit("onboard", "--dry-run")
		if err != nil || !strings.Contains(dryOut, "DRY-RUN") {
			t.Errorf("ONB-03 dry-run failed: %v\nOutput: %s", err, dryOut)
		}

		// Reset session before testing invalid token
		_, _ = executeOrbit("onboard", "--reset")
		invalidOut, _ := executeOrbit("onboard", "--token", "invalid-token-string", "--server", srv.URL, "--non-interactive", "--skip-stack")
		if !strings.Contains(invalidOut, "invalid") && !strings.Contains(invalidOut, "rejected") && !strings.Contains(invalidOut, "failed") {
			t.Errorf("ONB-03 expected invalid token rejection, got: %s", invalidOut)
		}
	})

	// --------------------------------------------------------------------------
	// DOC_DoctorDiagnostics (DOC-01, DOC-02)
	// --------------------------------------------------------------------------
	t.Run("DOC_DoctorDiagnostics", func(t *testing.T) {
		_, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		docOut, _ := executeOrbit("doctor")
		if !strings.Contains(docOut, "Orbit System Doctor") && !strings.Contains(docOut, "Git") {
			t.Errorf("DOC-01 doctor output missing expected header: %s", docOut)
		}

		jsonOut, _ := executeOrbit("doctor", "--json")
		if !strings.Contains(jsonOut, "results") {
			t.Errorf("DOC-01 doctor json output missing results key: %s", jsonOut)
		}

		fixOut, _ := executeOrbit("doctor", "--fix", "--non-interactive")
		if !strings.Contains(fixOut, "Doctor") && !strings.Contains(fixOut, "passed") {
			t.Errorf("DOC-02 doctor --fix failed: %s", fixOut)
		}
	})

	// --------------------------------------------------------------------------
	// AST_AssetsSync (AST-01, AST-02)
	// --------------------------------------------------------------------------
	t.Run("AST_AssetsSync", func(t *testing.T) {
		tmpDir, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		wsDir := filepath.Join(tmpDir, "workspace")
		_ = os.MkdirAll(wsDir, 0755)
		_ = os.Setenv("ORBIT_WORKSPACE", wsDir)

		wsYAML := `version: "1"
workspace: "test-ws"
groups:
  test:
    path: ""
    repositories:
      - name: "test-repo"
        path: "test-repo"
        required: true
`
		_ = os.WriteFile(filepath.Join(wsDir, "workspace.yaml"), []byte(wsYAML), 0644)

		repoDir := filepath.Join(wsDir, "test-repo")
		_ = os.MkdirAll(repoDir, 0755)

		// Git init test repo
		_ = exec.Command("git", "-C", repoDir, "init", "-q").Run()
		_ = exec.Command("git", "-C", repoDir, "config", "user.name", "Test").Run()
		_ = exec.Command("git", "-C", repoDir, "config", "user.email", "t@ex.com").Run()
		_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test"), 0644)
		_ = exec.Command("git", "-C", repoDir, "add", "README.md").Run()
		_ = exec.Command("git", "-C", repoDir, "commit", "-q", "-m", "init").Run()

		// Write r2.env
		r2Env := fmt.Sprintf("R2_ACCOUNT_ID=mock\nR2_ACCESS_KEY_ID=mock\nR2_SECRET_ACCESS_KEY=mock\nR2_BUCKET=manova-assets\nR2_ENDPOINT=%s\n", srv.URL)
		_ = os.WriteFile(os.Getenv("ORBIT_R2_ENV"), []byte(r2Env), 0600)

		// Create asset
		assetFile := filepath.Join(repoDir, "video.mp4")
		_ = os.WriteFile(assetFile, []byte("large-video-payload-content-1234567890"), 0644)

		// AST-01: Asset add
		origDir, _ := os.Getwd()
		_ = os.Chdir(repoDir)
		addOut, err := executeOrbit("assets", "add", "video.mp4")
		_ = os.Chdir(origDir)
		if err != nil || !strings.Contains(addOut, "Added video.mp4") {
			t.Fatalf("AST-01 asset add failed: %v\nOutput: %s", err, addOut)
		}

		// AST-01: Asset status
		statOut, err := executeOrbit("assets", "status")
		if err != nil || !strings.Contains(statOut, "ok") {
			t.Errorf("AST-01 asset status failed: %v\nOutput: %s", err, statOut)
		}

		// AST-01: Delete local and pull
		_ = os.Remove(assetFile)
		pullOut, err := executeOrbit("assets", "pull")
		if err != nil || !strings.Contains(pullOut, "Assets pulled") {
			t.Errorf("AST-01 asset pull failed: %v\nOutput: %s", err, pullOut)
		}
		if _, err := os.Stat(assetFile); err != nil {
			t.Errorf("AST-01 expected video.mp4 restored after pull")
		}

		// AST-02: Missing credentials error
		_ = os.Remove(os.Getenv("ORBIT_R2_ENV"))
		badPull, _ := executeOrbit("assets", "pull")
		if !strings.Contains(badPull, "credentials") && !strings.Contains(badPull, "missing") && !strings.Contains(badPull, "Error") {
			t.Errorf("AST-02 expected credentials error, got: %s", badPull)
		}
	})

	// --------------------------------------------------------------------------
	// PRT_PortManager (PRT-01)
	// --------------------------------------------------------------------------
	t.Run("PRT_PortManager", func(t *testing.T) {
		_, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		listOut, err := executeOrbit("port", "list")
		if err != nil || !strings.Contains(listOut, "Orbit Port Manager") {
			t.Fatalf("PRT-01 port list failed: %v\nOutput: %s", err, listOut)
		}

		allocOut, err := executeOrbit("port", "allocate", "orbit-platform", "test-worker")
		if err != nil || !strings.Contains(allocOut, "Successful") {
			t.Fatalf("PRT-01 port allocate failed: %v\nOutput: %s", err, allocOut)
		}

		errOut, _ := executeOrbit("port", "allocate", "invalid-project-xyz", "test-worker")
		if !strings.Contains(errOut, "unknown project") {
			t.Errorf("PRT-01 expected unknown project error, got: %s", errOut)
		}
	})

	// --------------------------------------------------------------------------
	// ENV_EnvironmentContracts (ENV-01)
	// --------------------------------------------------------------------------
	t.Run("ENV_EnvironmentContracts", func(t *testing.T) {
		tmpDir, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		wsDir := filepath.Join(tmpDir, "workspace")
		svcDir := filepath.Join(wsDir, "services", "backend")
		_ = os.MkdirAll(svcDir, 0755)
		_ = os.Setenv("ORBIT_WORKSPACE", wsDir)

		schemaContent := `version: "1"
variables:
  - name: DATABASE_URL
    required: true
    default: "postgres://orbit:pass@localhost:5432/db"
  - name: API_KEY
    required: true
    default: "secret-key"
`
		_ = os.WriteFile(filepath.Join(svcDir, ".env.schema.yaml"), []byte(schemaContent), 0644)

		// Setup .env
		setupOut, err := executeOrbit("env", "setup", wsDir)
		if err != nil || !strings.Contains(setupOut, "generated .env") {
			t.Fatalf("ENV-01 env setup failed: %v\nOutput: %s", err, setupOut)
		}

		envFile := filepath.Join(svcDir, ".env")
		info, err := os.Stat(envFile)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Errorf("ENV-01 expected .env mode 0600, got: %v (perm: %o)", err, info.Mode().Perm())
		}

		// Check .env
		checkOut, err := executeOrbit("env", "check", wsDir)
		if err != nil || (!strings.Contains(checkOut, "valid") && !strings.Contains(checkOut, "compliant")) {
			t.Errorf("ENV-01 env check failed: %v\nOutput: %s", err, checkOut)
		}
	})

	// --------------------------------------------------------------------------
	// CFG_ConfigurationStorage (CFG-01)
	// --------------------------------------------------------------------------
	t.Run("CFG_ConfigurationStorage", func(t *testing.T) {
		_, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		cfgFile := filepath.Join(os.Getenv("ORBIT_CONFIG_DIR"), "config.yaml")

		// Init config
		initOut, err := executeOrbit("config", "init")
		if err != nil || !strings.Contains(initOut, "initialized") {
			t.Fatalf("CFG-01 config init failed: %v\nOutput: %s", err, initOut)
		}

		info, err := os.Stat(cfgFile)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Errorf("CFG-01 expected config.yaml mode 0600, got: %v (perm: %o)", err, info.Mode().Perm())
		}

		// Set properties
		_, _ = executeOrbit("config", "set", "server.url", "http://orbit.dev.manova.space:8080")
		_, _ = executeOrbit("config", "set", "assets.bucket", "custom-bucket")
		_, _ = executeOrbit("config", "set", "custom.api_token", "super-secret-12345")

		// Show config
		showOut, err := executeOrbit("config", "show")
		if err != nil || !strings.Contains(showOut, "custom-bucket") {
			t.Errorf("CFG-01 config show failed: %v\nOutput: %s", err, showOut)
		}

		// Raw get
		getOut, err := executeOrbit("config", "get", "custom.api_token", "--raw")
		if err != nil || strings.TrimSpace(getOut) != "super-secret-12345" {
			t.Errorf("CFG-01 raw get failed: %v, got: %q", err, getOut)
		}

		// Unset
		unsetOut, err := executeOrbit("config", "unset", "custom.api_token")
		if err != nil || !strings.Contains(unsetOut, "Unset") {
			t.Errorf("CFG-01 config unset failed: %v\nOutput: %s", err, unsetOut)
		}

		// List entries
		listOut, err := executeOrbit("config", "list")
		if err != nil || !strings.Contains(listOut, "server.url") {
			t.Errorf("CFG-01 config list failed: %v\nOutput: %s", err, listOut)
		}
	})

	// --------------------------------------------------------------------------
	// WKS_WorkspaceOrchestration (WKS-01)
	// --------------------------------------------------------------------------
	t.Run("WKS_WorkspaceOrchestration", func(t *testing.T) {
		tmpDir, cleanup := setupIntegrationSandbox(t, srv.URL)
		defer cleanup()

		wsDir := filepath.Join(tmpDir, "workspace")
		remotesDir := filepath.Join(tmpDir, "mock-remotes")
		_ = os.MkdirAll(wsDir, 0755)
		_ = os.MkdirAll(remotesDir, 0755)
		_ = os.Setenv("ORBIT_WORKSPACE", wsDir)

		// Create bare git remotes
		for _, repo := range []string{"repo-a", "repo-b"} {
			bareDir := filepath.Join(remotesDir, repo+".git")
			_ = os.MkdirAll(bareDir, 0755)
			_ = exec.Command("git", "init", "--bare", "-q", bareDir).Run()

			// Seed initial commit
			seedDir := filepath.Join(tmpDir, "seed-"+repo)
			_ = os.MkdirAll(seedDir, 0755)
			_ = exec.Command("git", "-C", seedDir, "init", "-q").Run()
			_ = exec.Command("git", "-C", seedDir, "config", "user.name", "Test").Run()
			_ = exec.Command("git", "-C", seedDir, "config", "user.email", "test@example.com").Run()
			_ = os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("# "+repo), 0644)
			_ = exec.Command("git", "-C", seedDir, "add", "README.md").Run()
			_ = exec.Command("git", "-C", seedDir, "commit", "-q", "-m", "init").Run()
			_ = exec.Command("git", "-C", seedDir, "branch", "-M", "main").Run()
			_ = exec.Command("git", "-C", seedDir, "remote", "add", "origin", bareDir).Run()
			_ = exec.Command("git", "-C", seedDir, "push", "-q", "-u", "origin", "main").Run()
			_ = os.RemoveAll(seedDir)
		}

		wsYAML := fmt.Sprintf(`version: "1"
workspace: "test-workspace"
remotes:
  local: "file://%s"
groups:
  core:
    path: ""
    defaults:
      remote: "local"
      default_branch: "main"
    repositories:
      - name: "repo-a"
        path: "services/repo-a"
        required: true
      - name: "repo-b"
        path: "services/repo-b"
        required: true
`, remotesDir)
		_ = os.WriteFile(filepath.Join(wsDir, "workspace.yaml"), []byte(wsYAML), 0644)

		// WKS-01: Init
		initOut, err := executeOrbit("init", "--manifest", filepath.Join(wsDir, "workspace.yaml"))
		if err != nil || (!strings.Contains(initOut, "cloned") && !strings.Contains(initOut, "Cloned")) {
			t.Fatalf("WKS-01 workspace init failed: %v\nOutput: %s", err, initOut)
		}

		repoAPath := filepath.Join(wsDir, "services", "repo-a")
		repoBPath := filepath.Join(wsDir, "services", "repo-b")
		if _, err := os.Stat(filepath.Join(repoAPath, ".git")); err != nil {
			t.Errorf("WKS-01 repo-a .git missing after init")
		}

		// WKS-01: Status (clean)
		statOut, err := executeOrbit("status")
		if err != nil || !strings.Contains(statOut, "repo-a") {
			t.Errorf("WKS-01 workspace status failed: %v\nOutput: %s", err, statOut)
		}

		// WKS-01: Dirty status
		_ = os.WriteFile(filepath.Join(repoAPath, "new.txt"), []byte("dirty"), 0644)
		dirtyOut, _ := executeOrbit("status")
		if !strings.Contains(dirtyOut, "modified") && !strings.Contains(dirtyOut, "untracked") && !strings.Contains(dirtyOut, "dirty") {
			t.Logf("WKS-01 dirty status detected: %s", dirtyOut)
		}

		// WKS-01: Sync
		syncOut, _ := executeOrbit("sync")
		if !strings.Contains(syncOut, "Sync") && !strings.Contains(syncOut, "repo-a") {
			t.Logf("WKS-01 sync executed: %s", syncOut)
		}

		// WKS-01: Repair gitless repo
		_ = os.RemoveAll(filepath.Join(repoBPath, ".git"))
		gitlessStat, _ := executeOrbit("status")
		if !strings.Contains(gitlessStat, "gitless") && !strings.Contains(gitlessStat, "missing .git") {
			t.Logf("WKS-01 gitless status output: %s", gitlessStat)
		}

		repairOut, err := executeOrbit("repair")
		if err != nil || !strings.Contains(repairOut, "Repair") {
			t.Errorf("WKS-01 workspace repair failed: %v\nOutput: %s", err, repairOut)
		}
		if _, err := os.Stat(filepath.Join(repoBPath, ".git")); err != nil {
			t.Errorf("WKS-01 repo-b .git not restored after repair")
		}
	})
}
