package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/serverstore"
)

// DefaultTrustedCIDRs contains the standard loopback and private RFC 1918 / ULA ranges.
var DefaultTrustedCIDRs = []string{
	"127.0.0.1/32",  // IPv4 loopback
	"::1/128",       // IPv6 loopback
	"10.0.0.0/8",    // RFC 1918 Class A
	"172.16.0.0/12", // RFC 1918 Class B
	"192.168.0.0/16",// RFC 1918 Class C
	"fc00::/7",      // IPv6 Unique Local Address (ULA)
}

// LimiterOptions configures trusted proxy subnets and default rate limit behavior.
type LimiterOptions struct {
	TrustedProxies []string      // CIDR blocks or single IPs (defaults to DefaultTrustedCIDRs if empty)
	DefaultLimit   int           // Default maximum requests per window
	DefaultWindow  time.Duration // Default sliding window duration
}

// Limiter provides sliding-window rate limiting with hardened reverse proxy IP extraction.
type Limiter struct {
	store         serverstore.RateLimitStore
	trustedNets   []*net.IPNet
	defaultLimit  int
	defaultWindow time.Duration
	memMu         sync.Mutex
	memEvents     map[string][]time.Time
}

// NewLimiter initializes a Limiter backed by an optional serverstore.RateLimitStore.
// If store is nil, an in-memory sliding-window cache is used.
func NewLimiter(store serverstore.RateLimitStore, opts LimiterOptions) *Limiter {
	defaultLimit := opts.DefaultLimit
	if defaultLimit <= 0 {
		defaultLimit = 10
	}
	defaultWindow := opts.DefaultWindow
	if defaultWindow <= 0 {
		defaultWindow = time.Minute
	}

	return &Limiter{
		store:         store,
		trustedNets:   parseCIDRs(opts.TrustedProxies),
		defaultLimit:  defaultLimit,
		defaultWindow: defaultWindow,
		memEvents:     make(map[string][]time.Time),
	}
}

// parseCIDRs parses a list of CIDR strings or single IP addresses into []*net.IPNet.
func parseCIDRs(cidrs []string) []*net.IPNet {
	if len(cidrs) == 0 {
		cidrs = DefaultTrustedCIDRs
	}

	var nets []*net.IPNet
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip != nil {
				if ip.To4() != nil {
					raw += "/32"
				} else {
					raw += "/128"
				}
			}
		}
		_, ipNet, err := net.ParseCIDR(raw)
		if err == nil && ipNet != nil {
			nets = append(nets, ipNet)
		}
	}
	return nets
}

// isTrusted checks if an IP is within configured trusted proxy subnets.
func (l *Limiter) isTrusted(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range l.trustedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ExtractClientIP extracts the verified client IP address from an incoming HTTP request.
// If the remote address is NOT in TrustedProxies, forwarding headers are strictly ignored to prevent spoofing.
// If the remote address is a trusted proxy, it validates CF-Connecting-IP and X-Forwarded-For (rightmost untrusted IP).
func (l *Limiter) ExtractClientIP(r *http.Request) string {
	if r == nil {
		return "127.0.0.1"
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	remoteIP := net.ParseIP(host)
	if remoteIP == nil {
		return host
	}

	// If the immediate connecting peer is NOT a trusted proxy, ignore all forwarding headers.
	if !l.isTrusted(remoteIP) {
		return remoteIP.String()
	}

	// 1. Cloudflare CF-Connecting-IP header (trusted edge injection)
	if cfIP := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
		if parsed := net.ParseIP(cfIP); parsed != nil {
			return parsed.String()
		}
	}

	// 2. X-Forwarded-For header (rightmost untrusted IP in proxy chain)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		var ips []net.IP
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			h, _, err := net.SplitHostPort(part)
			if err != nil {
				h = part
			}
			if p := net.ParseIP(h); p != nil {
				ips = append(ips, p)
			}
		}

		// Traverse right-to-left: the first untrusted IP from the right is the client IP.
		for i := len(ips) - 1; i >= 0; i-- {
			if !l.isTrusted(ips[i]) {
				return ips[i].String()
			}
		}

		// If all IPs in XFF are inside trusted ranges, return the leftmost IP.
		if len(ips) > 0 {
			return ips[0].String()
		}
	}

	// 3. X-Real-IP header fallback
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		h, _, err := net.SplitHostPort(xrip)
		if err != nil {
			h = xrip
		}
		if parsed := net.ParseIP(h); parsed != nil {
			return parsed.String()
		}
	}

	return remoteIP.String()
}

