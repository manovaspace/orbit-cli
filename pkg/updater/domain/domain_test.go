package domain

import (
	"strings"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		target   string
		expected bool
	}{
		{name: "minor version upgrade", current: "v1.0.0", target: "v1.1.0", expected: true},
		{name: "patch version downgrade", current: "v1.2.0", target: "v1.1.5", expected: false},
		{name: "dev build against release", current: "dev", target: "v1.0.0", expected: true},
		{name: "dev build against dev build", current: "dev", target: "dev", expected: false},
		{name: "release against dev build", current: "v1.0.0", target: "dev", expected: false},
		{name: "identical versions with v prefix", current: "v1.0.0", target: "v1.0.0", expected: false},
		{name: "identical versions without v prefix", current: "1.0.0", target: "1.0.0", expected: false},
		{name: "mixed v prefix comparison", current: "1.0.0", target: "v1.0.1", expected: true},
		{name: "major version upgrade", current: "v1.9.9", target: "v2.0.0", expected: true},
		{name: "prerelease upgrade to stable", current: "v1.0.0-rc.1", target: "v1.0.0", expected: true},
		{name: "stable to older prerelease", current: "v1.0.0", target: "v1.0.0-rc.1", expected: false},
		{name: "empty current version against valid release", current: "", target: "v1.0.0", expected: true},
		{name: "empty target version", current: "v1.0.0", target: "", expected: false},
		{name: "invalid semver strings", current: "invalid-cur", target: "invalid-tgt", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNewerVersion(tt.current, tt.target); got != tt.expected {
				t.Errorf("IsNewerVersion(%q, %q) = %v; want %v", tt.current, tt.target, got, tt.expected)
			}
		})
	}
}

func TestResolveAPIEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		apiURL   string
		repoSlug string
		expected string
	}{
		{name: "empty defaults to GitHub", apiURL: "", repoSlug: "", expected: "https://api.github.com/repos/manovaspace/orbit-cli/releases/latest"},
		{name: "custom API with trailing slash", apiURL: "https://git.example.com/", repoSlug: "org/repo", expected: "https://git.example.com/api/v1/repos/org/repo/releases/latest"},
		{name: "custom API already having /api/v1", apiURL: "https://git.example.com/api/v1", repoSlug: "org/repo", expected: "https://git.example.com/api/v1/repos/org/repo/releases/latest"},
		{name: "standard github.com", apiURL: "https://github.com", repoSlug: "manovaspace/orbit-cli", expected: "https://api.github.com/repos/manovaspace/orbit-cli/releases/latest"},
		{name: "api.github.com", apiURL: "https://api.github.com", repoSlug: "manovaspace/orbit-cli", expected: "https://api.github.com/repos/manovaspace/orbit-cli/releases/latest"},
		{name: "exact releases latest URL", apiURL: "http://localhost:8080/custom/releases/latest", repoSlug: "ignored/repo", expected: "http://localhost:8080/custom/releases/latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveAPIEndpoint(tt.apiURL, tt.repoSlug); got != tt.expected {
				t.Errorf("ResolveAPIEndpoint(%q, %q) = %q; want %q", tt.apiURL, tt.repoSlug, got, tt.expected)
			}
		})
	}
}

func TestExtractReleaseHighlights(t *testing.T) {
	markdownBody := `### Orbit v0.2.1 Release Notes

#### Hardened Update Pipeline & Architecture Improvements
- **Cryptographic Asset Verification:** Release assets now publish checksums.txt.
- **Symlink-Agnostic Asset Resolution:** Asset filters explicitly match orbit-*.
- **Passive Background Update Reminders:** Non-blocking 24-hour cache notifier.
- **GitHub API Rate Limit Bypass:** Automatically attaches GITHUB_TOKEN.
- **Actionable Permission Guidance:** Provides clean sudo instructions.
- **Extra Feature 6:** This item should be ignored because max is 5.
`

	highlights := ExtractReleaseHighlights(markdownBody, 5)
	if len(highlights) != 5 {
		t.Fatalf("expected 5 highlights, got %d", len(highlights))
	}
	if !strings.Contains(highlights[0], "Cryptographic Asset Verification") {
		t.Errorf("expected clean bold-stripped text in highlight 0, got: %q", highlights[0])
	}
	if strings.Contains(highlights[0], "**") {
		t.Errorf("expected markdown asterisks to be stripped, got: %q", highlights[0])
	}
}
