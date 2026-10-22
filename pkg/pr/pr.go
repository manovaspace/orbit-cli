package pr

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"code.gitea.io/sdk/gitea"
)

const (
	// DefaultBaseURL is the default Forgejo git server URL for Manova.
	DefaultBaseURL = "https://git.internal.manova.space"
)

// Config holds credentials and server configuration for Forgejo.
type Config struct {
	BaseURL string
	Token   string
}

// PR aliases the underlying pull request type to avoid leaking SDK imports.
type PR = gitea.PullRequest

// MergeStyle aliases the merge style type.
type MergeStyle = gitea.MergeStyle

// Client wraps the Forgejo/Gitea API client with logging and convenient helpers.
type Client struct {
	client     *gitea.Client
	cfg        *Config
	logger     *slog.Logger
	httpClient *http.Client
}

// ClientOption configures a Client instance.
type ClientOption func(*Client)

// WithLogger sets a custom slog logger for the Client.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// WithHTTPClient sets a custom http.Client for the underlying API client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// ResolveCredentials resolves the Forgejo URL and API token by checking:
// 1. Process environment variables: FORGEJO_TOKEN, FORGEJO_ACCESS_TOKEN, GITEA_TOKEN
// 2. .cursor/mcp.env in workspaceRoot (or discovered workspace root)
// 3. ~/.config/orbit/ user configuration files (forgejo.env, mcp.env, orbit-server.env)
// Falls back to DefaultBaseURL if the URL is not explicitly configured.
func ResolveCredentials(workspaceRoot string) (*Config, error) {
	var token string
	var baseURL string

	// 1. Check process environment variables
	for _, k := range []string{"FORGEJO_ACCESS_TOKEN", "FORGEJO_TOKEN", "GITEA_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			token = v
			break
		}
	}
	for _, k := range []string{"FORGEJO_URL", "GITEA_URL"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			baseURL = v
			break
		}
	}

	// 2. Check .cursor/mcp.env if token or baseURL missing
	if token == "" || baseURL == "" {
		envPath := ""
		if workspaceRoot != "" {
			p := filepath.Join(workspaceRoot, ".cursor", "mcp.env")
			if _, err := os.Stat(p); err == nil {
				envPath = p
			}
		} else {
			envPath = findWorkspaceMcpEnv()
		}

		if envPath != "" {
			envMap, err := parseEnvFile(envPath)
			if err == nil {
				if token == "" {
					for _, k := range []string{"FORGEJO_ACCESS_TOKEN", "FORGEJO_TOKEN", "GITEA_TOKEN"} {
						if v := strings.TrimSpace(envMap[k]); v != "" {
							token = v
							break
						}
					}
				}
				if baseURL == "" {
					for _, k := range []string{"FORGEJO_URL", "GITEA_URL"} {
						if v := strings.TrimSpace(envMap[k]); v != "" {
							baseURL = v
							break
						}
					}
				}
			}
		}
	}

	// 3. Check ~/.config/orbit/ user configuration if still missing
	if token == "" || baseURL == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configDir := filepath.Join(home, ".config", "orbit")
			for _, filename := range []string{"forgejo.env", "mcp.env", "orbit-server.env"} {
				cfgPath := filepath.Join(configDir, filename)
				if _, err := os.Stat(cfgPath); err == nil {
					envMap, err := parseEnvFile(cfgPath)
					if err == nil {
						if token == "" {
							for _, k := range []string{"FORGEJO_ACCESS_TOKEN", "FORGEJO_TOKEN", "GITEA_TOKEN"} {
								if v := strings.TrimSpace(envMap[k]); v != "" {
									token = v
									break
								}
							}
						}
						if baseURL == "" {
							for _, k := range []string{"FORGEJO_URL", "GITEA_URL"} {
								if v := strings.TrimSpace(envMap[k]); v != "" {
									baseURL = v
									break
								}
							}
						}
					}
				}
			}
		}
	}

	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	if token == "" {
		return nil, errors.New("forgejo credentials not found: please set FORGEJO_TOKEN environment variable or configure .cursor/mcp.env")
	}

	return &Config{
		BaseURL: baseURL,
		Token:   token,
	}, nil
}

