package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClient_OptionsAndDefaults(t *testing.T) {
	// Default client
	c1 := NewClient("")
	if c1.BaseURL() != "http://localhost:8080" {
		t.Fatalf("expected default baseURL http://localhost:8080, got %s", c1.BaseURL())
	}

	// Custom URL without scheme and trailing slash
	c2 := NewClient("api.orbit.dev:9000/")
	if c2.BaseURL() != "http://api.orbit.dev:9000" {
		t.Fatalf("expected baseURL http://api.orbit.dev:9000, got %s", c2.BaseURL())
	}

	// Options
	customHTTP := &http.Client{Timeout: 5 * time.Second}
	c3 := NewClient("https://remote.dev:8443",
		WithHTTPClient(customHTTP),
		WithTimeout(3*time.Second),
		WithHeader("X-Custom", "value123"),
		WithBearerToken("tok-xyz"),
		WithRetry(5, 50*time.Millisecond),
	)

	if c3.BaseURL() != "https://remote.dev:8443" {
		t.Fatalf("expected https://remote.dev:8443, got %s", c3.BaseURL())
	}
	if c3.httpClient.Timeout != 3*time.Second {
		t.Fatalf("expected timeout 3s, got %v", c3.httpClient.Timeout)
	}
	if c3.headers["X-Custom"] != "value123" {
		t.Fatalf("expected header X-Custom=value123, got %s", c3.headers["X-Custom"])
	}
	if c3.headers["Authorization"] != "Bearer tok-xyz" {
		t.Fatalf("expected Bearer tok-xyz, got %s", c3.headers["Authorization"])
	}
	if c3.maxRetries != 5 {
		t.Fatalf("expected maxRetries 5, got %d", c3.maxRetries)
	}
	if c3.retryBackoff != 50*time.Millisecond {
		t.Fatalf("expected backoff 50ms, got %v", c3.retryBackoff)
	}

	// SetBaseURL
	c3.SetBaseURL("new-host:8081/")
	if c3.BaseURL() != "http://new-host:8081" {
		t.Fatalf("expected http://new-host:8081, got %s", c3.BaseURL())
	}
}

func TestClient_InitiateOwnerChallenge(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/api/v1/system/ownership/challenge" && r.URL.Path != "/api/v1/admin/challenge" {
				t.Fatalf("expected challenge path, got %s", r.URL.Path)
			}

			var req ChallengeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if req.Email != "admin@manova.space" {
				t.Fatalf("expected email admin@manova.space, got %s", req.Email)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ChallengeResponse{
				Status:    "challenge_sent",
				Email:     req.Email,
				ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
				Message:   "OTP challenge sent to email",
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		resp, err := cli.InitiateOwnerChallenge(context.Background(), "admin@manova.space")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Status != "challenge_sent" || resp.Email != "admin@manova.space" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("empty email", func(t *testing.T) {
		cli := NewClient("http://localhost:8080")
		_, err := cli.InitiateOwnerChallenge(context.Background(), "  ")
		if err == nil {
			t.Fatal("expected error for empty email, got nil")
		}
	})

	t.Run("server error 400", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid email format",
				"code":  400,
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		_, err := cli.InitiateOwnerChallenge(context.Background(), "bad-email")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *APIError, got %T: %v", err, err)
		}
		if apiErr.StatusCode != 400 || apiErr.Message != "invalid email format" {
			t.Fatalf("unexpected API error: %+v", apiErr)
		}
	})
}

