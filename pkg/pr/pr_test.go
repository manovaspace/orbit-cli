package pr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"code.gitea.io/sdk/gitea"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "Standard SSH with scheme",
			rawURL:    "ssh://git@git.internal.manova.space/manova/handbook.git",
			wantOwner: "manova",
			wantRepo:  "handbook",
			wantErr:   false,
		},
		{
			name:      "SSH with port",
			rawURL:    "ssh://git@git.internal.manova.space:2222/manova/handbook.git",
			wantOwner: "manova",
			wantRepo:  "handbook",
			wantErr:   false,
		},
		{
			name:      "SCP-like syntax",
			rawURL:    "git@git.internal.manova.space:manova/handbook.git",
			wantOwner: "manova",
			wantRepo:  "handbook",
			wantErr:   false,
		},
		{
			name:      "GitHub SCP-like",
			rawURL:    "git@github.com:manovaspace/orbit-cli.git",
			wantOwner: "manovaspace",
			wantRepo:  "orbit-cli",
			wantErr:   false,
		},
		{
			name:      "HTTPS with .git suffix",
			rawURL:    "https://git.internal.manova.space/manova/orbit-frontend.git",
			wantOwner: "manova",
			wantRepo:  "orbit-frontend",
			wantErr:   false,
		},
		{
			name:      "HTTPS without .git suffix",
			rawURL:    "https://git.internal.manova.space/clients/pulse",
			wantOwner: "clients",
			wantRepo:  "pulse",
			wantErr:   false,
		},
		{
			name:      "HTTPS with trailing slash",
			rawURL:    "https://git.internal.manova.space/manova/toolkit/",
			wantOwner: "manova",
			wantRepo:  "toolkit",
			wantErr:   false,
		},
		{
			name:    "Empty URL",
			rawURL:  "",
			wantErr: true,
		},
		{
			name:    "Single segment URL",
			rawURL:  "https://git.internal.manova.space/onlyone",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRemoteURL(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRemoteURL(%q) expected error, got nil", tt.rawURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q) unexpected error: %v", tt.rawURL, err)
			}
			if owner != tt.wantOwner {
				t.Errorf("ParseRemoteURL(%q) owner = %q, want %q", tt.rawURL, owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("ParseRemoteURL(%q) repo = %q, want %q", tt.rawURL, repo, tt.wantRepo)
			}
		})
	}
}

func TestDetectRepoInfo(t *testing.T) {
	tempDir := t.TempDir()

	// Initialize git repo in tempDir
	cmdInit := exec.Command("git", "-C", tempDir, "init", "-b", "feat/test-branch")
	if _, err := cmdInit.CombinedOutput(); err != nil {
		// Fallback for older git without -b
		if out2, err2 := exec.Command("git", "-C", tempDir, "init").CombinedOutput(); err2 != nil {
			t.Fatalf("git init failed: %v (%s)", err2, string(out2))
		}
		_ = exec.Command("git", "-C", tempDir, "checkout", "-b", "feat/test-branch").Run()
	}

	// Configure remote origin
	remoteURL := "ssh://git@git.internal.manova.space/manova/handbook.git"
	cmdRemote := exec.Command("git", "-C", tempDir, "remote", "add", "origin", remoteURL)
	if out, err := cmdRemote.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin failed: %v (%s)", err, string(out))
	}

	// Create a dummy commit so HEAD points to an actual commit and branch
	dummyFile := filepath.Join(tempDir, "README.md")
	if err := os.WriteFile(dummyFile, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}
	_ = exec.Command("git", "-C", tempDir, "config", "user.email", "agent@manova.space").Run()
	_ = exec.Command("git", "-C", tempDir, "config", "user.name", "Agent").Run()
	_ = exec.Command("git", "-C", tempDir, "add", "README.md").Run()
	_ = exec.Command("git", "-C", tempDir, "commit", "-m", "initial commit").Run()

	owner, repo, branch, err := DetectRepoInfo(tempDir)
	if err != nil {
		t.Fatalf("DetectRepoInfo(%q) failed: %v", tempDir, err)
	}

	if owner != "manova" {
		t.Errorf("DetectRepoInfo owner = %q, want %q", owner, "manova")
	}
	if repo != "handbook" {
		t.Errorf("DetectRepoInfo repo = %q, want %q", repo, "handbook")
	}
	if branch != "feat/test-branch" {
		t.Errorf("DetectRepoInfo branch = %q, want %q", branch, "feat/test-branch")
	}
}

