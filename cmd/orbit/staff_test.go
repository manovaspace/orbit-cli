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
