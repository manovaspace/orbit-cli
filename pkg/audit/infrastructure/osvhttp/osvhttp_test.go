package osvhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
)

func TestQueryBatchMapsVulnsToPackages(t *testing.T) {
	var got struct {
		Queries []struct {
			Package struct {
				Name      string `json:"name"`
				Ecosystem string `json:"ecosystem"`
			} `json:"package"`
			Version string `json:"version"`
		} `json:"queries"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/querybatch" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"GHSA-a","summary":"one"},{"id":"GHSA-b","summary":"two"}]},{"vulns":[]}]}`))
	}))
	defer server.Close()

	hits, err := New(server.URL).Query(context.Background(), []domain.PackageRef{
		{Name: "lodash", Version: "4.17.20", Ecosystem: "npm"},
		{Name: "safe", Version: "1.0.0", Ecosystem: "npm"},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1: %+v", len(hits), hits)
	}
	if hits[0].Package.Name != "lodash" || len(hits[0].Advisories) != 2 {
		t.Errorf("hit = %+v", hits[0])
	}
	if len(got.Queries) != 2 || got.Queries[0].Package.Ecosystem != "npm" || got.Queries[0].Version != "4.17.20" {
		t.Errorf("queries = %+v", got.Queries)
	}
}

func TestQueryBatchesLargeLists(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Queries []json.RawMessage `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if len(body.Queries) > 1 {
			t.Errorf("batch carried %d queries, want at most 1", len(body.Queries))
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[]}]}`))
	}))
	defer server.Close()

	client := New(server.URL)
	client.BatchSize = 1
	_, err := client.Query(context.Background(), []domain.PackageRef{
		{Name: "a", Version: "1.0.0", Ecosystem: "npm"},
		{Name: "b", Version: "2.0.0", Ecosystem: "npm"},
		{Name: "c", Version: "3.0.0", Ecosystem: "npm"},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if requests != 3 {
		t.Errorf("requests = %d, want 3", requests)
	}
}

func TestQueryReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer server.Close()

	if _, err := New(server.URL).Query(context.Background(), []domain.PackageRef{{Name: "a", Version: "1.0.0", Ecosystem: "npm"}}); err == nil {
		t.Fatal("403 accepted")
	}
}

func TestQueryRequiresBaseURL(t *testing.T) {
	if _, err := New("").Query(context.Background(), nil); err == nil {
		t.Fatal("empty base URL accepted")
	}
}
