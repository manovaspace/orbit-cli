package staffapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/staff/domain"
)

func TestDirectoryRoundTripsDomainTypes(t *testing.T) {
	var lastMethod, lastPath, lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod, lastPath = r.Method, r.URL.Path
		data, _ := io.ReadAll(r.Body)
		lastBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/staff":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uid":              "alice",
				"display_name":     "Alice",
				"personal_forward": "alice@example.com",
				"ldap_password":    "one-time",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/staff/alice":
			_ = json.NewEncoder(w).Encode(map[string]any{"uid": "alice", "status": "active"})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/staff/alice":
			_ = json.NewEncoder(w).Encode(map[string]any{"uid": "alice", "first_name": "Alice"})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	dir := New(client.NewStaffClient(srv.URL, "secret"))

	created, err := dir.Create(context.Background(), domain.CreateInput{UID: "alice", PersonalForward: "alice@example.com"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.UID != "alice" || created.LDAPPassword != "one-time" {
		t.Errorf("created = %+v", created)
	}
	if lastMethod != http.MethodPost || lastPath != "/api/v1/staff" {
		t.Errorf("create request = %s %s", lastMethod, lastPath)
	}

	member, err := dir.Get(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if member.UID != "alice" || member.Status != "active" {
		t.Errorf("member = %+v", member)
	}

	updated, err := dir.Update(context.Background(), "alice", domain.UpdateInput{FirstName: "Alice"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.FirstName != "Alice" {
		t.Errorf("updated = %+v", updated)
	}
	if !strings.Contains(lastBody, `"first_name":"Alice"`) {
		t.Errorf("update body = %s", lastBody)
	}

	if err := dir.Disable(context.Background(), "alice"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if lastPath != "/api/v1/staff/alice/disable" {
		t.Errorf("disable path = %s", lastPath)
	}
}

func TestDirectoryResetPasswordSendsSingleTarget(t *testing.T) {
	var body map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ldap_password": "rotated"})
	}))
	defer srv.Close()

	dir := New(client.NewStaffClient(srv.URL, "secret"))
	res, err := dir.ResetPassword(context.Background(), "alice", true, false)
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if res.LDAPPassword != "rotated" {
		t.Errorf("res = %+v", res)
	}
	if len(body["targets"]) != 1 || body["targets"][0] != "ldap" {
		t.Errorf("targets = %v", body["targets"])
	}
}
