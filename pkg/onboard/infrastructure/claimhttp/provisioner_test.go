package claimhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/onboard/domain"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/claimhttp"
)

func TestHTTPProvisioner_Provision_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || (r.URL.Path != "/api/v1/dev/onboard/claim" && r.URL.Path != "/api/v1/onboard/claim") {
			t.Fatalf("unexpected method or path: %s %s", r.Method, r.URL.Path)
		}

		var req client.ClaimRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.ClaimResponse{
			Status:           "success",
			IdempotentReplay: false,
			User: client.User{
				UID:         req.DesiredUID,
				Email:       req.Email,
				DisplayName: req.DisplayName,
				Groups:      []string{"dev", "orbit"},
			},
			Credentials: client.Credentials{
				ForgejoUsername: req.DesiredUID,
				ForgejoMCPToken: "fjo_tok_987654321", // gitleaks:allow
				WireGuardConfig: "[Interface]\nAddress = 10.8.0.5/24",
			},
			Workspace: client.WorkspaceInfo{
				GitRemoteBase:        "ssh://git@git.internal.manova.space/manova",
				DefaultManifestScope: "core",
			},
		})
	}))
	defer server.Close()

	prov := claimhttp.NewHTTPProvisionerFromURL(server.URL)
	if prov.Client() == nil {
		t.Fatal("expected non-nil Client()")
	}

	req := domain.ClaimRequest{
		InviteToken:        "manova-inv.mocktoken.sig",
		DesiredUID:         "alice",
		Email:              "alice@manova.space",
		DisplayName:        "Alice Developer",
		SSHPublicKey:       "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA...",
		MachineFingerprint: "fingerprint-123",
		Scope:              "core",
	}

	resp, err := prov.Provision(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected provision error: %v", err)
	}

	if resp.User.UID != "alice" {
		t.Fatalf("expected alice, got %s", resp.User.UID)
	}
	if resp.User.Email != "alice@manova.space" {
		t.Fatalf("expected alice@manova.space, got %s", resp.User.Email)
	}
	if resp.Credentials.ForgejoMCPToken != "fjo_tok_987654321" {
		t.Fatalf("expected token fjo_tok_987654321, got %s", resp.Credentials.ForgejoMCPToken)
	}
	if resp.Workspace.GitRemoteBase != "ssh://git@git.internal.manova.space/manova" {
		t.Fatalf("unexpected git remote base: %s", resp.Workspace.GitRemoteBase)
	}
}

func TestHTTPProvisioner_Provision_ErrorMappings(t *testing.T) {
	t.Run("409 maps to domain.ErrUserAlreadyExists", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "user alice already provisioned",
				"code":  409,
			})
		}))
		defer server.Close()

		prov := claimhttp.NewHTTPProvisioner(client.NewClient(server.URL))
		_, err := prov.Provision(context.Background(), domain.ClaimRequest{InviteToken: "tok", DesiredUID: "alice"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, domain.ErrUserAlreadyExists) {
			t.Fatalf("expected domain.ErrUserAlreadyExists, got %v", err)
		}
	})

	t.Run("400 maps to domain.ErrInvalidRequest", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid machine fingerprint",
				"code":  400,
			})
		}))
		defer server.Close()

		prov := claimhttp.NewHTTPProvisioner(client.NewClient(server.URL))
		_, err := prov.Provision(context.Background(), domain.ClaimRequest{InviteToken: "tok"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, domain.ErrInvalidRequest) {
			t.Fatalf("expected domain.ErrInvalidRequest, got %v", err)
		}
	})

	t.Run("503 maps to domain.ErrServiceDegraded", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "directory service unreachable",
				"code":  503,
			})
		}))
		defer server.Close()

		prov := claimhttp.NewHTTPProvisioner(client.NewClient(server.URL, client.WithRetry(0, 0)))
		_, err := prov.Provision(context.Background(), domain.ClaimRequest{InviteToken: "tok"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, domain.ErrServiceDegraded) {
			t.Fatalf("expected domain.ErrServiceDegraded, got %v", err)
		}
	})

	t.Run("500 maps to domain.ErrProvisionFailed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "internal error during ssh key injection",
				"code":  500,
			})
		}))
		defer server.Close()

		prov := claimhttp.NewHTTPProvisioner(client.NewClient(server.URL, client.WithRetry(0, 0)))
		_, err := prov.Provision(context.Background(), domain.ClaimRequest{InviteToken: "tok"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, domain.ErrProvisionFailed) {
			t.Fatalf("expected domain.ErrProvisionFailed, got %v", err)
		}
	})
}

