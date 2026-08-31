package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/manovaspace/orbit-cli/pkg/staffhmac"
)

const (
	DefaultEdgePort     = ":8080"
	DefaultStaffPort    = ":10800"
	DefaultS3Port       = ":9000"
	DefaultForgejoPort  = ":3000"
	DefaultHMACSecret   = "orbit-dev-insecure-staff-hmac-secret-32bytes"
	DefaultInviteSecret = "orbit-dev-insecure-invitation-signing-secret-key-32bytes"
)

// Config holds runtime configuration for the MockServer.
type Config struct {
	EdgeAddr     string
	StaffAddr    string
	S3Addr       string
	ForgejoAddr  string
	UnifiedAddr  string
	HMACSecret   string
	InviteSecret string
	Verbose      bool
}

// ChallengeState tracks in-memory OTP verification attempts.
type ChallengeState struct {
	ID          string
	Email       string
	Code        string
	Attempts    int
	MaxAttempts int
	Burned      bool
	Verified    bool
	ExpiresAt   time.Time
}

// GrantState tracks in-memory admin grant codes with 3-strike burn logic.
type GrantState struct {
	ID          string
	Email       string
	Code        string
	Role        string
	Attempts    int
	MaxAttempts int
	Burned      bool
	Used        bool
	ExpiresAt   time.Time
}

