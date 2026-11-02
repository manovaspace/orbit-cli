package claimhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/claimhttp"
)

func TestClaimPostsIdempotentRequest(t *testing.T) {
	var gotPath, gotContentType, gotKey string
	var gotBody domain.ClaimRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.Method + " " + r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotKey = r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(domain.ClaimResponse{
			Status:           "success",
			IdempotentReplay: true,
			User:             domain.User{UID: "dev", Email: "dev@manova.space"},
			Credentials:      domain.Credentials{ForgejoMCPToken: "fjo_tok_x"},
		})
	}))
	defer server.Close()

	resp, err := claimhttp.New().Claim(context.Background(), server.URL+"/", "idem-key", domain.ClaimRequest{
		InviteToken:        "token-value",
		DesiredUID:         "dev",
		Email:              "dev@manova.space",
		MachineFingerprint: "host-linux-amd64",
	})
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	if gotPath != "POST /api/v1/onboard/claim" {
		t.Errorf("request = %q", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("content type = %q", gotContentType)
	}
	if gotKey != "idem-key" {
		t.Errorf("idempotency key = %q", gotKey)
	}
	if gotBody.InviteToken != "token-value" || gotBody.MachineFingerprint != "host-linux-amd64" {
		t.Errorf("unexpected body: %+v", gotBody)
	}
	if resp.User.UID != "dev" || !resp.IdempotentReplay {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClaimMapsHTTPFailuresToExitCodes(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{status: http.StatusUnauthorized, code: clierr.CodeAuth},
		{status: http.StatusForbidden, code: clierr.CodeAuth},
		{status: http.StatusBadRequest, code: clierr.CodeUsage},
		{status: http.StatusConflict, code: clierr.CodeFailed},
		{status: http.StatusBadGateway, code: clierr.CodeNetwork},
	}

	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rejected"})
			}))
			defer server.Close()

			_, err := claimhttp.New().Claim(context.Background(), server.URL, "", domain.ClaimRequest{InviteToken: "x"})
			if err == nil {
				t.Fatal("expected an error")
			}
			if code := clierr.Code(err); code != tc.code {
				t.Errorf("code = %q, want %q (%v)", code, tc.code, err)
			}
		})
	}
}

func TestClaimReportsUnreachableEdgeAsNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	_, err := claimhttp.New().Claim(context.Background(), url, "", domain.ClaimRequest{InviteToken: "x"})
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if code := clierr.Code(err); code != clierr.CodeNetwork {
		t.Errorf("code = %q, want network (%v)", code, err)
	}
}

func TestClaimParsesResponseErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	_, err := claimhttp.New().Claim(context.Background(), server.URL, "", domain.ClaimRequest{InviteToken: "x"})
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if code := clierr.Code(err); code != clierr.CodeFailed {
		t.Errorf("code = %q, want failed", code)
	}
}

func TestRollbackSendsDeleteForUID(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := claimhttp.New().Rollback(context.Background(), server.URL+"/", "dev-user"); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/v1/onboard/rollback/dev-user" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestRollbackReportsRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if err := claimhttp.New().Rollback(context.Background(), server.URL, "dev-user"); err == nil {
		t.Fatal("expected the rollback rejection")
	}
}

func TestClientUsesConfiguredHTTPClient(t *testing.T) {
	called := false
	client := claimhttp.New()
	client.HTTP = &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, context.DeadlineExceeded
	})}

	_, err := client.Claim(context.Background(), "http://edge", "", domain.ClaimRequest{InviteToken: "x"})
	if err == nil {
		t.Fatal("expected the transport failure")
	}
	if !called {
		t.Error("the injected HTTP client was not used")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
