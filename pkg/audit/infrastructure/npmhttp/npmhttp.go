// Package npmhttp implements the audit registry port over the public npm
// registry HTTP API. It never runs npm or bun.
package npmhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Registry reads package versions from an npm-compatible registry.
type Registry struct {
	BaseURL string
	Client  *http.Client
}

// New builds a Registry against baseURL.
func New(baseURL string) *Registry {
	return &Registry{BaseURL: strings.TrimRight(baseURL, "/"), Client: &http.Client{Timeout: 10 * time.Second}}
}

// Latest returns the registry's latest dist-tag for pkg.
func (r *Registry) Latest(ctx context.Context, pkg string) (string, error) {
	if r == nil || r.BaseURL == "" {
		return "", fmt.Errorf("registry base URL is empty")
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := r.BaseURL + "/" + url.PathEscape(pkg)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: registry returned %s", pkg, response.Status)
	}
	var body struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("%s: %w", pkg, err)
	}
	latest := body.DistTags["latest"]
	if latest == "" {
		return "", fmt.Errorf("%s: registry reported no latest tag", pkg)
	}
	return latest, nil
}
