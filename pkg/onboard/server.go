package onboard

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
)

//go:embed install.sh
var canonicalInstallScript []byte

// ServerConfig configures the onboard edge HTTP server.
type ServerConfig struct {
	Addr             string
	Secret           []byte
	Provisioner      provisioner.Provisioner
	InviteStore      *invite.Store
	ChallengeManager *owner.ChallengeManager
	Mailer           invite.Mailer
	RateLimit        int           // Maximum requests per window per IP (default 10)
	RateInterval     time.Duration // Sliding window duration (default 1 minute)
	IdempotencyTTL   time.Duration // Retention time for cached claims (default 24h)
	DisableRateLimit bool          // Disables rate limiting for tests
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
}

// ErrorResponse defines the standard structured JSON error format.
type ErrorResponse struct {
	Error             string `json:"error"`
	Code              int    `json:"code"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

// cachedClaim represents a stored response for an idempotency key.
type cachedClaim struct {
	response  *provisioner.ClaimResponse
	expiresAt time.Time
}

// idempotencyCache provides thread-safe, TTL-based caching for claim replay responses.
type idempotencyCache struct {
	mu    sync.RWMutex
	items map[string]cachedClaim
}

func newIdempotencyCache() *idempotencyCache {
	return &idempotencyCache{
		items: make(map[string]cachedClaim),
	}
}

func (c *idempotencyCache) Get(key string) (*provisioner.ClaimResponse, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[key]
	if !exists {
		return nil, false
	}
	if time.Now().UTC().After(item.expiresAt) {
		return nil, false
	}

	// Return a copy to ensure caller mutations do not alter cache
	respCopy := *item.response
	return &respCopy, true
}

func (c *idempotencyCache) Set(key string, resp *provisioner.ClaimResponse, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	respCopy := *resp
	c.items[key] = cachedClaim{
		response:  &respCopy,
		expiresAt: time.Now().UTC().Add(ttl),
	}
}

// ipRateLimiter provides in-memory sliding window rate limiting per IP address.
type ipRateLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	requests map[string][]time.Time
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	if limit <= 0 {
		limit = 10
	}
	if window <= 0 {
		window = time.Minute
	}
	return &ipRateLimiter{
		limit:    limit,
		window:   window,
		requests: make(map[string][]time.Time),
	}
}

func (l *ipRateLimiter) Allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC()
	cutoff := now.Add(-l.window)

	timestamps := l.requests[ip]
	valid := make([]time.Time, 0, len(timestamps))
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= l.limit {
		oldest := valid[0]
		retryAfter := oldest.Add(l.window).Sub(now)
		if retryAfter < 0 {
			retryAfter = time.Second
		}
		l.requests[ip] = valid
		return false, retryAfter
	}

	valid = append(valid, now)
	l.requests[ip] = valid
	return true, 0
}

// Server is the HTTP edge service handling onboarding claims and health status.
type Server struct {
	config     ServerConfig
	mux        *http.ServeMux
	idemCache  *idempotencyCache
	limiter    *ipRateLimiter
	httpServer *http.Server
	mu         sync.Mutex
}

// NewServer initializes an onboard edge Server with the provided configuration.
func NewServer(cfg ServerConfig) (*Server, error) {
	if len(cfg.Secret) == 0 {
		return nil, errors.New("signing secret cannot be empty")
	}
	if cfg.Provisioner == nil {
		cfg.Provisioner = provisioner.NewDevProvisioner()
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 10
	}
	if cfg.RateInterval <= 0 {
		cfg.RateInterval = time.Minute
	}
	if cfg.IdempotencyTTL <= 0 {
		cfg.IdempotencyTTL = 24 * time.Hour
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 15 * time.Second
	}
	if cfg.ChallengeManager == nil {
		cfg.ChallengeManager = owner.NewChallengeManager()
	}
	if cfg.Mailer == nil {
		cfg.Mailer = invite.NewMailerFromEnv()
	}

	s := &Server{
		config:    cfg,
		mux:       http.NewServeMux(),
		idemCache: newIdempotencyCache(),
		limiter:   newIPRateLimiter(cfg.RateLimit, cfg.RateInterval),
	}

	s.routes()
	return s, nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleInstallScript)
	s.mux.HandleFunc("GET /install", s.handleInstallScript)
	s.mux.HandleFunc("GET /install.sh", s.handleInstallScript)
	s.mux.HandleFunc("GET /v1/onboard/health", s.handleHealth)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /v1/onboard/claim", s.handleClaim)
	s.mux.HandleFunc("POST /api/v1/admin/challenge", s.handleAdminChallenge)
	s.mux.HandleFunc("POST /api/v1/system/ownership/challenge", s.handleAdminChallenge)
	s.mux.HandleFunc("POST /api/v1/admin/verify", s.handleAdminVerify)
	s.mux.HandleFunc("POST /api/v1/system/ownership/verify", s.handleAdminVerify)
}

// Handler returns the http.Handler for embedding or testing.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/install" && r.URL.Path != "/install.sh" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(canonicalInstallScript)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := s.config.Provisioner.Health(ctx)

	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status": "degraded",
			"error":  err.Error(),
			"code":   http.StatusServiceUnavailable,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"provisioner": "healthy",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	// 1. Rate Limiting
	if !s.config.DisableRateLimit {
		ip := s.getClientIP(r)
		allowed, retryAfter := s.limiter.Allow(ip)
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded, please try again later", int(retryAfter.Seconds())+1)
			return
		}
	}

	// 2. Request body parsing
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	var req provisioner.ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("malformed request JSON: %v", err), 0)
		return
	}

	// 3. Idempotency Key Determination & Cache Lookup
	idempKey := r.Header.Get("Idempotency-Key")
	if idempKey == "" {
		idempKey = r.Header.Get("X-Idempotency-Key")
	}
	if idempKey == "" && req.InviteToken != "" {
		idempKey = invite.ComputeIdempotencyKey(req.InviteToken, req.MachineFingerprint)
	}

	if idempKey != "" {
		if cached, ok := s.idemCache.Get(idempKey); ok {
			cached.IdempotentReplay = true
			w.Header().Set("Idempotent-Replay", "true")
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}

	// 4. Token validation
	if req.InviteToken == "" {
		writeJSONError(w, http.StatusBadRequest, "missing invite_token", 0)
		return
	}

	claims, err := invite.ValidateToken(req.InviteToken, s.config.Secret)
	if err != nil {
		if errors.Is(err, invite.ErrTokenExpired) {
			writeJSONError(w, http.StatusUnauthorized, "invite token has expired", 0)
			return
		}
		if errors.Is(err, invite.ErrInvalidSignature) {
			writeJSONError(w, http.StatusUnauthorized, "invalid token signature", 0)
			return
		}
		if errors.Is(err, invite.ErrMalformedToken) {
			writeJSONError(w, http.StatusBadRequest, "malformed invite token", 0)
			return
		}
		writeJSONError(w, http.StatusUnauthorized, err.Error(), 0)
		return
	}

	// 5. Revocation check via Store (if configured)
	if s.config.InviteStore != nil {
		rec, err := s.config.InviteStore.GetInvite(claims.ID)
		if err == nil && rec != nil && rec.Revoked {
			writeJSONError(w, http.StatusForbidden, "invitation token has been revoked", 0)
			return
		}
	}

	// 6. Enrich request with validated claims data
	req.Email = claims.Email
	if req.DisplayName == "" {
		req.DisplayName = claims.DisplayName
	}
	req.Scope = claims.Scope
	if req.Metadata == nil {
		req.Metadata = claims.Metadata
	}
	if req.DesiredUID == "" {
		req.DesiredUID = strings.Split(claims.Email, "@")[0]
	}

	// 7. Invoke Provisioner
	resp, err := s.config.Provisioner.Provision(r.Context(), req)
	if err != nil {
		if errors.Is(err, provisioner.ErrUserAlreadyExists) {
			writeJSONError(w, http.StatusConflict, err.Error(), 0)
			return
		}
		if errors.Is(err, provisioner.ErrInvalidRequest) {
			writeJSONError(w, http.StatusBadRequest, err.Error(), 0)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error(), 0)
		return
	}

	// 8. Cache response for idempotency replay
	resp.IdempotentReplay = false
	if idempKey != "" {
		s.idemCache.Set(idempKey, resp, s.config.IdempotencyTTL)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminChallenge(w http.ResponseWriter, r *http.Request) {
	// 1. Rate Limiting
	if !s.config.DisableRateLimit {
		ip := s.getClientIP(r)
		allowed, retryAfter := s.limiter.Allow(ip)
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded, please try again later", int(retryAfter.Seconds())+1)
			return
		}
	}

	// 2. Request body parsing
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req client.ChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("malformed request JSON: %v", err), 0)
		return
	}

	normEmail := strings.ToLower(strings.TrimSpace(req.Email))
	if normEmail == "" {
		writeJSONError(w, http.StatusBadRequest, "email cannot be empty", 0)
		return
	}
	if !strings.Contains(normEmail, "@") || !strings.Contains(normEmail, ".") {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid email address %q", req.Email), 0)
		return
	}

	// 3. Create Challenge
	if s.config.ChallengeManager == nil {
		writeJSONError(w, http.StatusInternalServerError, "challenge manager not initialized", 0)
		return
	}

	ch, otp, err := s.config.ChallengeManager.CreateChallenge(normEmail, owner.DefaultChallengeTTL)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate verification challenge: %v", err), 0)
		return
	}

	// 4. Dispatch Email via Mailer
	if s.config.Mailer == nil {
		writeJSONError(w, http.StatusInternalServerError, "mailer service not configured", 0)
		return
	}

	emailData := invite.OwnerChallengeEmailData{
		OwnerEmail:  normEmail,
		OTPCode:     otp,
		ExpiresIn:   "10 minutes",
		ServerHost:  r.Host,
		GeneratedAt: time.Now().UTC(),
	}

	if err := s.config.Mailer.SendOwnerChallenge(r.Context(), normEmail, emailData); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to send challenge email: %v", err), 0)
		return
	}

	writeJSON(w, http.StatusOK, client.ChallengeResponse{
		Status:    "challenge_sent",
		Email:     normEmail,
		ExpiresAt: ch.ExpiresAt,
		Message:   fmt.Sprintf("verification OTP sent to %s", normEmail),
	})
}

func (s *Server) handleAdminVerify(w http.ResponseWriter, r *http.Request) {
	// 1. Rate Limiting
	if !s.config.DisableRateLimit {
		ip := s.getClientIP(r)
		allowed, retryAfter := s.limiter.Allow(ip)
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded, please try again later", int(retryAfter.Seconds())+1)
			return
		}
	}

	// 2. Request body parsing
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req client.VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("malformed request JSON: %v", err), 0)
		return
	}

	normEmail := strings.ToLower(strings.TrimSpace(req.Email))
	cleanCode := strings.TrimSpace(req.Code)

	if normEmail == "" {
		writeJSONError(w, http.StatusBadRequest, "email cannot be empty", 0)
		return
	}
	if cleanCode == "" {
		writeJSONError(w, http.StatusBadRequest, "verification code cannot be empty", 0)
		return
	}

	if s.config.ChallengeManager == nil {
		writeJSONError(w, http.StatusInternalServerError, "challenge manager not initialized", 0)
		return
	}

	// 3. Verify OTP code
	ok, err := s.config.ChallengeManager.VerifyCode(normEmail, cleanCode)
	if err != nil || !ok {
		msg := "invalid verification code"
		if err != nil {
			msg = err.Error()
		}
		writeJSONError(w, http.StatusBadRequest, msg, 0)
		return
	}

	// 4. Generate Key Fingerprint & Respond
	keyFingerprint := owner.ComputeFingerprint(string(s.config.Secret))

	writeJSON(w, http.StatusOK, client.VerifyResponse{
		Status:         "verified",
		Email:          normEmail,
		DisplayName:    strings.TrimSpace(req.DisplayName),
		KeyFingerprint: keyFingerprint,
		VerifiedAt:     time.Now().UTC(),
		Message:        fmt.Sprintf("platform ownership successfully verified for %s", normEmail),
	})
}

func (s *Server) getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// Start runs the HTTP server on the configured address.
func (s *Server) Start() error {
	addr := s.config.Addr
	if addr == "" {
		addr = ":8080"
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	return s.Serve(lis)
}

// Serve accepts incoming HTTP connections on listener lis.
func (s *Server) Serve(lis net.Listener) error {
	s.mu.Lock()
	s.httpServer = &http.Server{
		Handler:      s.mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}
	s.mu.Unlock()

	return s.httpServer.Serve(lis)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpServer
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// Close immediately closes active connections on the server.
func (s *Server) Close() error {
	s.mu.Lock()
	srv := s.httpServer
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	return srv.Close()
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, statusCode int, message string, retryAfterSec int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error:             message,
		Code:              statusCode,
		RetryAfterSeconds: retryAfterSec,
	})
}
