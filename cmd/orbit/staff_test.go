package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/invite"
)

func TestStaffOwnerGuard_Unverified(t *testing.T) {
	tempDir := t.TempDir()
	unverifiedOwnerPath := filepath.Join(tempDir, "nonexistent-owner.json")

	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "list",
		"--owner-store", unverifiedOwnerPath,
		"--server", "http://127.0.0.1:9",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when owner is unverified")
	}
	if !strings.Contains(err.Error(), staffOwnerUnverified) {
		t.Fatalf("expected error %q, got %q", staffOwnerUnverified, err.Error())
	}
}

func TestStaffCmdRegistered(t *testing.T) {
	cmd := newRootCmd()
	found := false
	for _, c := range cmd.Commands() {
		if c.Name() == "staff" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("staff command not registered")
	}
}

func TestStaffRecreateRegistered(t *testing.T) {
	staff := newStaffCmd()
	found := false
	for _, c := range staff.Commands() {
		if c.Name() == "recreate" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("recreate command not registered")
	}
}

func TestStaffRecreateDeletesThenCreates(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/staff/sara":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/staff":
			body, _ := io.ReadAll(r.Body)
			var in map[string]any
			_ = json.Unmarshal(body, &in)
			if in["uid"] != "sara" || in["totp"] != true {
				t.Errorf("create body = %s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uid":              "sara",
				"display_name":     "Sara",
				"mail":             "sara@manova.space",
				"personal_forward": "sara@gmail.com",
				"ldap_password":    "sso-pass",
				"mail_password":    "mail-pass",
				"otpauth":          "otpauth://totp/fake:sara",
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "recreate",
		"--uid", "sara",
		"--name", "Sara",
		"--forward", "sara@gmail.com",
		"--totp",
		"--owner-store", storePath,
		"--server", srv.URL,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("recreate: %v\n%s", err, buf.String())
	}
	if len(methods) != 2 || methods[0] != "DELETE /api/v1/staff/sara" || methods[1] != "POST /api/v1/staff" {
		t.Fatalf("methods = %v", methods)
	}
	out := buf.String()
	if !strings.Contains(out, "sso    sso-pass") || !strings.Contains(out, "otpauth otpauth://totp/fake:sara") {
		t.Fatalf("output = %q", out)
	}
}

func TestStaffRecreateIgnoresDelete404(t *testing.T) {
	created := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		created = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uid":              "sara",
			"personal_forward": "sara@gmail.com",
			"ldap_password":    "sso-pass",
			"mail_password":    "mail-pass",
		})
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "recreate",
		"--uid", "sara",
		"--forward", "sara@gmail.com",
		"--owner-store", storePath,
		"--server", srv.URL,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("recreate: %v\n%s", err, buf.String())
	}
	if !created {
		t.Fatal("expected create after 404 delete")
	}
}

func TestStaffResetPasswordTOTP(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/staff/sara/reset-password" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ldap_password": "",
			"mail_password": "",
			"otpauth":       "otpauth://totp/new:sara",
		})
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "reset-password", "sara",
		"--totp",
		"--owner-store", storePath,
		"--server", srv.URL,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("reset-password: %v\n%s", err, buf.String())
	}
	if !strings.Contains(string(gotBody), `"totp"`) {
		t.Fatalf("body = %s", gotBody)
	}
	if !strings.Contains(buf.String(), "otpauth otpauth://totp/new:sara") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestStaffDelete_MissingReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/staff/clitest" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "delete", "clitest",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error deleting missing staff member, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", err)
	}
	if strings.Contains(buf.String(), "deleted clitest") {
		t.Fatalf("must not print 'deleted clitest' on failure: %s", buf.String())
	}
}

func TestStaffDelete_Success(t *testing.T) {
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/staff/clitest0830" {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "delete", "clitest0830",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Fatal("expected delete handler called")
	}
	if !strings.Contains(buf.String(), "deleted clitest0830") {
		t.Fatalf("expected 'deleted clitest0830', got: %s", buf.String())
	}
}

func TestStaffDisable_MissingReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/staff/missing/disable" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "disable", "missing",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error disabling missing staff member, got nil")
	}
	if strings.Contains(buf.String(), "disabled missing") {
		t.Fatalf("must not print 'disabled missing' on failure: %s", buf.String())
	}
}

func TestStaffEnable_MissingReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/staff/missing/enable" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "enable", "missing",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error enabling missing staff member, got nil")
	}
	if strings.Contains(buf.String(), "enabled missing") {
		t.Fatalf("must not print 'enabled missing' on failure: %s", buf.String())
	}
}

func TestStaffGet_MissingReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/staff/missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "get", "missing",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error getting missing staff member, got nil")
	}
}

func TestStaffUpdate_MissingReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/api/v1/staff/missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "update", "missing",
		"--name", "Missing User",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error updating missing staff member, got nil")
	}
	if strings.Contains(buf.String(), "updated missing") {
		t.Fatalf("must not print 'updated missing' on failure: %s", buf.String())
	}
}