func TestClient_VerifyOwnerChallenge(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || (r.URL.Path != "/api/v1/system/ownership/verify" && r.URL.Path != "/api/v1/admin/verify") {
				t.Fatalf("unexpected method or path: %s %s", r.Method, r.URL.Path)
			}

			var req VerifyRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if req.Email != "admin@manova.space" || req.Code != "123456" {
				t.Fatalf("unexpected req: %+v", req)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(VerifyResponse{
				Status:         "verified",
				Email:          req.Email,
				KeyFingerprint: "sha256:abcd1234",
				VerifiedAt:     time.Now().UTC(),
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		resp, err := cli.VerifyOwnerChallenge(context.Background(), "admin@manova.space", "123456")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Status != "verified" || resp.KeyFingerprint != "sha256:abcd1234" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("validation errors", func(t *testing.T) {
		cli := NewClient("http://localhost:8080")
		if _, err := cli.VerifyOwnerChallenge(context.Background(), "", "123456"); err == nil {
			t.Fatal("expected error for empty email")
		}
		if _, err := cli.VerifyOwnerChallenge(context.Background(), "admin@manova.space", ""); err == nil {
			t.Fatal("expected error for empty code")
		}
	})

	t.Run("invalid code 401", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid verification code",
				"code":  401,
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		_, err := cli.VerifyOwnerChallenge(context.Background(), "admin@manova.space", "000000")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
			t.Fatalf("expected 401 APIError, got %v", err)
		}
	})
}

func TestClient_AdminStatus_And_RotateSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if (r.URL.Path == "/api/v1/system/ownership/status" || r.URL.Path == "/api/v1/admin/status") && r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(AdminStatusResponse{
				Verified:         true,
				Email:            "owner@manova.space",
				KeyFingerprint:   "sha256:xyz",
				PermissionsValid: true,
			})
			return
		}

		if (r.URL.Path == "/api/v1/system/ownership/rotate-secret" || r.URL.Path == "/api/v1/admin/rotate-secret") && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(RotateSecretResponse{
				Status:         "success",
				Email:          "owner@manova.space",
				KeyFingerprint: "sha256:newfingerprint",
				RotatedAt:      time.Now().UTC(),
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cli := NewClient(server.URL)
	status, err := cli.AdminStatus(context.Background())
	if err != nil {
		t.Fatalf("AdminStatus failed: %v", err)
	}
	if !status.Verified || status.Email != "owner@manova.space" {
		t.Fatalf("unexpected status: %+v", status)
	}

	rot, err := cli.RotateSecret(context.Background())
	if err != nil {
		t.Fatalf("RotateSecret failed: %v", err)
	}
	if rot.KeyFingerprint != "sha256:newfingerprint" {
		t.Fatalf("unexpected rotate response: %+v", rot)
	}
}

func TestClient_ClaimToken(t *testing.T) {
	t.Run("success standard path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || (r.URL.Path != "/api/v1/dev/onboard/claim" && r.URL.Path != "/api/v1/onboard/claim") {
				t.Fatalf("unexpected method or path: %s %s", r.Method, r.URL.Path)
			}

			idempKey := r.Header.Get("Idempotency-Key")
			if idempKey == "" {
				t.Fatal("expected Idempotency-Key header")
			}

			var req ClaimRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if req.InviteToken != "manova-inv.test.sig" {
				t.Fatalf("unexpected token: %s", req.InviteToken)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ClaimResponse{
				Status: "success",
				User: User{
					UID:         "dev-user",
					Email:       "dev@manova.space",
					DisplayName: "Dev User",
					Groups:      []string{"dev"},
				},
				Credentials: Credentials{
					ForgejoUsername: "dev-user",
					ForgejoMCPToken: "fjo_tok_123",
					WireGuardConfig: "[Interface]\nAddress = 10.8.0.2/24",
				},
				Workspace: WorkspaceInfo{
					GitRemoteBase:        "ssh://git@git.dev.manova.space/manova",
					DefaultManifestScope: "core",
				},
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		resp, err := cli.ClaimToken(context.Background(), ClaimRequest{
			InviteToken:        "manova-inv.test.sig",
			DesiredUID:         "dev-user",
			MachineFingerprint: "mach-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.User.UID != "dev-user" || resp.Credentials.ForgejoMCPToken != "fjo_tok_123" {
			t.Fatalf("unexpected claim response: %+v", resp)
		}
	})

	t.Run("fallback to /v1/onboard/claim", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/onboard/claim" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.URL.Path == "/v1/onboard/claim" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(ClaimResponse{
					Status: "success",
					User: User{
						UID: "fallback-user",
					},
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		resp, err := cli.ClaimToken(context.Background(), ClaimRequest{
			InviteToken: "manova-inv.test.sig",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.User.UID != "fallback-user" {
			t.Fatalf("expected fallback-user, got %s", resp.User.UID)
		}
	})

	t.Run("conflict error 409", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "user already exists",
				"code":  409,
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		_, err := cli.ClaimToken(context.Background(), ClaimRequest{
			InviteToken: "manova-inv.test.sig",
			DesiredUID:  "existing-user",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 409 {
			t.Fatalf("expected 409 APIError, got %v", err)
		}
	})

	t.Run("empty invite token", func(t *testing.T) {
		cli := NewClient("http://localhost:8080")
		_, err := cli.ClaimToken(context.Background(), ClaimRequest{})
		if err == nil {
			t.Fatal("expected error for empty invite token")
		}
	})
}

func TestClient_Rollback(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || (r.URL.Path != "/api/v1/dev/onboard/rollback" && r.URL.Path != "/api/v1/onboard/rollback") {
				t.Fatalf("unexpected method/path: %s %s", r.Method, r.URL.Path)
			}

			var req RollbackRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode req: %v", err)
			}
			if req.UID != "dev-user" {
				t.Fatalf("expected dev-user, got %s", req.UID)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RollbackResponse{
				Status: "success",
				UID:    req.UID,
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		if err := cli.Rollback(context.Background(), "dev-user"); err != nil {
			t.Fatalf("unexpected rollback error: %v", err)
		}
	})

	t.Run("empty uid", func(t *testing.T) {
		cli := NewClient("http://localhost:8080")
		if err := cli.Rollback(context.Background(), "   "); err == nil {
			t.Fatal("expected error for empty uid")
		}
	})
}

func TestClient_Health(t *testing.T) {
	t.Run("success /healthz", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(HealthResponse{
				Status:      "ok",
				Provisioner: "healthy",
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		if err := cli.Health(context.Background()); err != nil {
			t.Fatalf("unexpected health error: %v", err)
		}
	})

	t.Run("fallback /health", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(HealthResponse{
					Status: "ok",
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		cli := NewClient(server.URL)
		if err := cli.Health(context.Background()); err != nil {
			t.Fatalf("unexpected health error: %v", err)
		}
	})

	t.Run("degraded status code 503", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "database connection lost",
				"code":  503,
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL, WithRetry(0, 0))
		err := cli.Health(context.Background())
		if err == nil {
			t.Fatal("expected error for degraded health")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 {
			t.Fatalf("expected 503 APIError, got %v", err)
		}
	})
}

func TestClient_TimeoutHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cli := NewClient(server.URL, WithTimeout(50*time.Millisecond), WithRetry(0, 0))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := cli.Health(ctx)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// http.Client timeout also produces error with context deadline exceeded or timeout
		t.Logf("got expected timeout error: %v", err)
	}
}

func TestClient_RetryBehavior(t *testing.T) {
	t.Run("transient 503 retries and succeeds", func(t *testing.T) {
		var attempts int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cur := atomic.AddInt32(&attempts, 1)
			if cur < 3 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "service unavailable",
					"code":  503,
				})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(HealthResponse{
				Status: "ok",
			})
		}))
		defer server.Close()

		cli := NewClient(server.URL, WithRetry(3, 10*time.Millisecond))
		if err := cli.Health(context.Background()); err != nil {
			t.Fatalf("expected success after retries, got: %v", err)
		}

		if atomic.LoadInt32(&attempts) != 3 {
			t.Fatalf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
		}
	})

	t.Run("retry with Retry-After header", func(t *testing.T) {
		var attempts int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cur := atomic.AddInt32(&attempts, 1)
			if cur == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "rate limit exceeded",
					"code":  429,
				})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
		}))
		defer server.Close()

		cli := NewClient(server.URL, WithRetry(2, 5*time.Millisecond))
		if err := cli.Health(context.Background()); err != nil {
			t.Fatalf("expected success after retry, got: %v", err)
		}
	})

	t.Run("canceled context aborts retry", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		cli := NewClient(server.URL, WithRetry(5, 50*time.Millisecond))
		err := cli.Health(ctx)
		if err == nil {
			t.Fatal("expected error on cancelled context")
		}
		if !errors.Is(err, context.Canceled) {
			t.Logf("got error: %v", err)
		}
	})
}

func TestClient_ErrorDecoding(t *testing.T) {
	t.Run("structured JSON error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"malformed JSON request","code":400,"retry_after_seconds":10}`))
		}))
		defer server.Close()

		cli := NewClient(server.URL, WithRetry(0, 0))
		err := cli.Health(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *APIError, got %T: %v", err, err)
		}
		if apiErr.StatusCode != 400 || apiErr.Message != "malformed JSON request" || apiErr.RetryAfterSeconds != 10 {
			t.Fatalf("unexpected API error parsed: %+v", apiErr)
		}
		if apiErr.Error() != "api error (400): malformed JSON request" {
			t.Fatalf("unexpected Error() format: %s", apiErr.Error())
		}
	})

	t.Run("plain text error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal server failure details"))
		}))
		defer server.Close()

		cli := NewClient(server.URL, WithRetry(0, 0))
		err := cli.Health(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *APIError, got %T: %v", err, err)
		}
		if apiErr.StatusCode != 500 || apiErr.Message != "Internal server failure details" {
			t.Fatalf("unexpected API error: %+v", apiErr)
		}
	})
}