func TestResolveCredentials(t *testing.T) {
	// Clean environment variables before test
	cleanEnv := func() {
		os.Unsetenv("FORGEJO_TOKEN")
		os.Unsetenv("FORGEJO_ACCESS_TOKEN")
		os.Unsetenv("GITEA_TOKEN")
		os.Unsetenv("FORGEJO_URL")
		os.Unsetenv("GITEA_URL")
	}
	cleanEnv()
	defer cleanEnv()

	t.Run("From process env FORGEJO_TOKEN", func(t *testing.T) {
		cleanEnv()
		t.Setenv("FORGEJO_TOKEN", "token-from-env")
		t.Setenv("FORGEJO_URL", "https://custom.git.manova.space")

		cfg, err := ResolveCredentials("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Token != "token-from-env" {
			t.Errorf("got token %q, want %q", cfg.Token, "token-from-env")
		}
		if cfg.BaseURL != "https://custom.git.manova.space" {
			t.Errorf("got baseURL %q, want %q", cfg.BaseURL, "https://custom.git.manova.space")
		}
	})

	t.Run("From process env FORGEJO_ACCESS_TOKEN with default URL", func(t *testing.T) {
		cleanEnv()
		t.Setenv("FORGEJO_ACCESS_TOKEN", "access-token-456")

		cfg, err := ResolveCredentials("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Token != "access-token-456" {
			t.Errorf("got token %q, want %q", cfg.Token, "access-token-456")
		}
		if cfg.BaseURL != DefaultBaseURL {
			t.Errorf("got baseURL %q, want %q", cfg.BaseURL, DefaultBaseURL)
		}
	})

	t.Run("From .cursor/mcp.env in workspace root", func(t *testing.T) {
		cleanEnv()
		mockRoot := t.TempDir()
		cursorDir := filepath.Join(mockRoot, ".cursor")
		if err := os.MkdirAll(cursorDir, 0755); err != nil {
			t.Fatalf("failed to create .cursor dir: %v", err)
		}

		mcpContent := "# Forgejo Config\n" +
			"FORGEJO_URL=\"https://git.internal.test\"\n" +
			"FORGEJO_ACCESS_TOKEN=\"mock-token-789\"\n"
		if err := os.WriteFile(filepath.Join(cursorDir, "mcp.env"), []byte(mcpContent), 0600); err != nil {
			t.Fatalf("failed to write mcp.env: %v", err)
		}

		cfg, err := ResolveCredentials(mockRoot)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Token != "mock-token-789" {
			t.Errorf("got token %q, want %q", cfg.Token, "mock-token-789")
		}
		if cfg.BaseURL != "https://git.internal.test" {
			t.Errorf("got baseURL %q, want %q", cfg.BaseURL, "https://git.internal.test")
		}
	})

	t.Run("Missing credentials returns error", func(t *testing.T) {
		cleanEnv()
		emptyDir := t.TempDir()

		_, err := ResolveCredentials(emptyDir)
		if err == nil {
			t.Fatal("expected error when credentials missing, got nil")
		}
	})
}