func TestHTTPProvisioner_Rollback(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || (r.URL.Path != "/api/v1/dev/onboard/rollback" && r.URL.Path != "/api/v1/onboard/rollback") {
				t.Fatalf("unexpected method or path: %s %s", r.Method, r.URL.Path)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.RollbackResponse{
				Status: "success",
				UID:    "alice",
			})
		}))
		defer server.Close()

		prov := claimhttp.NewHTTPProvisionerFromURL(server.URL)
		if err := prov.Rollback(context.Background(), "alice"); err != nil {
			t.Fatalf("unexpected rollback error: %v", err)
		}
	})

	t.Run("empty UID", func(t *testing.T) {
		prov := claimhttp.NewHTTPProvisionerFromURL("http://localhost:8080")
		err := prov.Rollback(context.Background(), "   ")
		if err == nil {
			t.Fatal("expected error for empty UID")
		}
		if !errors.Is(err, domain.ErrInvalidRequest) {
			t.Fatalf("expected domain.ErrInvalidRequest, got %v", err)
		}
	})

	t.Run("server error maps to domain.ErrRollbackFailed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "database transaction failed",
				"code":  500,
			})
		}))
		defer server.Close()

		prov := claimhttp.NewHTTPProvisionerFromURL(server.URL, client.WithRetry(0, 0))
		err := prov.Rollback(context.Background(), "alice")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, domain.ErrRollbackFailed) {
			t.Fatalf("expected domain.ErrRollbackFailed, got %v", err)
		}
	})
}

func TestHTTPProvisioner_Health(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.HealthResponse{
				Status: "ok",
			})
		}))
		defer server.Close()

		prov := claimhttp.NewHTTPProvisionerFromURL(server.URL)
		if err := prov.Health(context.Background()); err != nil {
			t.Fatalf("unexpected health error: %v", err)
		}
	})

	t.Run("degraded maps to domain.ErrServiceDegraded", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "vpn tunnel down",
				"code":  503,
			})
		}))
		defer server.Close()

		prov := claimhttp.NewHTTPProvisionerFromURL(server.URL, client.WithRetry(0, 0))
		err := prov.Health(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, domain.ErrServiceDegraded) {
			t.Fatalf("expected domain.ErrServiceDegraded, got %v", err)
		}
	})
}

func TestHTTPProvisioner_NilClient(t *testing.T) {
	prov := &claimhttp.HTTPProvisioner{}

	if _, err := prov.Provision(context.Background(), domain.ClaimRequest{}); err == nil {
		t.Fatal("expected error on Provision with nil client")
	} else if !errors.Is(err, domain.ErrProvisionFailed) {
		t.Fatalf("expected domain.ErrProvisionFailed, got %v", err)
	}

	if err := prov.Rollback(context.Background(), "user"); err == nil {
		t.Fatal("expected error on Rollback with nil client")
	} else if !errors.Is(err, domain.ErrRollbackFailed) {
		t.Fatalf("expected domain.ErrRollbackFailed, got %v", err)
	}

	if err := prov.Health(context.Background()); err == nil {
		t.Fatal("expected error on Health with nil client")
	} else if !errors.Is(err, domain.ErrServiceDegraded) {
		t.Fatalf("expected domain.ErrServiceDegraded, got %v", err)
	}
}
