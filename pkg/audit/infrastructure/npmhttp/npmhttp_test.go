package npmhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestReadsDistTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "@manovaspace/ui") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"dist-tags":{"latest":"0.12.0"}}`))
	}))
	defer server.Close()

	latest, err := New(server.URL).Latest(context.Background(), "@manovaspace/ui")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest != "0.12.0" {
		t.Errorf("latest = %q", latest)
	}
}

func TestLatestReportsRegistryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	if _, err := New(server.URL).Latest(context.Background(), "@manovaspace/ui"); err == nil {
		t.Fatal("404 accepted as a version")
	}
}

func TestLatestRequiresBaseURL(t *testing.T) {
	if _, err := New("").Latest(context.Background(), "@manovaspace/ui"); err == nil {
		t.Fatal("empty base URL accepted")
	}
}
