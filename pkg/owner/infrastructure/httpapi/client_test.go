package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInitiateAndVerifyChallenge(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/admin/challenge":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending", "email": "owner@example.com"})
		case "/api/v1/admin/verify":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "verified", "email": "owner@example.com"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	server := New(srv.URL)

	dispatch, err := server.InitiateChallenge(context.Background(), "owner@example.com")
	if err != nil {
		t.Fatalf("InitiateChallenge: %v", err)
	}
	if dispatch.Email != "owner@example.com" {
		t.Errorf("dispatch = %+v", dispatch)
	}

	verification, err := server.VerifyChallenge(context.Background(), "owner@example.com", "123456")
	if err != nil {
		t.Fatalf("VerifyChallenge: %v", err)
	}
	if verification.Status != "verified" {
		t.Errorf("verification = %+v", verification)
	}

	if len(paths) != 2 || paths[0] != "/api/v1/admin/challenge" || paths[1] != "/api/v1/admin/verify" {
		t.Errorf("paths = %v", paths)
	}
}

func TestCreateGrantParsesExpiry(t *testing.T) {
	expires := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	var body map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/grants" {
			t.Errorf("path = %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "granted",
			"email":      "sara@manova.space",
			"code":       "8492-0194",
			"expires_at": expires,
		})
	}))
	defer srv.Close()

	registration, err := New(srv.URL).CreateGrant(context.Background(), "sara@manova.space", "admin", "8492-0194", "master-secret", 15*time.Minute)
	if err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	if !registration.ExpiresAt.Equal(expires) {
		t.Errorf("expiresAt = %v, want %v", registration.ExpiresAt, expires)
	}
	if auth != "Bearer master-secret" {
		t.Errorf("authorization = %q", auth)
	}
	if body["email"] != "sara@manova.space" || body["code"] != "8492-0194" {
		t.Errorf("body = %v", body)
	}
}
