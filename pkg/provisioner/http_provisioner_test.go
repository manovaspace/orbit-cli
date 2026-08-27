package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/client"
)

func TestHTTPProvisioner_Provision_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/onboard/claim" {
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
				ForgejoMCPToken: "fjo_tok_987654321",
				WireGuardConfig: "[Interface]\nAddress = 10.8.0.5/24",
			},
			Workspace: client.WorkspaceInfo{
				GitRemoteBase:        "ssh://git@git.dev.manova.space/manova",
				DefaultManifestScope: "core",
			},
		})
	}))
	defer server.Close()

	prov := NewHTTPProvisionerFromURL(server.URL)
	if prov.Client() == nil {
		t.Fatal("expected non-nil Client()")
	}

	req := ClaimRequest{
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
	if resp.Workspace.GitRemoteBase != "ssh://git@git.dev.manova.space/manova" {
		t.Fatalf("unexpected git remote base: %s", resp.Workspace.GitRemoteBase)
	}
}

func TestHTTPProvisioner_Provision_ErrorMappings(t *testing.T) {
	t.Run("409 maps to ErrUserAlreadyExists", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "user alice already provisioned",
				"code":  409,
			})
		}))
		defer server.Close()

		prov := NewHTTPProvisioner(client.NewClient(server.URL))
		_, err := prov.Provision(context.Background(), ClaimRequest{InviteToken: "tok", DesiredUID: "alice"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrUserAlreadyExists) {
			t.Fatalf("expected ErrUserAlreadyExists, got %v", err)
		}
	})

	t.Run("400 maps to ErrInvalidRequest", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid machine fingerprint",
				"code":  400,
			})
		}))
		defer server.Close()

		prov := NewHTTPProvisioner(client.NewClient(server.URL))
		_, err := prov.Provision(context.Background(), ClaimRequest{InviteToken: "tok"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest, got %v", err)
		}
	})

	t.Run("503 maps to ErrServiceDegraded", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "directory service unreachable",
				"code":  503,
			})
		}))
		defer server.Close()

		prov := NewHTTPProvisioner(client.NewClient(server.URL, client.WithRetry(0, 0)))
		_, err := prov.Provision(context.Background(), ClaimRequest{InviteToken: "tok"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrServiceDegraded) {
			t.Fatalf("expected ErrServiceDegraded, got %v", err)
		}
	})

	t.Run("500 maps to ErrProvisionFailed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "internal error during ssh key injection",
				"code":  500,
			})
		}))
		defer server.Close()

		prov := NewHTTPProvisioner(client.NewClient(server.URL, client.WithRetry(0, 0)))
		_, err := prov.Provision(context.Background(), ClaimRequest{InviteToken: "tok"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrProvisionFailed) {
			t.Fatalf("expected ErrProvisionFailed, got %v", err)
		}
	})
}

func TestHTTPProvisioner_Rollback(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/onboard/rollback" {
				t.Fatalf("unexpected method or path: %s %s", r.Method, r.URL.Path)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.RollbackResponse{
				Status: "success",
				UID:    "alice",
			})
		}))
		defer server.Close()

		prov := NewHTTPProvisionerFromURL(server.URL)
		if err := prov.Rollback(context.Background(), "alice"); err != nil {
			t.Fatalf("unexpected rollback error: %v", err)
		}
	})

	t.Run("empty UID", func(t *testing.T) {
		prov := NewHTTPProvisionerFromURL("http://localhost:8080")
		err := prov.Rollback(context.Background(), "   ")
		if err == nil {
			t.Fatal("expected error for empty UID")
		}
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest, got %v", err)
		}
	})

	t.Run("server error maps to ErrRollbackFailed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "database transaction failed",
				"code":  500,
			})
		}))
		defer server.Close()

		prov := NewHTTPProvisionerFromURL(server.URL, client.WithRetry(0, 0))
		err := prov.Rollback(context.Background(), "alice")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrRollbackFailed) {
			t.Fatalf("expected ErrRollbackFailed, got %v", err)
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

		prov := NewHTTPProvisionerFromURL(server.URL)
		if err := prov.Health(context.Background()); err != nil {
			t.Fatalf("unexpected health error: %v", err)
		}
	})

	t.Run("degraded maps to ErrServiceDegraded", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "vpn tunnel down",
				"code":  503,
			})
		}))
		defer server.Close()

		prov := NewHTTPProvisionerFromURL(server.URL, client.WithRetry(0, 0))
		err := prov.Health(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrServiceDegraded) {
			t.Fatalf("expected ErrServiceDegraded, got %v", err)
		}
	})
}

func TestHTTPProvisioner_NilClient(t *testing.T) {
	prov := &HTTPProvisioner{client: nil}

	if _, err := prov.Provision(context.Background(), ClaimRequest{}); err == nil {
		t.Fatal("expected error on Provision with nil client")
	} else if !errors.Is(err, ErrProvisionFailed) {
		t.Fatalf("expected ErrProvisionFailed, got %v", err)
	}

	if err := prov.Rollback(context.Background(), "user"); err == nil {
		t.Fatal("expected error on Rollback with nil client")
	} else if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("expected ErrRollbackFailed, got %v", err)
	}

	if err := prov.Health(context.Background()); err == nil {
		t.Fatal("expected error on Health with nil client")
	} else if !errors.Is(err, ErrServiceDegraded) {
		t.Fatalf("expected ErrServiceDegraded, got %v", err)
	}
}