// AllowKey checks sliding window rate limit for an arbitrary key (e.g. IP, email, or token) on a specific route.
// Returns (allowed, retryAfter, error).
func (l *Limiter) AllowKey(ctx context.Context, key, route string, limit int, window time.Duration) (bool, time.Duration, error) {
	if limit <= 0 {
		limit = l.defaultLimit
	}
	if limit <= 0 {
		limit = 10
	}
	if window <= 0 {
		window = l.defaultWindow
	}
	if window <= 0 {
		window = time.Minute
	}

	now := time.Now().UTC()

	// Persistent store path
	if l.store != nil {
		since := now.Add(-window)
		count, err := l.store.CountEventsSince(ctx, key, route, since)
		if err != nil {
			return false, 0, fmt.Errorf("rate limit store query failed: %w", err)
		}
		if count >= limit {
			return false, window, nil
		}
		if err := l.store.RecordEvent(ctx, key, route, now); err != nil {
			return false, 0, fmt.Errorf("rate limit store record failed: %w", err)
		}
		return true, 0, nil
	}

	// In-memory sliding window path
	l.memMu.Lock()
	defer l.memMu.Unlock()

	memKey := route + ":" + key
	cutoff := now.Add(-window)
	timestamps := l.memEvents[memKey]
	valid := make([]time.Time, 0, len(timestamps)+1)
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= limit {
		oldest := valid[0]
		retryAfter := oldest.Add(window).Sub(now)
		if retryAfter <= 0 {
			retryAfter = time.Second
		}
		l.memEvents[memKey] = valid
		return false, retryAfter, nil
	}

	valid = append(valid, now)
	l.memEvents[memKey] = valid
	return true, 0, nil
}

// AllowIP checks sliding window rate limit for the client IP extracted from the HTTP request.
func (l *Limiter) AllowIP(ctx context.Context, r *http.Request, route string, limit int, window time.Duration) (bool, time.Duration, error) {
	ip := l.ExtractClientIP(r)
	return l.AllowKey(ctx, ip, route, limit, window)
}

// Allow is a convenience method for simple in-memory IP checks.
func (l *Limiter) Allow(ip string) (bool, time.Duration) {
	allowed, retryAfter, _ := l.AllowKey(context.Background(), ip, "default", l.defaultLimit, l.defaultWindow)
	return allowed, retryAfter
}

// Prune removes expired events older than the cutoff timestamp.
func (l *Limiter) Prune(ctx context.Context, cutoff time.Time) error {
	if l.store != nil {
		return l.store.PruneEventsOlderThan(ctx, cutoff)
	}

	l.memMu.Lock()
	defer l.memMu.Unlock()

	for k, timestamps := range l.memEvents {
		var valid []time.Time
		for _, t := range timestamps {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(l.memEvents, k)
		} else {
			l.memEvents[k] = valid
		}
	}
	return nil
}

// RouteLimitOptions configures per-route middleware rate limiting.
type RouteLimitOptions struct {
	Route       string
	Limit       int
	Window      time.Duration
	KeyFunc     func(r *http.Request) string                                           // Optional: custom key extractor; if nil, extracts client IP
	OnRateLimit func(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) // Optional custom 429 response handler
}

// Middleware creates standard HTTP middleware that enforces sliding-window rate limits.
func (l *Limiter) Middleware(opts RouteLimitOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := opts.Route
			if route == "" {
				route = r.URL.Path
			}

			var (
				allowed    bool
				retryAfter time.Duration
				err        error
			)

			if opts.KeyFunc != nil {
				key := opts.KeyFunc(r)
				if key != "" {
					allowed, retryAfter, err = l.AllowKey(r.Context(), key, route, opts.Limit, opts.Window)
				} else {
					allowed, retryAfter, err = l.AllowIP(r.Context(), r, route, opts.Limit, opts.Window)
				}
			} else {
				allowed, retryAfter, err = l.AllowIP(r.Context(), r, route, opts.Limit, opts.Window)
			}

			if err != nil {
				// Fail-open on internal store error to keep service available
				next.ServeHTTP(w, r)
				return
			}

			if !allowed {
				retrySec := int(retryAfter.Seconds())
				if retrySec < 1 {
					retrySec = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retrySec))
				if opts.OnRateLimit != nil {
					opts.OnRateLimit(w, r, retryAfter)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error":               "rate limit exceeded, please try again later",
					"code":                http.StatusTooManyRequests,
					"retry_after_seconds": retrySec,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