// findWorkspaceMcpEnv scans current working directory and parent directories for .cursor/mcp.env.
func findWorkspaceMcpEnv() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		p := filepath.Join(dir, ".cursor", "mcp.env")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// parseEnvFile reads a simple KEY=VALUE formatted file, ignoring empty lines and comments.
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		env[key] = val
	}
	return env, scanner.Err()
}

// ParseRemoteURL extracts the repository owner and repository name from various git remote URL formats:
// - SSH URL: ssh://git@git.internal.manova.space/manova/handbook.git
// - SSH with port: ssh://git@git.internal.manova.space:2222/manova/handbook.git
// - SCP-like: git@git.internal.manova.space:manova/handbook.git
// - HTTPS: https://git.internal.manova.space/manova/handbook.git or https://git.internal.manova.space/manova/handbook
func ParseRemoteURL(rawURL string) (owner, repo string, err error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", "", errors.New("empty remote URL")
	}

	var pathPart string
	if strings.Contains(rawURL, "://") {
		u, err := url.Parse(rawURL)
		if err != nil {
			return "", "", fmt.Errorf("failed to parse remote URL %q: %w", rawURL, err)
		}
		pathPart = u.Path
	} else if idx := strings.Index(rawURL, ":"); idx != -1 {
		// SCP-style syntax: git@host:owner/repo.git
		pathPart = rawURL[idx+1:]
	} else {
		// Local file path or simple path
		pathPart = rawURL
	}

	pathPart = strings.Trim(pathPart, "/")
	pathPart = strings.TrimSuffix(pathPart, ".git")

	parts := strings.Split(pathPart, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repository remote URL format %q: expected owner/repo", rawURL)
	}

	owner = parts[len(parts)-2]
	repo = parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("invalid repository remote URL %q: owner or repo is empty", rawURL)
	}

	return owner, repo, nil
}

// DetectRepoInfo inspects git configuration in dir to detect remote origin owner/repo and the active branch.
func DetectRepoInfo(dir string) (owner, repo, currentBranch string, err error) {
	if dir == "" {
		dir = "."
	}

	// 1. Get git remote origin URL
	cmd := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		cmdFallback := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
		out, err = cmdFallback.Output()
		if err != nil {
			return "", "", "", fmt.Errorf("failed to get git remote origin in %s: %w", dir, err)
		}
	}
	remoteURL := strings.TrimSpace(string(out))
	if remoteURL == "" {
		return "", "", "", fmt.Errorf("empty remote origin URL in %s", dir)
	}

	owner, repo, err = ParseRemoteURL(remoteURL)
	if err != nil {
		return "", "", "", err
	}

	// 2. Get current branch name
	cmdBranch := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := cmdBranch.Output()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get current git branch in %s: %w", dir, err)
	}
	currentBranch = strings.TrimSpace(string(branchOut))
	if currentBranch == "HEAD" {
		cmdSym := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD")
		if symOut, symErr := cmdSym.Output(); symErr == nil {
			currentBranch = strings.TrimSpace(string(symOut))
		}
	}

	return owner, repo, currentBranch, nil
}

// NewClient creates a new Forgejo PR Client from Config.
func NewClient(cfg *Config, opts ...ClientOption) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("pr config cannot be nil")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("pr BaseURL cannot be empty")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("pr Token cannot be empty")
	}

	c := &Client{
		cfg:    cfg,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}

	giteaOpts := []gitea.ClientOption{
		gitea.SetToken(cfg.Token),
		gitea.SetUserAgent("orbit-cli/1.0"),
		gitea.SetGiteaVersion(""), // Skip remote version ping on init, allow all operations
	}
	if c.httpClient != nil {
		giteaOpts = append(giteaOpts, gitea.SetHTTPClient(c.httpClient))
	}

	gc, err := gitea.NewClient(cfg.BaseURL, giteaOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Forgejo/Gitea client for %s: %w", cfg.BaseURL, err)
	}
	c.client = gc

	return c, nil
}

// NewClientWithToken creates a new Forgejo PR Client using raw baseURL and token strings.
func NewClientWithToken(baseURL, token string, opts ...ClientOption) (*Client, error) {
	return NewClient(&Config{BaseURL: baseURL, Token: token}, opts...)
}

