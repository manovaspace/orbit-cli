package onboard

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/invite"
	"github.com/manovaspace/orbit-cli/pkg/onboard/middleware"
	"github.com/manovaspace/orbit-cli/pkg/onboard/ratelimit"
	"github.com/manovaspace/orbit-cli/pkg/owner"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/manovaspace/orbit-cli/pkg/serverstore"
)

//go:embed install.sh
var canonicalInstallScript []byte

//go:embed landing.html
var installLandingHTML []byte

// ServerConfig configures the onboard edge HTTP server.
type ServerConfig struct {
	Addr                    string
	Secret                  []byte
	Provisioner             provisioner.Provisioner
	InviteStore             *invite.Store
	Store                   serverstore.Store
	RateLimitStore          serverstore.RateLimitStore
	ChallengeManager        *owner.ChallengeManager
	GrantManager            *owner.GrantManager
	Mailer                  invite.Mailer
	DisablePublicChallenges bool          // When true, rejects unauthenticated /api/v1/admin/challenge requests
	AllowedAdminEmails      []string      // When non-empty, restricts challenge requests to specific emails
	RateLimit               int           // Maximum requests per window per IP (default 10)
	RateInterval            time.Duration // Sliding window duration (default 1 minute)
	IdempotencyTTL          time.Duration // Retention time for cached claims (default 24h)
	DisableRateLimit        bool          // Disables rate limiting for tests
	TrustedProxies          []string      // Trusted reverse proxy CIDRs or IP addresses
	Limiter                 *ratelimit.Limiter
	ReadTimeout             time.Duration
	WriteTimeout            time.Duration
	Logger                  *slog.Logger
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

// Server is the HTTP edge service handling onboarding claims and health status.
type Server struct {
	config     ServerConfig
	mux        *http.ServeMux
	handler    http.Handler
	idemCache  *idempotencyCache
	limiter    *ratelimit.Limiter
	httpServer *http.Server
	mu         sync.Mutex

	// Tier limit configurations:
	claimIPLimit         int
	claimIPWindow        time.Duration
	claimKeyLimit        int
	claimKeyWindow       time.Duration
	challengeIPLimit     int
	challengeIPWindow    time.Duration
	challengeEmailLimit  int
	challengeEmailWindow time.Duration
	verifyIPLimit        int
	verifyIPWindow       time.Duration
	healthIPLimit        int
	healthIPWindow       time.Duration
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
	if cfg.GrantManager == nil {
		cfg.GrantManager = owner.NewGrantManager()
	}
	if cfg.Mailer == nil {
		cfg.Mailer = invite.NewMailerFromEnv()
	}

	rateStore := cfg.RateLimitStore
	if rateStore == nil && cfg.Store != nil {
		rateStore = cfg.Store.RateLimits()
	}

	lim := cfg.Limiter
	if lim == nil {
		lim = ratelimit.NewLimiter(rateStore, ratelimit.LimiterOptions{
			TrustedProxies: cfg.TrustedProxies,
			DefaultLimit:   cfg.RateLimit,
			DefaultWindow:  cfg.RateInterval,
		})
	}

	// Sensible default tier limits
	claimIPLimit := 10
	claimIPWindow := time.Minute
	challengeIPLimit := 5
	challengeIPWindow := 5 * time.Minute
	verifyIPLimit := 10
	verifyIPWindow := 5 * time.Minute
	healthIPLimit := 60
	healthIPWindow := time.Minute

	if cfg.RateLimit > 0 && cfg.RateLimit != 10 {
		claimIPLimit = cfg.RateLimit
		challengeIPLimit = cfg.RateLimit
		verifyIPLimit = cfg.RateLimit
		healthIPLimit = cfg.RateLimit
	}
	if cfg.RateInterval > 0 && cfg.RateInterval != time.Minute {
		claimIPWindow = cfg.RateInterval
		challengeIPWindow = cfg.RateInterval
		verifyIPWindow = cfg.RateInterval
		healthIPWindow = cfg.RateInterval
	}

	s := &Server{
		config:               cfg,
		mux:                  http.NewServeMux(),
		idemCache:            newIdempotencyCache(),
		limiter:              lim,
		claimIPLimit:         claimIPLimit,
		claimIPWindow:        claimIPWindow,
		claimKeyLimit:        5,
		claimKeyWindow:       10 * time.Minute,
		challengeIPLimit:     challengeIPLimit,
		challengeIPWindow:    challengeIPWindow,
		challengeEmailLimit:  3,
		challengeEmailWindow: 10 * time.Minute,
		verifyIPLimit:        verifyIPLimit,
		verifyIPWindow:       verifyIPWindow,
		healthIPLimit:        healthIPLimit,
		healthIPWindow:       healthIPWindow,
	}

	s.routes()
	s.wrapMiddleware()
	return s, nil
}

func (s *Server) wrapMiddleware() {
	logger := s.config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Chain: Recovery(logger) -> RequestID -> TraceContext -> Logging(logger) -> mux
	var h http.Handler = s.mux
	h = middleware.Logging(logger)(h)
	h = middleware.TraceContext(h)
	h = middleware.RequestID(h)
	h = middleware.Recovery(logger)(h)

	s.handler = h
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleInstallScript)
	s.mux.HandleFunc("GET /setup", s.handleInstallScript)
	s.mux.HandleFunc("GET /onboard", s.handleInstallScript)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /api/v1/onboard/claim", s.handleClaim)
	s.mux.HandleFunc("POST /api/v1/admin/challenge", s.handleAdminChallenge)
	s.mux.HandleFunc("POST /api/v1/admin/verify", s.handleAdminVerify)
	s.mux.HandleFunc("POST /api/v1/admin/grants", s.handleAdminGrantCreate)
	s.mux.HandleFunc("GET /api/v1/admin/grants", s.handleAdminGrantList)
}

