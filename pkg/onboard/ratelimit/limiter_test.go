package ratelimit_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/onboard/ratelimit"
	"github.com/manovaspace/orbit-cli/pkg/serverstore/sqlite"
)

func TestLimiter_ProxySpoofing(t *testing.T) {
	l := ratelimit.NewLimiter(nil, ratelimit.LimiterOptions{
		TrustedProxies: []string{"127.0.0.1/32"},
		DefaultLimit:   10,
		DefaultWindow:  time.Minute,
	})

	// 1. Untrusted public remote IP attempting to spoof X-Forwarded-For and CF-Connecting-IP
	req := httptest.NewRequest("POST", "/api/v1/onboard/claim", nil)
	req.RemoteAddr = "198.51.100.1:54321" // Public IP
	req.Header.Set("X-Forwarded-For", "127.0.0.1, 10.0.0.5")
	req.Header.Set("CF-Connecting-IP", "1.1.1.1")
	req.Header.Set("X-Real-IP", "2.2.2.2")

	ip := l.ExtractClientIP(req)
	if ip != "198.51.100.1" {
		t.Fatalf("expected remote addr to be used when untrusted proxy, got: %s", ip)
	}
}

func TestLimiter_TrustedProxy_CFConnectingIP(t *testing.T) {
	l := ratelimit.NewLimiter(nil, ratelimit.LimiterOptions{
		TrustedProxies: []string{"127.0.0.1/32", "10.0.0.0/8"},
	})

	// Trusted proxy forwarding Cloudflare header
	req := httptest.NewRequest("POST", "/api/v1/onboard/claim", nil)
	req.RemoteAddr = "127.0.0.1:41234"
	req.Header.Set("CF-Connecting-IP", "203.0.113.88")
	req.Header.Set("X-Forwarded-For", "203.0.113.88, 10.0.0.2")

	ip := l.ExtractClientIP(req)
	if ip != "203.0.113.88" {
		t.Fatalf("expected CF-Connecting-IP 203.0.113.88, got: %s", ip)
	}
}

func TestLimiter_TrustedProxy_XForwardedFor_Chain(t *testing.T) {
	l := ratelimit.NewLimiter(nil, ratelimit.LimiterOptions{
		TrustedProxies: []string{"127.0.0.1/32", "10.0.0.0/8", "172.16.0.0/12"},
	})

	// RemoteAddr is 127.0.0.1 (trusted)
	// XFF: 203.0.113.99 (untrusted client), 172.16.5.10 (trusted intermediate proxy), 10.0.0.1 (trusted edge)
	req := httptest.NewRequest("POST", "/api/v1/admin/challenge", nil)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("X-Forwarded-For", "203.0.113.99, 172.16.5.10, 10.0.0.1")

	ip := l.ExtractClientIP(req)
	if ip != "203.0.113.99" {
		t.Fatalf("expected rightmost untrusted IP 203.0.113.99, got: %s", ip)
	}
}

func TestLimiter_TrustedProxy_AllInternal(t *testing.T) {
	l := ratelimit.NewLimiter(nil, ratelimit.LimiterOptions{
		TrustedProxies: []string{"127.0.0.1/32", "10.0.0.0/8"},
	})

	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("X-Forwarded-For", "10.0.0.5, 10.0.0.6")

	ip := l.ExtractClientIP(req)
	if ip != "10.0.0.5" {
		t.Fatalf("expected leftmost IP 10.0.0.5 when all proxies trusted, got: %s", ip)
	}
}

