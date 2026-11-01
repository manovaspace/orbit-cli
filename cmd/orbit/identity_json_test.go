package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// identityRun executes the CLI and returns its combined output.
func identityRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	cmd := newRootCmdForTest(t)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func newStaffStub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/staff":
			_ = json.NewEncoder(w).Encode(map[string]any{"uid": "alice", "personal_forward": "alice@example.com"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/staff":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"uid": "alice", "status": "active"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/staff/alice":
			_ = json.NewEncoder(w).Encode(map[string]any{"uid": "alice", "status": "active"})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/staff/alice":
			_ = json.NewEncoder(w).Encode(map[string]any{"uid": "alice"})
		case r.URL.Path == "/api/v1/staff/alice/reset-password":
			_ = json.NewEncoder(w).Encode(map[string]any{"ldap_password": "rotated"})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
}

func newGrantStub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "granted",
			"email":      "sara@manova.space",
			"code":       "8492-0194",
			"expires_at": time.Now().Add(15 * time.Minute),
		})
	}))
}

func TestStaffCommandsJSON(t *testing.T) {
	srv := newStaffStub(t)
	defer srv.Close()
	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())

	base := []string{"--owner-store", storePath, "--server", srv.URL, "--json"}

	t.Run("list", func(t *testing.T) {
		out, err := identityRun(t, append([]string{"staff", "list"}, base...)...)
		if err != nil {
			t.Fatalf("staff list: %v\n%s", err, out)
		}
		env := decodeEnvelope(t, out)
		var members []map[string]any
		if err := json.Unmarshal(env.Data, &members); err != nil || len(members) != 1 {
			t.Fatalf("data = %s (err %v)", env.Data, err)
		}
	})

	t.Run("get", func(t *testing.T) {
		out, err := identityRun(t, append([]string{"staff", "get", "alice"}, base...)...)
		if err != nil {
			t.Fatalf("staff get: %v\n%s", err, out)
		}
		if !strings.Contains(string(decodeEnvelope(t, out).Data), `"uid":"alice"`) {
			t.Fatalf("unexpected data: %s", out)
		}
	})

	t.Run("create", func(t *testing.T) {
		out, err := identityRun(t, append([]string{"staff", "create", "--uid", "alice", "--forward", "alice@example.com"}, base...)...)
		if err != nil {
			t.Fatalf("staff create: %v\n%s", err, out)
		}
		if !strings.Contains(string(decodeEnvelope(t, out).Data), `"uid":"alice"`) {
			t.Fatalf("unexpected data: %s", out)
		}
	})

	t.Run("update", func(t *testing.T) {
		out, err := identityRun(t, append([]string{"staff", "update", "alice", "--name", "Alice"}, base...)...)
		if err != nil {
			t.Fatalf("staff update: %v\n%s", err, out)
		}
		if !strings.Contains(string(decodeEnvelope(t, out).Data), `"uid":"alice"`) {
			t.Fatalf("unexpected data: %s", out)
		}
	})

	for _, action := range []string{"disable", "enable", "delete"} {
		t.Run(action, func(t *testing.T) {
			out, err := identityRun(t, append([]string{"staff", action, "alice"}, base...)...)
			if err != nil {
				t.Fatalf("staff %s: %v\n%s", action, err, out)
			}
			data := string(decodeEnvelope(t, out).Data)
			wantStatus := map[string]string{"disable": "disabled", "enable": "enabled", "delete": "deleted"}[action]
			if !strings.Contains(data, `"uid":"alice"`) || !strings.Contains(data, `"status":"`+wantStatus+`"`) {
				t.Fatalf("unexpected data: %s", data)
			}
		})
	}

	t.Run("reset-password", func(t *testing.T) {
		out, err := identityRun(t, append([]string{"staff", "reset-password", "alice", "--ldap"}, base...)...)
		if err != nil {
			t.Fatalf("staff reset-password: %v\n%s", err, out)
		}
		if !strings.Contains(string(decodeEnvelope(t, out).Data), `"ldap_password":"rotated"`) {
			t.Fatalf("unexpected data: %s", out)
		}
	})
}

