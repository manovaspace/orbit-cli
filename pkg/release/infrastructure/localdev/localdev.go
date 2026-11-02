// Package localdev implements the local release-tier environment over the
// existing dev Postgres, Redis and NATS services. It starts no containers and
// never prints a connection URL or password.
package localdev

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/env"
	"github.com/manovaspace/orbit-cli/pkg/release/application"
	releasedomain "github.com/manovaspace/orbit-cli/pkg/release/domain"
	"github.com/manovaspace/orbit-cli/pkg/runner"
)

// Local resolves the dev service URLs for one repository and creates the
// throwaway database through psql on PATH.
type Local struct {
	Root   string
	Runner runner.Runner
	Files  application.Files
}

// New builds a Local.
func New(root string, r runner.Runner, files application.Files) *Local {
	return &Local{Root: root, Runner: r, Files: files}
}

type settings struct {
	databaseURL string
	redisURL    string
	natsURL     string
}

// Reachable checks that the dev Postgres accepts connections. Redis and NATS
// are only passed to the test command; they are not probed here because the
// release tier itself reports their absence.
func (l *Local) Reachable(ctx context.Context, repoDir string) error {
	cfg := l.settings(repoDir)
	if cfg.databaseURL == "" {
		return clierr.Unverified("dev Postgres URL is not configured; set DATABASE_URL in the repository or workspace .env")
	}
	res, err := l.psql(ctx, cfg, "-tAc", "SELECT 1")
	if err != nil {
		return clierr.Unverified("dev Postgres is unreachable: " + err.Error())
	}
	if strings.TrimSpace(res.Stdout) == "" {
		return clierr.Unverified("dev Postgres returned no result")
	}
	return nil
}

// CreateDatabase creates the throwaway database.
func (l *Local) CreateDatabase(ctx context.Context, repoDir, name string) error {
	if !releasedomain.ValidDatabaseName(name) {
		return fmt.Errorf("refusing invalid throwaway database name")
	}
	cfg := l.settings(repoDir)
	if cfg.databaseURL == "" {
		return fmt.Errorf("dev Postgres URL is not configured")
	}
	_, err := l.psql(ctx, cfg, "-v", "ON_ERROR_STOP=1", "-c", "CREATE DATABASE "+name)
	return err
}

