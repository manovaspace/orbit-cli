package changelog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/changelog"
)

func TestDefaultReleases_NotEmpty(t *testing.T) {
	if len(changelog.DefaultReleases) == 0 {
		t.Fatal("expected DefaultReleases to have built-in changelog entries")
	}
}

func TestFindRelease(t *testing.T) {
	releases := []changelog.ReleaseEntry{
		{Version: "v0.3.0", Highlights: []string{"Item 1"}},
		{Version: "v0.2.9", Highlights: []string{"Item 2"}},
	}

	// With 'v'
	r1 := changelog.FindRelease(releases, "v0.3.0")
	if r1 == nil || r1.Version != "v0.3.0" {
		t.Errorf("expected to find v0.3.0, got: %+v", r1)
	}

	// Without 'v'
	r2 := changelog.FindRelease(releases, "0.2.9")
	if r2 == nil || r2.Version != "v0.2.9" {
		t.Errorf("expected to find 0.2.9 normalized to v0.2.9, got: %+v", r2)
	}

	// Not found
	r3 := changelog.FindRelease(releases, "v9.9.9")
	if r3 != nil {
		t.Errorf("expected nil for nonexistent version, got: %+v", r3)
	}
}

func TestGetRecentReleases_Limit(t *testing.T) {
	releases := []changelog.ReleaseEntry{
		{Version: "v0.3.0"},
		{Version: "v0.2.9"},
		{Version: "v0.2.8"},
	}

	res := changelog.GetRecentReleases(releases, 2)
	if len(res) != 2 || res[0].Version != "v0.3.0" || res[1].Version != "v0.2.9" {
		t.Errorf("unexpected limit slice: %+v", res)
	}

	resAll := changelog.GetRecentReleases(releases, 100)
	if len(resAll) != 3 {
		t.Errorf("expected all 3 releases, got %d", len(resAll))
	}
}

func TestFetchReleases_RemoteMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"version": "v0.9.9",
			"published_at": "2026-08-25T19:00:00Z",
			"highlights": ["Remote test feature"]
		}`))
	}))
	defer srv.Close()

	releases := changelog.FetchReleases(context.Background(), srv.URL)
	if len(releases) == 0 || releases[0].Version != "v0.9.9" {
		t.Errorf("expected remote v0.9.9 to be prepended, got: %+v", releases)
	}
}

func TestFormatReleaseCard(t *testing.T) {
	entry := changelog.ReleaseEntry{
		Version:     "v0.3.0",
		PublishedAt: time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC),
		Highlights:  []string{"Highlight one", "Highlight two"},
	}

	out := changelog.FormatReleaseCard(entry)
	if !strings.Contains(out, "v0.3.0") || !strings.Contains(out, "Highlight one") {
		t.Errorf("expected formatted release card to contain version and highlights, got:\n%s", out)
	}
}