// RawClient returns the underlying *gitea.Client.
func (c *Client) RawClient() *gitea.Client {
	return c.client
}

// CreatePR creates a new pull request on the repository.
func (c *Client) CreatePR(owner, repo, title, body, head, base string) (*PR, error) {
	if owner == "" || repo == "" {
		return nil, errors.New("owner and repo must not be empty")
	}
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("PR title must not be empty")
	}
	if strings.TrimSpace(head) == "" {
		return nil, errors.New("head branch must not be empty")
	}
	if strings.TrimSpace(base) == "" {
		base = "main"
	}

	c.logger.Debug("creating pull request",
		slog.String("owner", owner),
		slog.String("repo", repo),
		slog.String("title", title),
		slog.String("head", head),
		slog.String("base", base),
	)

	opt := gitea.CreatePullRequestOption{
		Title: title,
		Body:  body,
		Head:  head,
		Base:  base,
	}

	pr, resp, err := c.client.CreatePullRequest(owner, repo, opt)
	if err != nil {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		return nil, fmt.Errorf("failed to create PR in %s/%s (status %d): %w", owner, repo, statusCode, err)
	}
	return pr, nil
}

// MergePR merges a pull request using the specified merge method (merge, rebase, squash, rebase-merge).
// Defaults to "merge" if empty.
func (c *Client) MergePR(owner, repo string, index int64, method string) error {
	if owner == "" || repo == "" {
		return errors.New("owner and repo must not be empty")
	}
	if index <= 0 {
		return fmt.Errorf("invalid PR index: %d", index)
	}

	m := strings.ToLower(strings.TrimSpace(method))
	var style gitea.MergeStyle
	switch m {
	case "squash":
		style = gitea.MergeStyleSquash
	case "rebase":
		style = gitea.MergeStyleRebase
	case "rebase-merge":
		style = gitea.MergeStyleRebaseMerge
	case "merge", "":
		style = gitea.MergeStyleMerge
	default:
		return fmt.Errorf("invalid merge method %q: must be one of 'merge', 'squash', 'rebase', 'rebase-merge'", method)
	}

	c.logger.Debug("merging pull request",
		slog.String("owner", owner),
		slog.String("repo", repo),
		slog.Int64("index", index),
		slog.String("style", string(style)),
	)

	opt := gitea.MergePullRequestOption{
		Style: style,
	}
	success, resp, err := c.client.MergePullRequest(owner, repo, index, opt)
	if err != nil {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		return fmt.Errorf("failed to merge PR #%d in %s/%s (status %d): %w", index, owner, repo, statusCode, err)
	}
	if !success {
		return fmt.Errorf("merge PR #%d in %s/%s was rejected by server", index, owner, repo)
	}
	return nil
}

// GetPR retrieves a pull request by its index.
func (c *Client) GetPR(owner, repo string, index int64) (*PR, error) {
	if owner == "" || repo == "" {
		return nil, errors.New("owner and repo must not be empty")
	}
	if index <= 0 {
		return nil, fmt.Errorf("invalid PR index: %d", index)
	}

	c.logger.Debug("getting pull request",
		slog.String("owner", owner),
		slog.String("repo", repo),
		slog.Int64("index", index),
	)

	pr, resp, err := c.client.GetPullRequest(owner, repo, index)
	if err != nil {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		return nil, fmt.Errorf("failed to get PR #%d in %s/%s (status %d): %w", index, owner, repo, statusCode, err)
	}
	return pr, nil
}

// ListOpenPRs lists all open pull requests for a repository.
func (c *Client) ListOpenPRs(owner, repo string) ([]*PR, error) {
	if owner == "" || repo == "" {
		return nil, errors.New("owner and repo must not be empty")
	}

	c.logger.Debug("listing open pull requests",
		slog.String("owner", owner),
		slog.String("repo", repo),
	)

	opt := gitea.ListPullRequestsOptions{
		State: gitea.StateOpen,
	}
	prs, resp, err := c.client.ListRepoPullRequests(owner, repo, opt)
	if err != nil {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		return nil, fmt.Errorf("failed to list open PRs in %s/%s (status %d): %w", owner, repo, statusCode, err)
	}
	return prs, nil
}