func TestLimiter_AllowIP_InMemory(t *testing.T) {
	l := ratelimit.NewLimiter(nil, ratelimit.LimiterOptions{
		DefaultLimit:  2,
		DefaultWindow: 500 * time.Millisecond,
	})

	req := httptest.NewRequest("POST", "/claim", nil)
	req.RemoteAddr = "192.0.2.1:1234"

	ctx := context.Background()

	// 1st request -> allowed
	allowed, _, err := l.AllowIP(ctx, req, "/claim", 2, 500*time.Millisecond)
	if err != nil || !allowed {
		t.Fatalf("1st request should be allowed, got allowed=%v, err=%v", allowed, err)
	}

	// 2nd request -> allowed
	allowed, _, err = l.AllowIP(ctx, req, "/claim", 2, 500*time.Millisecond)
	if err != nil || !allowed {
		t.Fatalf("2nd request should be allowed, got allowed=%v, err=%v", allowed, err)
	}

	// 3rd request -> rejected
	allowed, retryAfter, err := l.AllowIP(ctx, req, "/claim", 2, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("3rd request error: %v", err)
	}
	if allowed {
		t.Fatalf("3rd request should be rejected (rate limited)")
	}
	if retryAfter <= 0 {
		t.Errorf("expected positive retryAfter, got %v", retryAfter)
	}
}

func TestLimiter_AllowKey_SQLiteStore(t *testing.T) {
	db := sqlite.NewTestDB(t)
	l := ratelimit.NewLimiter(db.RateLimits(), ratelimit.LimiterOptions{})
	ctx := context.Background()

	email := "admin@manova.space"
	route := "/api/v1/admin/challenge"

	// 1st request -> allowed
	allowed, _, err := l.AllowKey(ctx, email, route, 2, time.Minute)
	if err != nil || !allowed {
		t.Fatalf("1st request should be allowed: %v", err)
	}

	// 2nd request -> allowed
	allowed, _, err = l.AllowKey(ctx, email, route, 2, time.Minute)
	if err != nil || !allowed {
		t.Fatalf("2nd request should be allowed: %v", err)
	}

	// 3rd request -> rejected
	allowed, retryAfter, err := l.AllowKey(ctx, email, route, 2, time.Minute)
	if err != nil {
		t.Fatalf("3rd request error: %v", err)
	}
	if allowed {
		t.Fatalf("expected 3rd request to be rate limited")
	}
	if retryAfter <= 0 {
		t.Errorf("expected positive retryAfter, got %v", retryAfter)
	}

	// Different key should still be allowed
	allowedOther, _, err := l.AllowKey(ctx, "other@manova.space", route, 2, time.Minute)
	if err != nil || !allowedOther {
		t.Fatalf("different key should be allowed: %v", err)
	}
}

func TestLimiter_Middleware_Enforcement(t *testing.T) {
	l := ratelimit.NewLimiter(nil, ratelimit.LimiterOptions{
		TrustedProxies: []string{"127.0.0.1/32"},
	})

	handler := l.Middleware(ratelimit.RouteLimitOptions{
		Route:  "/test-route",
		Limit:  2,
		Window: time.Minute,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req1 := httptest.NewRequest("GET", "/test-route", nil)
	req1.RemoteAddr = "192.0.2.55:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("req1 expected 200, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest("GET", "/test-route", nil)
	req2.RemoteAddr = "192.0.2.55:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("req2 expected 200, got %d", rec2.Code)
	}

	req3 := httptest.NewRequest("GET", "/test-route", nil)
	req3.RemoteAddr = "192.0.2.55:1234"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("req3 expected 429 Too Many Requests, got %d", rec3.Code)
	}

	retryAfterHeader := rec3.Header().Get("Retry-After")
	if retryAfterHeader == "" {
		t.Errorf("expected Retry-After header to be set on 429 response")
	}

	var errBody map[string]interface{}
	if err := json.NewDecoder(rec3.Body).Decode(&errBody); err != nil {
		t.Fatalf("failed to decode 429 json body: %v", err)
	}
	if errBody["code"] != float64(429) {
		t.Errorf("expected code 429 in json body, got %+v", errBody)
	}
}

func TestLimiter_Prune(t *testing.T) {
	db := sqlite.NewTestDB(t)
	l := ratelimit.NewLimiter(db.RateLimits(), ratelimit.LimiterOptions{})
	ctx := context.Background()

	_, _, _ = l.AllowKey(ctx, "user1", "/route", 10, time.Minute)

	cutoff := time.Now().UTC().Add(time.Minute)
	if err := l.Prune(ctx, cutoff); err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
}