func TestClientMethodsWithMockServer(t *testing.T) {
	now := time.Now()
	mockPR := &gitea.PullRequest{
		Index:     42,
		Title:     "feat(core): add feature",
		Body:      "Details about feature",
		State:     gitea.StateOpen,
		Created:   &now,
		Base:      &gitea.PRBranchInfo{Name: "main"},
		Head:      &gitea.PRBranchInfo{Name: "feat/cool-feature"},
		Mergeable: true,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Auth check
		auth := r.Header.Get("Authorization")
		if auth != "token test-token" {
			http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		switch {
		// Create PR: POST /api/v1/repos/manova/handbook/pulls
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/manova/handbook/pulls":
			var opt gitea.CreatePullRequestOption
			if err := json.NewDecoder(r.Body).Decode(&opt); err != nil {
				http.Error(w, `{"message":"Bad Request"}`, http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(mockPR)

		// Get PR: GET /api/v1/repos/manova/handbook/pulls/42
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/manova/handbook/pulls/42":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(mockPR)

		// List Open PRs: GET /api/v1/repos/manova/handbook/pulls
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/manova/handbook/pulls":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*gitea.PullRequest{mockPR})

		// Merge PR: POST /api/v1/repos/manova/handbook/pulls/42/merge
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/manova/handbook/pulls/42/merge":
			var opt gitea.MergePullRequestOption
			if err := json.NewDecoder(r.Body).Decode(&opt); err != nil {
				http.Error(w, `{"message":"Bad Request"}`, http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "pull request merged"})

		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := NewClient(&Config{
		BaseURL: ts.URL,
		Token:   "test-token",
	}, WithHTTPClient(ts.Client()))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// 1. CreatePR
	t.Run("CreatePR", func(t *testing.T) {
		pr, err := client.CreatePR("manova", "handbook", "feat(core): add feature", "Details about feature", "feat/cool-feature", "main")
		if err != nil {
			t.Fatalf("CreatePR failed: %v", err)
		}
		if pr.Index != 42 {
			t.Errorf("CreatePR index = %d, want 42", pr.Index)
		}
		if pr.Title != "feat(core): add feature" {
			t.Errorf("CreatePR title = %q, want %q", pr.Title, "feat(core): add feature")
		}
	})

	// 2. GetPR
	t.Run("GetPR", func(t *testing.T) {
		pr, err := client.GetPR("manova", "handbook", 42)
		if err != nil {
			t.Fatalf("GetPR failed: %v", err)
		}
		if pr.Index != 42 {
			t.Errorf("GetPR index = %d, want 42", pr.Index)
		}
	})

	// 3. ListOpenPRs
	t.Run("ListOpenPRs", func(t *testing.T) {
		prs, err := client.ListOpenPRs("manova", "handbook")
		if err != nil {
			t.Fatalf("ListOpenPRs failed: %v", err)
		}
		if len(prs) != 1 {
			t.Fatalf("ListOpenPRs count = %d, want 1", len(prs))
		}
		if prs[0].Index != 42 {
			t.Errorf("ListOpenPRs index = %d, want 42", prs[0].Index)
		}
	})

	// 4. MergePR
	t.Run("MergePR styles", func(t *testing.T) {
		// Merge
		if err := client.MergePR("manova", "handbook", 42, gitea.MergeStyleMerge); err != nil {
			t.Errorf("MergePR(merge) failed: %v", err)
		}
		// Squash
		if err := client.MergePR("manova", "handbook", 42, gitea.MergeStyleSquash); err != nil {
			t.Errorf("MergePR(squash) failed: %v", err)
		}
		// Rebase
		if err := client.MergePR("manova", "handbook", 42, gitea.MergeStyleRebase); err != nil {
			t.Errorf("MergePR(rebase) failed: %v", err)
		}
		// Invalid style
		if err := client.MergePR("manova", "handbook", 42, gitea.MergeStyle("invalid")); err == nil {
			t.Error("MergePR(invalid) expected error, got nil")
		}
	})

	// 5. Validation errors
	t.Run("Validation errors", func(t *testing.T) {
		if _, err := client.CreatePR("", "repo", "title", "body", "head", "base"); err == nil {
			t.Error("expected error with empty owner")
		}
		if _, err := client.CreatePR("owner", "repo", "", "body", "head", "base"); err == nil {
			t.Error("expected error with empty title")
		}
		if _, err := client.CreatePR("owner", "repo", "title", "body", "", "base"); err == nil {
			t.Error("expected error with empty head")
		}
		if err := client.MergePR("owner", "repo", 0, gitea.MergeStyleMerge); err == nil {
			t.Error("expected error with index <= 0")
		}
		if _, err := client.GetPR("owner", "repo", -1); err == nil {
			t.Error("expected error with index <= 0")
		}
	})
}