func TestStaffRecreateJSONFailure(t *testing.T) {
	out, err := identityRun(t, "staff", "recreate", "--uid", "alice", "--json")
	if err == nil {
		t.Fatal("expected recreate to be refused")
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "failed" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestInviteCommandsJSON(t *testing.T) {
	dir := t.TempDir()
	storePath, _ := createTestVerifiedOwnerStore(t, dir)
	inviteStore := filepath.Join(dir, "invites.json")

	t.Run("create", func(t *testing.T) {
		out, err := identityRun(t, "invite", "create", "dev@example.com",
			"--owner-store", storePath,
			"--store-file", inviteStore,
			"--no-send",
			"--json",
		)
		if err != nil {
			t.Fatalf("invite create: %v\n%s", err, out)
		}
		env := decodeEnvelope(t, out)
		var data struct {
			Token string `json:"token"`
			Email string `json:"email"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			t.Fatalf("data: %v", err)
		}
		if !strings.HasPrefix(data.Token, "orbit-inv.") || data.Email != "dev@example.com" {
			t.Fatalf("unexpected data: %+v", data)
		}
	})

	t.Run("list", func(t *testing.T) {
		out, err := identityRun(t, "invite", "list", "--store-file", inviteStore, "--json")
		if err != nil {
			t.Fatalf("invite list: %v\n%s", err, out)
		}
		var records []map[string]any
		if err := json.Unmarshal(decodeEnvelope(t, out).Data, &records); err != nil || len(records) != 1 {
			t.Fatalf("records = %v (err %v)", records, err)
		}
	})

	t.Run("revoke", func(t *testing.T) {
		out, err := identityRun(t, "invite", "revoke", "--all", "--store-file", inviteStore, "--json")
		if err != nil {
			t.Fatalf("invite revoke: %v\n%s", err, out)
		}
		var data struct {
			Revoked []map[string]any `json:"revoked"`
		}
		if err := json.Unmarshal(decodeEnvelope(t, out).Data, &data); err != nil || len(data.Revoked) != 1 {
			t.Fatalf("data = %+v (err %v)", data, err)
		}
	})
}

func TestAdminCommandsJSON(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "owner.json")

	t.Run("init", func(t *testing.T) {
		out, err := identityRun(t, "admin", "init", "--owner", "admin@example.com",
			"--no-send", "--code", "123456", "--store", storePath, "--json")
		if err != nil {
			t.Fatalf("admin init: %v\n%s", err, out)
		}
		var data map[string]any
		if err := json.Unmarshal(decodeEnvelope(t, out).Data, &data); err != nil {
			t.Fatalf("data: %v", err)
		}
		if data["verified"] != true || data["email"] != "admin@example.com" {
			t.Fatalf("unexpected data: %v", data)
		}
	})

	t.Run("status", func(t *testing.T) {
		out, err := identityRun(t, "admin", "status", "--store", storePath, "--json")
		if err != nil {
			t.Fatalf("admin status: %v\n%s", err, out)
		}
		var data map[string]any
		if err := json.Unmarshal(decodeEnvelope(t, out).Data, &data); err != nil {
			t.Fatalf("data: %v", err)
		}
		if data["verified"] != true || data["vault_permissions"] != "0600 (secure)" {
			t.Fatalf("unexpected data: %v", data)
		}
	})

	t.Run("verify", func(t *testing.T) {
		verifyStore := filepath.Join(t.TempDir(), "owner.json")
		out, err := identityRun(t, "admin", "verify", "owner@example.com", "654321", "--store", verifyStore, "--json")
		if err != nil {
			t.Fatalf("admin verify: %v\n%s", err, out)
		}
		if !strings.Contains(string(decodeEnvelope(t, out).Data), `"verified":true`) {
			t.Fatalf("unexpected data: %s", out)
		}
	})

	t.Run("rotate-secret", func(t *testing.T) {
		out, err := identityRun(t, "admin", "rotate-secret", "--store", storePath, "--yes", "--json")
		if err != nil {
			t.Fatalf("admin rotate-secret: %v\n%s", err, out)
		}
		if !strings.Contains(string(decodeEnvelope(t, out).Data), `"cancelled":false`) {
			t.Fatalf("unexpected data: %s", out)
		}
	})

	t.Run("grant", func(t *testing.T) {
		srv := newGrantStub(t)
		defer srv.Close()
		out, err := identityRun(t, "admin", "grant", "sara@manova.space",
			"--store", storePath, "--server", srv.URL, "--json")
		if err != nil {
			t.Fatalf("admin grant: %v\n%s", err, out)
		}
		var data map[string]any
		if err := json.Unmarshal(decodeEnvelope(t, out).Data, &data); err != nil {
			t.Fatalf("data: %v", err)
		}
		if data["email"] != "sara@manova.space" || data["code"] == nil {
			t.Fatalf("unexpected data: %v", data)
		}
	})

	t.Run("totp", func(t *testing.T) {
		out, err := identityRun(t, "admin", "totp", "reset", "user@manova.space", "--store", storePath, "--json")
		if err != nil {
			t.Fatalf("admin totp reset: %v\n%s", err, out)
		}
		if !strings.Contains(string(decodeEnvelope(t, out).Data), `"recovery_code"`) {
			t.Fatalf("unexpected data: %s", out)
		}
	})
}

func TestIdentityJSONFailureEnvelope(t *testing.T) {
	out, err := identityRun(t, "staff", "list", "--owner-store", filepath.Join(t.TempDir(), "missing.json"),
		"--server", "http://127.0.0.1:1", "--json")
	if err == nil {
		t.Fatal("expected unverified owner error")
	}
	env := decodeEnvelope(t, out)
	if env.OK || env.Error == nil || env.Error.Code != "auth" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}