// DropDatabase removes the throwaway database. FORCE closes any connection the
// test command left open.
func (l *Local) DropDatabase(ctx context.Context, repoDir, name string) error {
	if !releasedomain.ValidDatabaseName(name) {
		return fmt.Errorf("refusing invalid throwaway database name")
	}
	cfg := l.settings(repoDir)
	if cfg.databaseURL == "" {
		return fmt.Errorf("dev Postgres URL is not configured")
	}
	_, err := l.psql(ctx, cfg, "-v", "ON_ERROR_STOP=1", "-c", "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	return err
}

// Env returns the release-tier environment for database. The dev URLs are
// rewritten, never copied: the test command gets the throwaway database and
// the same Redis and NATS endpoints.
func (l *Local) Env(repoDir, database string) (map[string]string, error) {
	cfg := l.settings(repoDir)
	if cfg.databaseURL == "" {
		return nil, fmt.Errorf("dev Postgres URL is not configured; set DATABASE_URL in the repository or workspace .env")
	}
	testURL, err := releasedomain.ReplaceDatabaseName(cfg.databaseURL, database)
	if err != nil {
		return nil, err
	}
	env := map[string]string{
		"CI_TIER":           "release",
		"TEST_DATABASE_URL": testURL,
	}
	if cfg.redisURL != "" {
		env["TEST_REDIS_URL"] = cfg.redisURL
		env["BUS_REDIS_URL"] = cfg.redisURL
	}
	if cfg.natsURL != "" {
		env["TEST_NATS_URL"] = cfg.natsURL
		env["BUS_NATS_URL"] = cfg.natsURL
	}
	return env, nil
}

func (l *Local) settings(repoDir string) settings {
	values := map[string]string{}
	for _, path := range l.envFiles(repoDir) {
		data, err := l.Files.ReadFile(path)
		if err != nil {
			continue
		}
		for key, value := range parseEnv(string(data)) {
			setIfMissing(values, key, value)
		}
	}
	for _, path := range l.schemaFiles(repoDir) {
		loadSchemaDefaults(values, path)
	}
	for _, key := range []string{
		"DATABASE_URL", "POSTGRES_URL", "DEV_DATABASE_URL", "TEST_DATABASE_URL",
		"REDIS_URL", "TEST_REDIS_URL", "BUS_REDIS_URL",
		"NATS_URL", "TEST_NATS_URL", "BUS_NATS_URL",
	} {
		setIfMissing(values, key, os.Getenv(key))
	}

	return settings{
		databaseURL: pick(values, "DATABASE_URL", "POSTGRES_URL", "DEV_DATABASE_URL", "TEST_DATABASE_URL"),
		redisURL:    pick(values, "TEST_REDIS_URL", "REDIS_URL", "BUS_REDIS_URL"),
		natsURL:     pick(values, "TEST_NATS_URL", "NATS_URL", "BUS_NATS_URL"),
	}
}

func (l *Local) envFiles(repoDir string) []string {
	return uniquePaths([]string{
		filepath.Join(repoDir, ".env"),
		filepath.Join(repoDir, ".env.example"),
		filepath.Join(l.Root, ".env"),
		filepath.Join(l.Root, ".env.example"),
		filepath.Join(l.Root, "orbit", "orbit-infra", "dev", ".env"),
		filepath.Join(l.Root, "orbit", "orbit-infra", "dev", ".env.example"),
		filepath.Join(l.Root, "orbit", "orbit-infra", ".env"),
		filepath.Join(l.Root, "orbit", "orbit-infra", ".env.example"),
	})
}

func (l *Local) schemaFiles(repoDir string) []string {
	return uniquePaths([]string{
		filepath.Join(repoDir, ".env.schema.yaml"),
		filepath.Join(l.Root, ".env.schema.yaml"),
		filepath.Join(l.Root, "orbit", "orbit-infra", "dev", ".env.schema.yaml"),
	})
}

func (l *Local) psql(ctx context.Context, cfg settings, args ...string) (runner.Result, error) {
	full := append([]string{cfg.databaseURL}, args...)
	res, err := l.Runner.Run(ctx, "", "psql", full...)
	if err == nil {
		return res, nil
	}
	detail := strings.TrimSpace(res.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(res.Stdout)
	}
	if detail == "" {
		if res.ExitCode == 0 {
			detail = "psql is not available on PATH"
		} else {
			detail = fmt.Sprintf("psql exited with code %d", res.ExitCode)
		}
	}
	secrets := []string{cfg.databaseURL, cfg.redisURL, cfg.natsURL}
	return res, fmt.Errorf("%s", releasedomain.Redact(detail, secrets))
}

func loadSchemaDefaults(values map[string]string, path string) {
	schema, err := env.LoadSchema(path)
	if err != nil {
		return
	}
	for _, key := range []string{
		"DATABASE_URL", "POSTGRES_URL", "DEV_DATABASE_URL", "TEST_DATABASE_URL",
		"REDIS_URL", "TEST_REDIS_URL", "BUS_REDIS_URL",
		"NATS_URL", "TEST_NATS_URL", "BUS_NATS_URL",
	} {
		if variable := schema.GetVariable(key); variable != nil {
			setIfMissing(values, key, variable.Default)
		}
	}
}

func parseEnv(raw string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = unquote(strings.TrimSpace(value))
	}
	return values
}

func unquote(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func setIfMissing(values map[string]string, key, value string) {
	if key == "" || strings.TrimSpace(value) == "" {
		return
	}
	if _, exists := values[key]; !exists {
		values[key] = value
	}
}

func pick(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		cleaned := filepath.Clean(path)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
	}
	return out
}
