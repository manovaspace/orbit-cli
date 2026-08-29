package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/staffhmac"
)

func TestStaffClientCreateSignsHeaders(t *testing.T) {
	secret := "test-hmac-secret-for-staff-client"
	var gotTS int64
	var gotSig string
	var gotBody []byte
	var gotMethod, gotPath string
	var gotIdem string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if r.URL.RawQuery != "" {
			t.Fatal("query string must be empty")
		}
		gotIdem = r.Header.Get("Idempotency-Key")
		tsHeader := r.Header.Get("X-Orbit-Timestamp")
		gotSig = r.Header.Get("X-Orbit-Signature")
		var err error
		gotTS, err = strconv.ParseInt(tsHeader, 10, 64)
		if err != nil {
			t.Fatalf("bad timestamp: %v", err)
		}
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		want := staffhmac.Sign(secret, gotTS, r.Method, r.URL.Path, gotBody)
		if gotSig != want {
			t.Fatalf("signature mismatch: got %s want %s", gotSig, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(StaffCreateResult{
			StaffMember: StaffMember{
				UID:             "sara",
				DisplayName:     "Sara",
				Mail:            "sara@manova.space",
				PersonalForward: "sara@gmail.com",
				Groups:          []string{"dev"},
				Status:          "active",
			},
			LDAPPassword: "ldap-pass",
			MailPassword: "mail-pass",
			Idempotent:   false,
		})
	}))
	defer srv.Close()

	c := NewStaffClient(srv.URL, secret)
	res, err := c.Create(context.Background(), StaffCreateInput{
		UID:             "sara",
		DisplayName:     "Sara",
		PersonalForward: "sara@gmail.com",
		Groups:          []string{"dev"},
		IdempotencyKey:  "idem-1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/staff" {
		t.Fatalf("got %s %s", gotMethod, gotPath)
	}
	if gotIdem != "idem-1" {
		t.Fatalf("idempotency key: %q", gotIdem)
	}
	if time.Now().Unix()-gotTS > 5 {
		t.Fatalf("timestamp too old: %d", gotTS)
	}
	wantSig := staffhmac.Sign(secret, gotTS, http.MethodPost, "/api/v1/staff", gotBody)
	if gotSig != wantSig {
		t.Fatalf("client signature %s != recomputed %s", gotSig, wantSig)
	}
	if res.UID != "sara" || res.LDAPPassword != "ldap-pass" {
		t.Fatalf("unexpected result: %+v", res)
	}
}
