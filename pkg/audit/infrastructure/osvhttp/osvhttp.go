// Package osvhttp implements the audit advisory port over the OSV
// querybatch HTTP API. It never runs a scanner.
package osvhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
)

// DefaultBatchSize is the number of package queries OSV accepts per request.
const DefaultBatchSize = 1000

// Client queries OSV over HTTP.
type Client struct {
	BaseURL   string
	Client    *http.Client
	BatchSize int
}

// New builds a Client against baseURL, for example https://api.osv.dev.
func New(baseURL string) *Client {
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Client:    &http.Client{Timeout: 30 * time.Second},
		BatchSize: DefaultBatchSize,
	}
}

type query struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Version string `json:"version"`
}

// Query reads advisories for the resolved packages. A failed batch returns
// the hits collected so far together with the error.
func (c *Client) Query(ctx context.Context, packages []domain.PackageRef) ([]domain.AdvisoryHit, error) {
	if c == nil || c.BaseURL == "" {
		return nil, fmt.Errorf("OSV base URL is empty")
	}
	if len(packages) == 0 {
		return nil, nil
	}
	batch := c.BatchSize
	if batch <= 0 {
		batch = DefaultBatchSize
	}
	var hits []domain.AdvisoryHit
	for start := 0; start < len(packages); start += batch {
		end := start + batch
		if end > len(packages) {
			end = len(packages)
		}
		part, err := c.query(ctx, packages[start:end])
		hits = append(hits, part...)
		if err != nil {
			return hits, err
		}
	}
	return hits, nil
}

func (c *Client) query(ctx context.Context, packages []domain.PackageRef) ([]domain.AdvisoryHit, error) {
	queries := make([]query, 0, len(packages))
	for _, pkg := range packages {
		q := query{Version: pkg.Version}
		q.Package.Name = pkg.Name
		q.Package.Ecosystem = pkg.Ecosystem
		queries = append(queries, q)
	}
	body, err := json.Marshal(struct {
		Queries []query `json:"queries"`
	}{Queries: queries})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/querybatch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, fmt.Errorf("OSV returned %s: %s", response.Status, strings.TrimSpace(string(detail)))
	}

	var decoded struct {
		Results []struct {
			Vulns []struct {
				ID      string `json:"id"`
				Summary string `json:"summary"`
			} `json:"vulns"`
		} `json:"results"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decoding OSV response: %w", err)
	}

	var hits []domain.AdvisoryHit
	for i, result := range decoded.Results {
		if i >= len(packages) || len(result.Vulns) == 0 {
			continue
		}
		hit := domain.AdvisoryHit{Package: packages[i]}
		seen := map[string]bool{}
		for _, vuln := range result.Vulns {
			if vuln.ID == "" || seen[vuln.ID] {
				continue
			}
			seen[vuln.ID] = true
			hit.Advisories = append(hit.Advisories, domain.Advisory{ID: vuln.ID, Summary: vuln.Summary})
		}
		if len(hit.Advisories) > 0 {
			hits = append(hits, hit)
		}
	}
	return hits, nil
}