func TestStaffCreate_InviteWithTOTPAndWebSetupURL(t *testing.T) {
	const expectedOTPAuth = "otpauth://totp/Orbit:newuser?secret=JBSWY3DPEHPK3PXP&issuer=Orbit"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/staff" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uid":              "newuser",
			"display_name":     "New User",
			"personal_forward": "newuser@example.com",
			"ldap_password":    "sso-password",
			"mail_password":    "mail-password",
			"otpauth":          expectedOTPAuth,
		})
	}))
	defer srv.Close()

	tempDir := t.TempDir()
	storePath, ownerRec := createTestVerifiedOwnerStore(t, tempDir)
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "create",
		"--uid", "newuser",
		"--name", "New User",
		"--forward", "newuser@example.com",
		"--totp",
		"--invite",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("staff create --invite --totp failed: %v\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "web setup https://orbit.manova.space/setup?token=orbit-inv.") {
		t.Fatalf("expected web setup URL in output, got:\n%s", out)
	}

	var tokenStr string
	prefix := "web setup https://orbit.manova.space/setup?token="
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			tokenStr = strings.TrimSpace(strings.TrimPrefix(line, prefix))
			break
		}
	}
	if tokenStr == "" {
		t.Fatalf("failed to extract token from output:\n%s", out)
	}

	claims, err := invite.ValidateToken(tokenStr, []byte(ownerRec.RootSigningSecret))
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.Metadata == nil {
		t.Fatal("expected claims.Metadata to be non-nil")
	}
	if claims.Metadata["otpauth"] != expectedOTPAuth {
		t.Fatalf("expected metadata['otpauth'] = %q, got %q", expectedOTPAuth, claims.Metadata["otpauth"])
	}
}

func TestStaffCreate_InviteWithNoSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uid":              "nosenduser",
			"display_name":     "No Send User",
			"mail":             "nosenduser@manova.space",
			"personal_forward": "nosenduser@example.com",
			"status":           "active",
		})
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "create",
		"--uid", "nosenduser",
		"--name", "No Send User",
		"--forward", "nosenduser@example.com",
		"--invite",
		"--no-send",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("staff create --invite --no-send failed: %v\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "web setup https://orbit.manova.space/setup?token=orbit-inv.") {
		t.Fatalf("expected web setup URL in output, got:\n%s", out)
	}
	if strings.Contains(out, "mail      dispatched to") {
		t.Errorf("expected no email dispatch with --no-send, got:\n%s", out)
	}
}


func TestStaffList_TableRendering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/staff" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"uid":              "sara",
				"display_name":     "Sara Connor",
				"mail":             "sara@manova.space",
				"personal_forward": "sara@gmail.com",
				"status":           "active",
			},
			{
				"uid":              "john",
				"display_name":     "John Doe",
				"mail":             "john@manova.space",
				"personal_forward": "john@example.com",
				"status":           "disabled",
			},
			{
				"uid":              "alex",
				"display_name":     "",
				"mail":             "alex@manova.space",
				"personal_forward": "alex@manova.space",
				"status":           "enabled",
			},
			{
				"uid":              "custom",
				"display_name":     "Custom User",
				"mail":             "custom@manova.space",
				"personal_forward": "custom@test.com",
				"status":           "pending",
			},
		})
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "list",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("staff list failed: %v\n%s", err, buf.String())
	}

	out := buf.String()

	// 1. Check no raw tab characters are present
	if strings.Contains(out, "\t") {
		t.Errorf("expected aligned table without raw tab characters, got: %q", out)
	}

	// 2. Check headers
	for _, header := range []string{"UID", "NAME", "STATUS", "FORWARD EMAIL"} {
		if !strings.Contains(out, header) {
			t.Errorf("expected header %q in output, got:\n%s", header, out)
		}
	}

	// 3. Check divider line
	if !strings.Contains(out, "─") {
		t.Errorf("expected horizontal divider line in table output, got:\n%s", out)
	}

	// 4. Check data rows and status formatting
	if !strings.Contains(out, "sara") || !strings.Contains(out, "Sara Connor") || !strings.Contains(out, "✔ active") || !strings.Contains(out, "sara@gmail.com") {
		t.Errorf("expected sara row with ✔ active, got:\n%s", out)
	}

	if !strings.Contains(out, "john") || !strings.Contains(out, "John Doe") || !strings.Contains(out, "✖ disabled") || !strings.Contains(out, "john@example.com") {
		t.Errorf("expected john row with ✖ disabled, got:\n%s", out)
	}

	if !strings.Contains(out, "alex") || !strings.Contains(out, "-") || !strings.Contains(out, "alex@manova.space") {
		t.Errorf("expected alex row with '-' for empty name, got:\n%s", out)
	}

	if !strings.Contains(out, "custom") || !strings.Contains(out, "Custom User") || !strings.Contains(out, "pending") || !strings.Contains(out, "custom@test.com") {
		t.Errorf("expected custom row with plain status 'pending', got:\n%s", out)
	}
}

func TestStaffList_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "list",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("staff list empty failed: %v\n%s", err, buf.String())
	}

	out := buf.String()
	if strings.Contains(out, "\t") {
		t.Errorf("unexpected tab in output: %q", out)
	}
	if !strings.Contains(out, "UID") || !strings.Contains(out, "NAME") {
		t.Errorf("expected headers in empty list output, got:\n%s", out)
	}
}

func TestStaffList_Pagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"uid": "user1", "display_name": "User One", "mail": "user1@manova.space", "personal_forward": "u1@gmail.com", "status": "active"},
			{"uid": "user2", "display_name": "User Two", "mail": "user2@manova.space", "personal_forward": "u2@gmail.com", "status": "active"},
			{"uid": "user3", "display_name": "User Three", "mail": "user3@manova.space", "personal_forward": "u3@gmail.com", "status": "active"},
		})
	}))
	defer srv.Close()

	storePath, _ := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "list",
		"--owner-store", storePath,
		"--server", srv.URL,
		"--page", "1",
		"--limit", "2",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("staff list pagination failed: %v\n%s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "user1") || !strings.Contains(out, "user2") {
		t.Errorf("expected user1 and user2 on page 1, got:\n%s", out)
	}
	if strings.Contains(out, "user3") {
		t.Errorf("expected user3 NOT to be on page 1, got:\n%s", out)
	}
	if !strings.Contains(out, "Showing 1-2 of 3 rows (Page 1/2)") {
		t.Errorf("expected pagination footer, got:\n%s", out)
	}
}


