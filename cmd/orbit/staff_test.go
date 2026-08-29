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