// Handler returns the http.Handler for embedding or testing.
func (s *Server) Handler() http.Handler {
	if s.handler != nil {
		return s.handler
	}
	return s.mux
}

// Limiter returns the active rate limiter instance.
func (s *Server) Limiter() *ratelimit.Limiter {
	return s.limiter
}

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path != "/" && path != "/setup" && path != "/onboard" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Vary", "Accept, User-Agent")
	if wantsInstallHTML(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(installLandingHTML)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(canonicalInstallScript)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.config.DisableRateLimit && s.limiter != nil {
		allowed, retryAfter, _ := s.limiter.AllowIP(r.Context(), r, "/health", s.healthIPLimit, s.healthIPWindow)
		if !allowed {
			retrySec := int(retryAfter.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(retrySec))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded, please try again later", retrySec)
			return
		}
	}

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
	// 1. IP Rate Limiting
	if !s.config.DisableRateLimit && s.limiter != nil {
		allowed, retryAfter, _ := s.limiter.AllowIP(r.Context(), r, "/api/v1/onboard/claim", s.claimIPLimit, s.claimIPWindow)
		if !allowed {
			retrySec := int(retryAfter.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(retrySec))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded, please try again later", retrySec)
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

	// 3. Token / Key Rate Limiting
	if !s.config.DisableRateLimit && s.limiter != nil && req.InviteToken != "" {
		allowed, retryAfter, _ := s.limiter.AllowKey(r.Context(), req.InviteToken, "/api/v1/onboard/claim/token", s.claimKeyLimit, s.claimKeyWindow)
		if !allowed {
			retrySec := int(retryAfter.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(retrySec))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded for token, please try again later", retrySec)
			return
		}
	}

	// 4. Idempotency Key Determination & Cache Lookup
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

	// 5. Token validation
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

	// 6. Revocation check via Store (if configured)
	if s.config.Store != nil {
		rec, err := s.config.Store.Invites().GetInvite(r.Context(), claims.ID)
		if err == nil && rec != nil && rec.Revoked {
			writeJSONError(w, http.StatusForbidden, "invitation token has been revoked", 0)
			return
		}
	} else if s.config.InviteStore != nil {
		rec, err := s.config.InviteStore.GetInvite(claims.ID)
		if err == nil && rec != nil && rec.Revoked {
			writeJSONError(w, http.StatusForbidden, "invitation token has been revoked", 0)
			return
		}
	}

	// 7. Enrich request with validated claims data
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

	// 8. Invoke Provisioner
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

	// 9. Cache response for idempotency replay
	resp.IdempotentReplay = false
	if idempKey != "" {
		s.idemCache.Set(idempKey, resp, s.config.IdempotencyTTL)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminChallenge(w http.ResponseWriter, r *http.Request) {
	// 1. IP Rate Limiting
	if !s.config.DisableRateLimit && s.limiter != nil {
		allowed, retryAfter, _ := s.limiter.AllowIP(r.Context(), r, "/api/v1/admin/challenge", s.challengeIPLimit, s.challengeIPWindow)
		if !allowed {
			retrySec := int(retryAfter.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(retrySec))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded, please try again later", retrySec)
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

	// 3. Email Rate Limiting
	if !s.config.DisableRateLimit && s.limiter != nil {
		allowed, retryAfter, _ := s.limiter.AllowKey(r.Context(), normEmail, "/api/v1/admin/challenge/email", s.challengeEmailLimit, s.challengeEmailWindow)
		if !allowed {
			retrySec := int(retryAfter.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(retrySec))
			writeJSONError(w, http.StatusTooManyRequests, fmt.Sprintf("rate limit exceeded for email %s, please try again later", normEmail), retrySec)
			return
		}
	}

	// 4. Security: Check if public challenges are disabled
	if s.config.DisablePublicChallenges {
		writeJSONError(w, http.StatusForbidden, "public challenge requests are disabled for security; please use an owner-issued 8-digit admin grant code (orbit admin grant <email>)", 0)
		return
	}

	// 5. Security: Check allowlist if configured
	if len(s.config.AllowedAdminEmails) > 0 {
		allowed := false
		for _, ae := range s.config.AllowedAdminEmails {
			if strings.ToLower(strings.TrimSpace(ae)) == normEmail {
				allowed = true
				break
			}
		}
		if !allowed {
			writeJSONError(w, http.StatusForbidden, fmt.Sprintf("email %s is not in the administrator allowlist", normEmail), 0)
			return
		}
	}

	// 6. Create Challenge
	if s.config.ChallengeManager == nil {
		writeJSONError(w, http.StatusInternalServerError, "challenge manager not initialized", 0)
		return
	}

	ch, otp, err := s.config.ChallengeManager.CreateChallenge(normEmail, owner.DefaultChallengeTTL)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate verification challenge: %v", err), 0)
		return
	}

	// 7. Dispatch Email via Mailer
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
	// 1. IP Rate Limiting
	if !s.config.DisableRateLimit && s.limiter != nil {
		allowed, retryAfter, _ := s.limiter.AllowIP(r.Context(), r, "/api/v1/admin/verify", s.verifyIPLimit, s.verifyIPWindow)
		if !allowed {
			retrySec := int(retryAfter.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(retrySec))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded, please try again later", retrySec)
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
	rawCode := strings.TrimSpace(req.Code)
	cleanCode := owner.CleanCode(rawCode)

	if normEmail == "" {
		writeJSONError(w, http.StatusBadRequest, "email cannot be empty", 0)
		return
	}
	if cleanCode == "" {
		writeJSONError(w, http.StatusBadRequest, "verification code cannot be empty", 0)
		return
	}

	keyFingerprint := owner.ComputeFingerprint(string(s.config.Secret))

	// 3. Priority A: Check if valid 8-digit admin grant
	if s.config.GrantManager != nil && len(cleanCode) == 8 {
		grant, err := s.config.GrantManager.VerifyGrant(normEmail, cleanCode)
		if err == nil && grant != nil {
			writeJSON(w, http.StatusOK, client.VerifyResponse{
				Status:         "verified",
				Email:          normEmail,
				DisplayName:    strings.TrimSpace(req.DisplayName),
				KeyFingerprint: keyFingerprint,
				VerifiedAt:     time.Now().UTC(),
				Message:        fmt.Sprintf("admin grant successfully verified for %s (role: %s)", normEmail, grant.Role),
			})
			return
		}
		if err != nil && !errors.Is(err, owner.ErrGrantNotFound) {
			writeJSONError(w, http.StatusBadRequest, err.Error(), 0)
			return
		}
	}

	// 4. Priority B: Check 6-digit legacy / self challenge
	if s.config.ChallengeManager != nil {
		ok, err := s.config.ChallengeManager.VerifyCode(normEmail, cleanCode)
		if err == nil && ok {
			writeJSON(w, http.StatusOK, client.VerifyResponse{
				Status:         "verified",
				Email:          normEmail,
				DisplayName:    strings.TrimSpace(req.DisplayName),
				KeyFingerprint: keyFingerprint,
				VerifiedAt:     time.Now().UTC(),
				Message:        fmt.Sprintf("platform ownership successfully verified for %s", normEmail),
			})
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), 0)
			return
		}
	}

	writeJSONError(w, http.StatusBadRequest, "invalid verification code", 0)
}

func (s *Server) handleAdminGrantCreate(w http.ResponseWriter, r *http.Request) {
	// 1. Authenticate with root secret or valid token
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if strings.TrimSpace(token) != string(s.config.Secret) {
		if _, err := invite.ValidateToken(token, s.config.Secret); err != nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized: valid master secret or token required", 0)
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Email      string `json:"email"`
		Role       string `json:"role"`
		Code       string `json:"code,omitempty"`
		TTLSeconds int    `json:"ttl_seconds,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("malformed request JSON: %v", err), 0)
		return
	}

	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = owner.DefaultGrantTTL
	}

	var (
		rec           *owner.GrantRecord
		codeFormatted string
		err           error
	)

	if req.Code != "" {
		rec, err = s.config.GrantManager.RegisterGrantWithCode(req.Email, req.Role, req.Code, "remote-owner", ttl)
		codeFormatted = owner.Format8DigitCode(req.Code)
	} else {
		rec, codeFormatted, err = s.config.GrantManager.CreateGrant(req.Email, req.Role, "remote-owner", ttl)
	}

	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), 0)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status":     "grant_created",
		"id":         rec.ID,
		"email":      rec.Email,
		"role":       rec.Role,
		"code":       codeFormatted,
		"expires_at": rec.ExpiresAt,
	})
}

func (s *Server) handleAdminGrantList(w http.ResponseWriter, r *http.Request) {
	// Authenticate with root secret
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if strings.TrimSpace(token) != string(s.config.Secret) {
		if _, err := invite.ValidateToken(token, s.config.Secret); err != nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized: valid master secret or token required", 0)
			return
		}
	}

	email := r.URL.Query().Get("email")
	if email != "" {
		grant, exists := s.config.GrantManager.GetGrant(email)
		if !exists {
			writeJSONError(w, http.StatusNotFound, "grant not found", 0)
			return
		}
		writeJSON(w, http.StatusOK, grant)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "active grants ledger",
	})
}

func (s *Server) getClientIP(r *http.Request) string {
	if s.limiter != nil {
		return s.limiter.ExtractClientIP(r)
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
		Handler:      s.Handler(),
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