// ForgejoKey represents an SSH key registered in the Forgejo mock.
type ForgejoKey struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Key         string    `json:"key"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

// S3Object represents an object stored in the S3 / R2 mock.
type S3Object struct {
	Bucket      string
	Key         string
	Data        []byte
	ContentType string
	ETag        string
	Metadata    map[string]string
	UpdatedAt   time.Time
}

// MockServer provides simulated edge, staff, S3, and Forgejo endpoints.
type MockServer struct {
	cfg Config
	mu  sync.RWMutex

	challenges   map[string]*ChallengeState
	grants       map[string]*GrantState
	verified     map[string]bool
	staffMembers map[string]*client.StaffMember
	sshKeys      map[int]*ForgejoKey
	nextSSHKeyID int
	s3Objects    map[string]*S3Object

	listeners []*net.Listener
	servers   []*http.Server
}

// NewMockServer creates and initializes a new MockServer instance.
func NewMockServer(cfg Config) *MockServer {
	if cfg.EdgeAddr == "" {
		cfg.EdgeAddr = DefaultEdgePort
	}
	if cfg.StaffAddr == "" {
		cfg.StaffAddr = DefaultStaffPort
	}
	if cfg.S3Addr == "" {
		cfg.S3Addr = DefaultS3Port
	}
	if cfg.ForgejoAddr == "" {
		cfg.ForgejoAddr = DefaultForgejoPort
	}
	if cfg.HMACSecret == "" {
		cfg.HMACSecret = DefaultHMACSecret
	}
	if cfg.InviteSecret == "" {
		cfg.InviteSecret = DefaultInviteSecret
	}

	return &MockServer{
		cfg:          cfg,
		challenges:   make(map[string]*ChallengeState),
		grants:       make(map[string]*GrantState),
		verified:     make(map[string]bool),
		staffMembers: make(map[string]*client.StaffMember),
		sshKeys:      make(map[int]*ForgejoKey),
		nextSSHKeyID: 1,
		s3Objects:    make(map[string]*S3Object),
	}
}

// isReservedUID checks if a UID is in the reserved system accounts list.
func isReservedUID(uid string) bool {
	clean := strings.ToLower(strings.TrimSpace(uid))
	switch clean {
	case "admin", "authelia-bind", "verdaccio-bind", "verdaccio-ci":
		return true
	default:
		return false
	}
}

// writeJSON writes a JSON response with status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"error": msg,
		"code":  status,
	})
}

// ============================================================================
// Edge Handlers (Owner Challenge, Verify, Grants, Onboard Claim, Validate)
// ============================================================================

func (s *MockServer) EdgeHandler() http.Handler {
	mux := http.NewServeMux()

	// Health check endpoints
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /v1/onboard/health", s.handleHealth)
	mux.HandleFunc("GET /", s.handleRoot)

	// Owner challenge & verify endpoints
	mux.HandleFunc("POST /v1/owner/challenge", s.handleOwnerChallenge)
	mux.HandleFunc("POST /api/v1/admin/challenge", s.handleOwnerChallenge)
	mux.HandleFunc("POST /api/v1/system/ownership/challenge", s.handleOwnerChallenge)

	mux.HandleFunc("POST /v1/owner/verify", s.handleOwnerVerify)
	mux.HandleFunc("POST /api/v1/admin/verify", s.handleOwnerVerify)
	mux.HandleFunc("POST /api/v1/system/ownership/verify", s.handleOwnerVerify)

	// Admin grants endpoints
	mux.HandleFunc("POST /api/v1/admin/grants", s.handleAdminGrantCreate)
	mux.HandleFunc("GET /api/v1/admin/grants", s.handleAdminGrantList)

	// Onboarding claim & validation endpoints
	mux.HandleFunc("POST /v1/onboard/claim", s.handleOnboardClaim)
	mux.HandleFunc("POST /api/v1/onboard/claim", s.handleOnboardClaim)
	mux.HandleFunc("POST /api/v1/dev/onboard/claim", s.handleOnboardClaim)

	mux.HandleFunc("GET /v1/onboard/validate", s.handleOnboardValidate)
	mux.HandleFunc("GET /api/v1/onboard/validate", s.handleOnboardValidate)

	return mux
}

func (s *MockServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"provisioner": "mock-healthy",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *MockServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("#!/usr/bin/env bash\necho 'Orbit Mock Server Active'\n"))
}

func (s *MockServer) handleOwnerChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Email) == "" {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}

	normEmail := strings.ToLower(strings.TrimSpace(req.Email))

	s.mu.Lock()
	defer s.mu.Unlock()

	chID := "ch-" + hex.EncodeToString([]byte(fmt.Sprintf("%d-%s", time.Now().UnixNano(), normEmail)))
	if len(chID) > 32 {
		chID = chID[:32]
	}

	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	s.challenges[normEmail] = &ChallengeState{
		ID:          chID,
		Email:       normEmail,
		Code:        "123456", // Default OTP for deterministic mock tests
		Attempts:    0,
		MaxAttempts: 3,
		Burned:      false,
		Verified:    false,
		ExpiresAt:   expiresAt,
	}

	writeJSON(w, http.StatusOK, client.ChallengeResponse{
		Status:    "pending",
		Email:     normEmail,
		ExpiresAt: expiresAt,
		Message:   "OTP challenge created (mock code: 123456)",
	})
}

func (s *MockServer) handleAdminGrantCreate(w http.ResponseWriter, r *http.Request) {
	var req client.CreateGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Email) == "" {
		writeError(w, http.StatusBadRequest, "invalid grant payload")
		return
	}

	normEmail := strings.ToLower(strings.TrimSpace(req.Email))
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "admin"
	}
	code := owner.CleanCode(req.Code)
	if code == "" {
		code = "12345678"
	}
	ttlSec := req.TTLSeconds
	if ttlSec <= 0 {
		ttlSec = 900
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	gID := "grant-" + hex.EncodeToString([]byte(fmt.Sprintf("%d-%s", time.Now().UnixNano(), normEmail)))
	if len(gID) > 24 {
		gID = gID[:24]
	}

	expiresAt := time.Now().UTC().Add(time.Duration(ttlSec) * time.Second)
	s.grants[normEmail] = &GrantState{
		ID:          gID,
		Email:       normEmail,
		Code:        code,
		Role:        role,
		Attempts:    0,
		MaxAttempts: 3,
		Burned:      false,
		Used:        false,
		ExpiresAt:   expiresAt,
	}

	writeJSON(w, http.StatusCreated, client.CreateGrantResponse{
		Status:    "created",
		ID:        gID,
		Email:     normEmail,
		Role:      role,
		Code:      owner.Format8DigitCode(code),
		ExpiresAt: expiresAt,
	})
}

func (s *MockServer) handleAdminGrantList(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []client.CreateGrantResponse
	now := time.Now().UTC()
	for _, g := range s.grants {
		if !g.Burned && !g.Used && now.Before(g.ExpiresAt) {
			list = append(list, client.CreateGrantResponse{
				Status:    "active",
				ID:        g.ID,
				Email:     g.Email,
				Role:      g.Role,
				Code:      owner.Format8DigitCode(g.Code),
				ExpiresAt: g.ExpiresAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *MockServer) handleOwnerVerify(w http.ResponseWriter, r *http.Request) {
	var req client.VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	normEmail := strings.ToLower(strings.TrimSpace(req.Email))
	cleanCode := owner.CleanCode(req.Code)

	if normEmail == "" || cleanCode == "" {
		writeError(w, http.StatusBadRequest, "email and code are required")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()

	// 1. Check Grant
	if grant, ok := s.grants[normEmail]; ok {
		if grant.Burned || grant.Used || now.After(grant.ExpiresAt) {
			writeError(w, http.StatusBadRequest, "admin grant code has expired, already used, or burned")
			return
		}

		if cleanCode == grant.Code {
			grant.Used = true
			s.verified[normEmail] = true
			writeJSON(w, http.StatusOK, client.VerifyResponse{
				Status:         "verified",
				Email:          normEmail,
				Role:           grant.Role,
				KeyFingerprint: "SHA256:mock-verified-fingerprint-" + normEmail,
				VerifiedAt:     now,
				Message:        "ownership grant verified successfully",
			})
			return
		}

		grant.Attempts++
		if grant.Attempts >= grant.MaxAttempts {
			grant.Burned = true
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":              "maximum grant verification attempts exceeded: grant burned",
				"code":               http.StatusBadRequest,
				"remaining_attempts": 0,
			})
			return
		}

		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":              "invalid admin grant code",
			"code":               http.StatusBadRequest,
			"remaining_attempts": grant.MaxAttempts - grant.Attempts,
		})
		return
	}

	// 2. Check Challenge
	if ch, ok := s.challenges[normEmail]; ok {
		if ch.Burned || ch.Verified || now.After(ch.ExpiresAt) {
			writeError(w, http.StatusBadRequest, "challenge has expired, already used, or burned")
			return
		}

		if cleanCode == ch.Code {
			ch.Verified = true
			s.verified[normEmail] = true
			writeJSON(w, http.StatusOK, client.VerifyResponse{
				Status:         "verified",
				Email:          normEmail,
				Role:           "admin",
				KeyFingerprint: "SHA256:mock-verified-fingerprint-" + normEmail,
				VerifiedAt:     now,
				Message:        "OTP verified successfully",
			})
			return
		}

		ch.Attempts++
		if ch.Attempts >= ch.MaxAttempts {
			ch.Burned = true
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":              "maximum verification attempts exceeded: challenge burned",
				"code":               http.StatusBadRequest,
				"remaining_attempts": 0,
			})
			return
		}

		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":              "invalid verification code",
			"code":               http.StatusBadRequest,
			"remaining_attempts": ch.MaxAttempts - ch.Attempts,
		})
		return
	}

	// Default test fallback: if 123456 passed without prior challenge
	if cleanCode == "123456" {
		s.verified[normEmail] = true
		writeJSON(w, http.StatusOK, client.VerifyResponse{
			Status:         "verified",
			Email:          normEmail,
			Role:           "admin",
			KeyFingerprint: "SHA256:mock-verified-fingerprint-" + normEmail,
			VerifiedAt:     now,
			Message:        "OTP verified",
		})
		return
	}

	writeError(w, http.StatusNotFound, "no active verification challenge or grant found for email")
}

func (s *MockServer) handleOnboardClaim(w http.ResponseWriter, r *http.Request) {
	var req provisioner.ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed JSON payload")
		return
	}

	token := strings.TrimSpace(req.InviteToken)
	if token == "" {
		writeError(w, http.StatusBadRequest, "invite_token is required")
		return
	}

	if token == "invalid-token-str" || token == "revoked-token" || strings.HasPrefix(token, "invalid") {
		writeError(w, http.StatusUnauthorized, "invalid or revoked invite token")
		return
	}

	uid := strings.TrimSpace(req.DesiredUID)
	if uid == "" {
		if req.Email != "" && strings.Contains(req.Email, "@") {
			uid = strings.Split(req.Email, "@")[0]
		} else {
			uid = "alice"
		}
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = uid + "@example.com"
	}

	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		name = strings.Title(uid)
	}

	s.mu.Lock()
	if strings.TrimSpace(req.SSHPublicKey) != "" {
		keyID := s.nextSSHKeyID
		s.nextSSHKeyID++
		s.sshKeys[keyID] = &ForgejoKey{
			ID:          keyID,
			Title:       "orbit-" + uid,
			Key:         req.SSHPublicKey,
			Fingerprint: "SHA256:mockfingerprint" + uid,
			CreatedAt:   time.Now().UTC(),
		}
	}
	s.mu.Unlock()

	resp := provisioner.ClaimResponse{
		Status:           "claimed",
		IdempotentReplay: false,
		User: provisioner.User{
			UID:         uid,
			Email:       email,
			DisplayName: name,
			Groups:      []string{"core", "dev"},
		},
		Credentials: provisioner.Credentials{
			ForgejoUsername: uid,
			ForgejoMCPToken: "mock-forgejo-token-" + uid,
			WireGuardConfig: "[Interface]\nPrivateKey = mock-wg-key\nAddress = 10.0.0.2/32\n\n[Peer]\nPublicKey = mock-peer-key\nEndpoint = vpn.dev.manova.space:51820\nAllowedIPs = 10.0.0.0/16\n",
		},
		Workspace: provisioner.WorkspaceInfo{
			GitRemoteBase:        "http://git.dev.manova.space:3000",
			DefaultManifestScope: "core",
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *MockServer) handleOnboardValidate(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		}
	}

	if token == "" || token == "invalid-token-str" || strings.HasPrefix(token, "invalid") {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"status": "invalid",
			"valid":  false,
			"error":  "invalid or expired invite token",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "valid",
		"valid":  true,
		"scope":  "core",
	})
}

// ============================================================================
// Staff Handlers (HMAC Verification, Reserved UID Guards, CRUD State Machine)
// ============================================================================

func (s *MockServer) StaffHandler() http.Handler {
	mux := http.NewServeMux()

	// Health check endpoints (no HMAC required for basic liveness)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// Staff routes (both /api/v1/staff and /v1/staff)
	mux.HandleFunc("POST /api/v1/staff", s.wrapStaffHMAC(s.handleStaffCreate))
	mux.HandleFunc("POST /v1/staff/create", s.wrapStaffHMAC(s.handleStaffCreate))

	mux.HandleFunc("GET /api/v1/staff", s.wrapStaffHMAC(s.handleStaffList))
	mux.HandleFunc("GET /v1/staff/list", s.wrapStaffHMAC(s.handleStaffList))

	mux.HandleFunc("GET /api/v1/staff/{uid}", s.wrapStaffHMAC(s.handleStaffGet))
	mux.HandleFunc("GET /v1/staff/{uid}", s.wrapStaffHMAC(s.handleStaffGet))

	mux.HandleFunc("PATCH /api/v1/staff/{uid}", s.wrapStaffHMAC(s.handleStaffUpdate))
	mux.HandleFunc("PUT /api/v1/staff/{uid}", s.wrapStaffHMAC(s.handleStaffUpdate))
	mux.HandleFunc("PUT /v1/staff/{uid}", s.wrapStaffHMAC(s.handleStaffUpdate))

	mux.HandleFunc("POST /api/v1/staff/{uid}/disable", s.wrapStaffHMAC(s.handleStaffDisable))
	mux.HandleFunc("POST /v1/staff/{uid}/disable", s.wrapStaffHMAC(s.handleStaffDisable))

	mux.HandleFunc("POST /api/v1/staff/{uid}/enable", s.wrapStaffHMAC(s.handleStaffEnable))
	mux.HandleFunc("POST /v1/staff/{uid}/enable", s.wrapStaffHMAC(s.handleStaffEnable))

	mux.HandleFunc("DELETE /api/v1/staff/{uid}", s.wrapStaffHMAC(s.handleStaffDelete))
	mux.HandleFunc("DELETE /v1/staff/{uid}", s.wrapStaffHMAC(s.handleStaffDelete))

	mux.HandleFunc("POST /api/v1/staff/{uid}/reset-password", s.wrapStaffHMAC(s.handleStaffResetPassword))
	mux.HandleFunc("POST /v1/staff/{uid}/reset-password", s.wrapStaffHMAC(s.handleStaffResetPassword))

	mux.HandleFunc("POST /api/v1/staff/{uid}/recreate", s.wrapStaffHMAC(s.handleStaffRecreate))
	mux.HandleFunc("POST /v1/staff/{uid}/recreate", s.wrapStaffHMAC(s.handleStaffRecreate))

	return mux
}

// wrapStaffHMAC enforces HMAC-SHA256 signature verification on staff endpoints.
func (s *MockServer) wrapStaffHMAC(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tsStr := r.Header.Get("X-Orbit-Timestamp")
		sig := r.Header.Get("X-Orbit-Signature")

		if tsStr == "" || sig == "" {
			writeError(w, http.StatusUnauthorized, "bad hmac: missing timestamp or signature header")
			return
		}

		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "bad hmac: invalid timestamp format")
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		if err := staffhmac.Verify(s.cfg.HMACSecret, ts, r.Method, r.URL.Path, bodyBytes, sig, time.Now()); err != nil {
			writeError(w, http.StatusUnauthorized, "bad hmac: "+err.Error())
			return
		}

		next(w, r)
	}
}

func (s *MockServer) handleStaffCreate(w http.ResponseWriter, r *http.Request) {
	var in client.StaffCreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request JSON")
		return
	}

	uid := strings.TrimSpace(in.UID)
	if uid == "" {
		writeError(w, http.StatusBadRequest, "required field: uid")
		return
	}

	if isReservedUID(uid) {
		writeError(w, http.StatusForbidden, "reserved uid")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.staffMembers[uid]; exists {
		writeJSON(w, http.StatusOK, client.StaffCreateResult{
			StaffMember: *existing,
			Idempotent:  true,
		})
		return
	}

	groups := in.Groups
	if len(groups) == 0 {
		groups = []string{"dev"}
	}

	member := &client.StaffMember{
		UID:             uid,
		DisplayName:     strings.TrimSpace(in.DisplayName),
		Mail:            uid + "@dev.manova.space",
		PersonalForward: strings.TrimSpace(in.PersonalForward),
		Groups:          groups,
		Status:          "active",
	}
	s.staffMembers[uid] = member

	otpAuth := ""
	if in.TOTP {
		otpAuth = fmt.Sprintf("otpauth://totp/Orbit:%s@manova.space?secret=JBSWY3DPEHPK3PXP&issuer=Orbit", uid)
	}

	writeJSON(w, http.StatusCreated, client.StaffCreateResult{
		StaffMember:  *member,
		LDAPPassword: "mock-ldap-password-" + uid,
		MailPassword: "mock-mail-password-" + uid,
		OTPAuth:      otpAuth,
		Idempotent:   false,
	})
}

func (s *MockServer) handleStaffList(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]client.StaffMember, 0, len(s.staffMembers))
	for _, m := range s.staffMembers {
		list = append(list, *m)
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *MockServer) handleStaffGet(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("uid"))
	if uid == "" {
		writeError(w, http.StatusBadRequest, "uid required")
		return
	}

	if isReservedUID(uid) {
		writeError(w, http.StatusForbidden, "reserved uid")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	member, exists := s.staffMembers[uid]
	if !exists {
		writeError(w, http.StatusNotFound, "staff member not found")
		return
	}

	writeJSON(w, http.StatusOK, member)
}

func (s *MockServer) handleStaffUpdate(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("uid"))
	if isReservedUID(uid) {
		writeError(w, http.StatusForbidden, "reserved uid")
		return
	}

	var in client.StaffUpdateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request JSON")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	member, exists := s.staffMembers[uid]
	if !exists {
		writeError(w, http.StatusNotFound, "staff member not found")
		return
	}

	if in.DisplayName != "" {
		member.DisplayName = strings.TrimSpace(in.DisplayName)
	}
	if in.PersonalForward != "" {
		member.PersonalForward = strings.TrimSpace(in.PersonalForward)
	}
	if len(in.Groups) > 0 {
		member.Groups = in.Groups
	}

	writeJSON(w, http.StatusOK, member)
}

func (s *MockServer) handleStaffDisable(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("uid"))
	if isReservedUID(uid) {
		writeError(w, http.StatusForbidden, "reserved uid")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	member, exists := s.staffMembers[uid]
	if !exists {
		writeError(w, http.StatusNotFound, "staff member not found")
		return
	}

	member.Status = "disabled"
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled", "uid": uid})
}

func (s *MockServer) handleStaffEnable(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("uid"))
	if isReservedUID(uid) {
		writeError(w, http.StatusForbidden, "reserved uid")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	member, exists := s.staffMembers[uid]
	if !exists {
		writeError(w, http.StatusNotFound, "staff member not found")
		return
	}

	member.Status = "active"
	writeJSON(w, http.StatusOK, map[string]string{"status": "active", "uid": uid})
}

func (s *MockServer) handleStaffDelete(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("uid"))
	if isReservedUID(uid) {
		writeError(w, http.StatusForbidden, "reserved uid")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.staffMembers[uid]; !exists {
		writeError(w, http.StatusNotFound, "staff member not found")
		return
	}

	delete(s.staffMembers, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "uid": uid})
}

func (s *MockServer) handleStaffResetPassword(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("uid"))
	if isReservedUID(uid) {
		writeError(w, http.StatusForbidden, "reserved uid")
		return
	}

	s.mu.RLock()
	_, exists := s.staffMembers[uid]
	s.mu.RUnlock()

	if !exists {
		writeError(w, http.StatusNotFound, "staff member not found")
		return
	}

	writeJSON(w, http.StatusOK, client.StaffResetResult{
		LDAPPassword: "reset-mock-ldap-password-" + uid,
		MailPassword: "reset-mock-mail-password-" + uid,
		OTPAuth:      fmt.Sprintf("otpauth://totp/Orbit:%s@manova.space?secret=JBSWY3DPEHPK3PXP&issuer=Orbit", uid),
	})
}

func (s *MockServer) handleStaffRecreate(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("uid"))
	if isReservedUID(uid) {
		writeError(w, http.StatusForbidden, "reserved uid")
		return
	}

	var in client.StaffCreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request JSON")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	groups := in.Groups
	if len(groups) == 0 {
		groups = []string{"dev"}
	}

	member := &client.StaffMember{
		UID:             uid,
		DisplayName:     strings.TrimSpace(in.DisplayName),
		Mail:            uid + "@dev.manova.space",
		PersonalForward: strings.TrimSpace(in.PersonalForward),
		Groups:          groups,
		Status:          "active",
	}
	s.staffMembers[uid] = member

	otpAuth := ""
	if in.TOTP {
		otpAuth = fmt.Sprintf("otpauth://totp/Orbit:%s@manova.space?secret=JBSWY3DPEHPK3PXP&issuer=Orbit", uid)
	}

	writeJSON(w, http.StatusCreated, client.StaffCreateResult{
		StaffMember:  *member,
		LDAPPassword: "recreated-mock-ldap-password-" + uid,
		MailPassword: "recreated-mock-mail-password-" + uid,
		OTPAuth:      otpAuth,
		Idempotent:   false,
	})
}

// ============================================================================
// Forgejo Mock Handlers (SSH Keys registration and query)
// ============================================================================

func (s *MockServer) ForgejoHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/v1/user/keys", s.handleForgejoGetKeys)
	mux.HandleFunc("POST /api/v1/user/keys", s.handleForgejoPostKeys)
	mux.HandleFunc("DELETE /api/v1/user/keys/{id}", s.handleForgejoDeleteKey)
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": "1.21.0+mock"})
	})

	return mux
}

func (s *MockServer) handleForgejoGetKeys(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]*ForgejoKey, 0, len(s.sshKeys))
	for _, k := range s.sshKeys {
		keys = append(keys, k)
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *MockServer) handleForgejoPostKeys(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Key   string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Key) == "" {
		writeError(w, http.StatusBadRequest, "invalid ssh key payload")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	keyID := s.nextSSHKeyID
	s.nextSSHKeyID++

	keyObj := &ForgejoKey{
		ID:          keyID,
		Title:       in.Title,
		Key:         strings.TrimSpace(in.Key),
		Fingerprint: "SHA256:mock-forgejo-key-fingerprint-" + strconv.Itoa(keyID),
		CreatedAt:   time.Now().UTC(),
	}
	s.sshKeys[keyID] = keyObj

	writeJSON(w, http.StatusCreated, keyObj)
}

func (s *MockServer) handleForgejoDeleteKey(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimSpace(r.PathValue("id"))
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sshKeys, id)
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// S3 / R2 Mock Handlers (PUT, GET, HEAD, DELETE for object storage)
// ============================================================================

func (s *MockServer) S3Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveS3(w, r)
	})
}

func (s *MockServer) serveS3(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/")
	if trimmed == "" || trimmed == "health" || trimmed == "healthz" {
		s.handleHealth(w, r)
		return
	}

	parts := strings.SplitN(trimmed, "/", 2)
	bucket := parts[0]
	key := ""
	if len(parts) > 1 {
		key = parts[1]
	}

	if key == "" {
		// Bucket-level operations (HEAD bucket, GET bucket)
		if r.Method == http.MethodHead || r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPut:
		s.handleS3PutObject(w, r, bucket, key)
	case http.MethodHead:
		s.handleS3HeadObject(w, r, bucket, key)
	case http.MethodGet:
		s.handleS3GetObject(w, r, bucket, key)
	case http.MethodDelete:
		s.handleS3DeleteObject(w, r, bucket, key)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *MockServer) handleS3PutObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read object body", http.StatusInternalServerError)
		return
	}

	meta := make(map[string]string)
	for hKey, vals := range r.Header {
		if strings.HasPrefix(strings.ToLower(hKey), "x-amz-meta-") && len(vals) > 0 {
			meta[strings.ToLower(hKey)] = vals[0]
		}
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	hash := md5.Sum(data)
	etag := fmt.Sprintf("\"%s\"", hex.EncodeToString(hash[:]))

	s.mu.Lock()
	fullKey := bucket + "/" + key
	s.s3Objects[fullKey] = &S3Object{
		Bucket:      bucket,
		Key:         key,
		Data:        data,
		ContentType: contentType,
		ETag:        etag,
		Metadata:    meta,
		UpdatedAt:   time.Now().UTC(),
	}
	s.mu.Unlock()

	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

func (s *MockServer) handleS3HeadObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	fullKey := bucket + "/" + key

	s.mu.RLock()
	obj, exists := s.s3Objects[fullKey]
	s.mu.RUnlock()

	if !exists {
		w.Header().Set("x-amz-error-code", "NoSuchKey")
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(obj.Data)))
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("ETag", obj.ETag)
	for k, v := range obj.Metadata {
		w.Header().Set(k, v)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *MockServer) handleS3GetObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	fullKey := bucket + "/" + key

	s.mu.RLock()
	obj, exists := s.s3Objects[fullKey]
	s.mu.RUnlock()

	if !exists {
		w.Header().Set("x-amz-error-code", "NoSuchKey")
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(obj.Data)))
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("ETag", obj.ETag)
	for k, v := range obj.Metadata {
		w.Header().Set(k, v)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(obj.Data)
}

func (s *MockServer) handleS3DeleteObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	fullKey := bucket + "/" + key

	s.mu.Lock()
	delete(s.s3Objects, fullKey)
	s.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// Unified Handler (For single-port / in-process testbed execution)
// ============================================================================

func (s *MockServer) UnifiedHandler() http.Handler {
	edgeHandler := s.EdgeHandler()
	staffHandler := s.StaffHandler()
	forgejoHandler := s.ForgejoHandler()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// 1. Staff endpoints
		if strings.HasPrefix(path, "/api/v1/staff") || strings.HasPrefix(path, "/v1/staff") {
			staffHandler.ServeHTTP(w, r)
			return
		}

		// 2. Forgejo SSH keys / version endpoints
		if strings.HasPrefix(path, "/api/v1/user/keys") || path == "/api/v1/version" {
			forgejoHandler.ServeHTTP(w, r)
			return
		}

		// 3. Edge / Admin / Onboard / Health endpoints
		if strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/api/v1/") ||
			path == "/health" || path == "/healthz" || path == "/" {
			edgeHandler.ServeHTTP(w, r)
			return
		}

		// 4. Default to S3 mock handler
		s.serveS3(w, r)
	})
}

// Start launches the configured HTTP listeners.
func (s *MockServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cfg.UnifiedAddr != "" {
		lis, err := net.Listen("tcp", s.cfg.UnifiedAddr)
		if err != nil {
			return fmt.Errorf("failed to listen on unified addr %s: %w", s.cfg.UnifiedAddr, err)
		}
		s.listeners = append(s.listeners, &lis)
		srv := &http.Server{Handler: s.UnifiedHandler()}
		s.servers = append(s.servers, srv)
		go func() { _ = srv.Serve(lis) }()
		slog.Info("mockserver unified listener active", "addr", lis.Addr().String())
		return nil
	}

	// Multi-port listeners
	services := []struct {
		name    string
		addr    string
		handler http.Handler
	}{
		{"edge", s.cfg.EdgeAddr, s.EdgeHandler()},
		{"staff", s.cfg.StaffAddr, s.StaffHandler()},
		{"s3", s.cfg.S3Addr, s.S3Handler()},
		{"forgejo", s.cfg.ForgejoAddr, s.ForgejoHandler()},
	}

	for _, svc := range services {
		if svc.addr == "" {
			continue
		}
		lis, err := net.Listen("tcp", svc.addr)
		if err != nil {
			return fmt.Errorf("failed to listen for %s on %s: %w", svc.name, svc.addr, err)
		}
		s.listeners = append(s.listeners, &lis)
		srv := &http.Server{Handler: svc.handler}
		s.servers = append(s.servers, srv)
		go func(name string, l net.Listener, server *http.Server) {
			slog.Info("mockserver service active", "service", name, "addr", l.Addr().String())
			_ = server.Serve(l)
		}(svc.name, lis, srv)
	}

	return nil
}

// Close terminates all active servers and listeners.
func (s *MockServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, srv := range s.servers {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
	for _, lis := range s.listeners {
		if lis != nil {
			_ = (*lis).Close()
		}
	}
	return nil
}

// ============================================================================
// Standalone Binary Entrypoint
// ============================================================================

func main() {
	var cfg Config
	flag.StringVar(&cfg.EdgeAddr, "edge-addr", DefaultEdgePort, "Edge listener address (default :8080)")
	flag.StringVar(&cfg.StaffAddr, "staff-addr", DefaultStaffPort, "Staff control plane listener address (default :10800)")
	flag.StringVar(&cfg.S3Addr, "s3-addr", DefaultS3Port, "S3/R2 mock storage listener address (default :9000)")
	flag.StringVar(&cfg.ForgejoAddr, "forgejo-addr", DefaultForgejoPort, "Forgejo mock listener address (default :3000)")
	flag.StringVar(&cfg.UnifiedAddr, "unified-addr", "", "Single unified port mode (overrides individual listeners if set)")
	flag.StringVar(&cfg.HMACSecret, "hmac-secret", DefaultHMACSecret, "Secret for staff HMAC-SHA256 signature verification")
	flag.StringVar(&cfg.InviteSecret, "invite-secret", DefaultInviteSecret, "Secret for onboarding invitation signing")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging")
	flag.Parse()

	// Allow environment variable overrides
	if env := os.Getenv("ORBIT_STAFF_HMAC_SECRET"); env != "" {
		cfg.HMACSecret = env
	}
	if env := os.Getenv("ORBIT_SIGNING_SECRET"); env != "" {
		cfg.InviteSecret = env
	}
	if env := os.Getenv("ORBIT_MOCK_EDGE_ADDR"); env != "" {
		cfg.EdgeAddr = env
	}
	if env := os.Getenv("ORBIT_MOCK_STAFF_ADDR"); env != "" {
		cfg.StaffAddr = env
	}
	if env := os.Getenv("ORBIT_MOCK_S3_ADDR"); env != "" {
		cfg.S3Addr = env
	}
	if env := os.Getenv("ORBIT_MOCK_FORGEJO_ADDR"); env != "" {
		cfg.ForgejoAddr = env
	}
	if env := os.Getenv("ORBIT_MOCK_UNIFIED_ADDR"); env != "" {
		cfg.UnifiedAddr = env
	}

	server := NewMockServer(cfg)
	if err := server.Start(); err != nil {
		slog.Error("failed to start mock server", "error", err)
		os.Exit(1)
	}

	fmt.Printf("🚀 Orbit Mock Server Daemon started successfully\n")
	if cfg.UnifiedAddr != "" {
		fmt.Printf("  • Unified HTTP: %s\n", cfg.UnifiedAddr)
	} else {
		fmt.Printf("  • Edge API:     %s\n", cfg.EdgeAddr)
		fmt.Printf("  • Staff API:    %s\n", cfg.StaffAddr)
		fmt.Printf("  • S3/R2 Mock:   %s\n", cfg.S3Addr)
		fmt.Printf("  • Forgejo Mock: %s\n", cfg.ForgejoAddr)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Printf("\nShutting down Orbit Mock Server...\n")
	_ = server.Close()
	fmt.Printf("Mock server stopped.\n")
}
