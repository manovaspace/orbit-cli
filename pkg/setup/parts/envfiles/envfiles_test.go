package envfiles

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const (
	testPass  = "test-only-password-7f3a"
	schemaRel = ".env.schema.yaml"
)

// schemaText declares one variable per resolution rule the tests exercise.
const schemaText = `version: "1"
variables:
  - name: DATABASE_URL
    type: url
    from: postgres:app_api
    description: Local Postgres
    required: true
  - name: MAIL_URL
    type: url
    from: mail
    description: Mailpit SMTP
  - name: APP_PORT
    type: integer
    default: 3000
    description: HTTP port
    required: true
  - name: BUILD_ONLY
    type: string
    default: build
    description: Build flag
    for: [build]
    required: true
`

// orbitWithDB declares app_api and the default env output.
const orbitWithDB = "version: 1\nservices:\n  postgres:\n    databases:\n      - name: app_api\n"

// testManifest selects one repository, clients/alpha, through project app.
func testManifest() *manifest.WorkspaceManifest {
	return &manifest.WorkspaceManifest{
		Groups: map[string]manifest.GroupConfig{
			"clients": {Repositories: []manifest.RepoConfig{{
				Name: "alpha", Path: "clients/alpha", RemoteURL: "git@github.com:manovaspace/alpha.git", DefaultBranch: "main",
			}}},
		},
		Projects: map[string]manifest.ProjectConfig{
			"app": {Repos: []string{"clients/alpha"}},
		},
	}
}

// harness builds a real temporary workspace with one repository, clients/alpha, and a State in the
// given services mode. The secrets file holds secretsText (dummy values only). orbit.yaml is read
// through os by spec.LoadRepoSpec, so the repository must be on disk.
func harness(t *testing.T, services, secretsText string) (string, *engine.State, part) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "clients", "alpha")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	secretsFile := filepath.Join(dir, "config", "local.env")
	if err := os.MkdirAll(filepath.Dir(secretsFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretsFile, []byte(secretsText), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &engine.State{
		Root:     dir,
		Machine:  spec.Machine{Projects: []string{"app"}, Services: services},
		Manifest: testManifest(),
		FS:       engine.OSFS{},
	}
	p := part{secretsPath: func() (string, error) { return secretsFile, nil }}
	return repo, s, p
}

// put writes a repository file with an exact mode.
func put(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func content(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func goodSecrets() string {
	return "PG_APP_API_PASSWORD=" + testPass + "\n"
}

func plan(t *testing.T, p part, s *engine.State) engine.Plan {
	t.Helper()
	got, err := p.Plan(context.Background(), s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return got
}

func apply(t *testing.T, got engine.Plan) {
	t.Helper()
	for _, a := range got.Actions {
		if a.Do == nil {
			t.Fatalf("action %q has no Do", a.Summary)
		}
		if err := a.Do(context.Background()); err != nil {
			t.Fatalf("action %q: %v", a.Summary, err)
		}
	}
}

func findings(got engine.Plan, sev engine.Severity) []engine.Finding {
	var out []engine.Finding
	for _, f := range got.Findings {
		if f.Severity == sev {
			out = append(out, f)
		}
	}
	return out
}

func summaries(got engine.Plan) []string {
	var out []string
	for _, a := range got.Actions {
		out = append(out, a.Summary)
	}
	return out
}

func lines(content string) []string {
	return strings.Split(strings.TrimSpace(content), "\n")
}

func hasLine(content, want string) bool {
	for _, l := range lines(content) {
		if l == want {
			return true
		}
	}
	return false
}

func TestEnvFillsPostgresFromLocalSecret(t *testing.T) {
	repo, s, p := harness(t, "native", goodSecrets())
	put(t, repo+"/orbit.yaml", orbitWithDB, 0o644)
	put(t, repo+"/"+schemaRel, schemaText, 0o644)

	got := plan(t, p, s)
	if len(findings(got, engine.SeverityError)) != 0 {
		t.Fatalf("unexpected errors: %+v", got.Findings)
	}
	if want := []string{"Write clients/alpha/.env"}; strings.Join(summaries(got), "|") != strings.Join(want, "|") {
		t.Fatalf("actions = %v, want %v", summaries(got), want)
	}
	apply(t, got)
	content := content(t, repo+"/.env")
	wantURL := "DATABASE_URL=postgres://app_api:" + testPass + "@127.0.0.1:10000/app_api?sslmode=disable"
	if !hasLine(content, wantURL) {
		t.Fatalf(".env lacks the postgres URL line; content has %d lines", len(lines(content)))
	}
	if !hasLine(content, "MAIL_URL=smtp://127.0.0.1:10024") {
		t.Fatalf(".env lacks the mail line")
	}
	if !hasLine(content, "APP_PORT=3000") {
		t.Fatalf(".env lacks the default APP_PORT line")
	}
}

func TestEnvKeepsHandSetKeys(t *testing.T) {
	repo, s, p := harness(t, "native", goodSecrets())
	put(t, repo+"/orbit.yaml", orbitWithDB, 0o600)
	put(t, repo+"/"+schemaRel, schemaText, 0o644)
	existing := "# hand written\nDATABASE_URL=postgres://hand:set@db.example/x\nEXTRA=keep-me\n"
	put(t, repo+"/.env", existing, 0o600)

	got := plan(t, p, s)
	if want := []string{"Update clients/alpha/.env"}; strings.Join(summaries(got), "|") != strings.Join(want, "|") {
		t.Fatalf("actions = %v, want %v", summaries(got), want)
	}
	apply(t, got)
	content := content(t, repo+"/.env")
	if !strings.HasPrefix(content, existing) {
		t.Fatalf("existing content was changed; got %q", content)
	}
	if n := strings.Count(content, "DATABASE_URL="); n != 1 {
		t.Fatalf("DATABASE_URL appears %d times, want 1", n)
	}
	if strings.Contains(content, testPass) {
		t.Fatalf("hand-set DATABASE_URL was overwritten by the local secret")
	}
	if !hasLine(content, "# HTTP port") || !hasLine(content, "APP_PORT=3000") {
		t.Fatalf("missing key APP_PORT with its description comment; content %q", content)
	}
	if !hasLine(content, "MAIL_URL=smtp://127.0.0.1:10024") {
		t.Fatalf("missing key MAIL_URL")
	}
}

func TestEnvSkipsBuildOnlyVars(t *testing.T) {
	repo, s, p := harness(t, "native", goodSecrets())
	put(t, repo+"/orbit.yaml", orbitWithDB, 0o644)
	put(t, repo+"/"+schemaRel, schemaText, 0o644)

	got := plan(t, p, s)
	if n := len(findings(got, engine.SeverityError)); n != 0 {
		t.Fatalf("build-only required variable produced %d errors: %+v", n, got.Findings)
	}
	apply(t, got)
	if strings.Contains(content(t, repo+"/.env"), "BUILD_ONLY") {
		t.Fatalf("build-only variable was written to the dev env file")
	}
}

func TestEnvSecretIsWarningNotFailure(t *testing.T) {
	repo, s, p := harness(t, "native", goodSecrets())
	put(t, repo+"/orbit.yaml", orbitWithDB, 0o644)
	put(t, repo+"/"+schemaRel, "version: \"1\"\nvariables:\n  - name: SIGNING_KEY\n    type: secret\n    from: secret:signing\n    description: request signing\n    required: true\n", 0o644)

	got := plan(t, p, s)
	if n := len(findings(got, engine.SeverityError)); n != 0 {
		t.Fatalf("ungranted secret failed the part: %+v", got.Findings)
	}
	warns := findings(got, engine.SeverityWarn)
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "SIGNING_KEY not granted") {
		t.Fatalf("warnings = %+v, want one 'SIGNING_KEY not granted' warning", warns)
	}
	apply(t, got)
	if strings.Contains(content(t, repo+"/.env"), "SIGNING_KEY=") {
		t.Fatalf("ungranted secret was written with a value")
	}
}

func TestEnvFileMode0600(t *testing.T) {
	t.Run("new file", func(t *testing.T) {
		repo, s, p := harness(t, "native", goodSecrets())
		put(t, repo+"/orbit.yaml", orbitWithDB, 0o644)
		put(t, repo+"/"+schemaRel, schemaText, 0o644)
		apply(t, plan(t, p, s))
		if m := mode(t, repo+"/.env"); m != 0o600 {
			t.Fatalf("new .env mode = %o, want 600", m)
		}
	})
	t.Run("update replaces loose mode", func(t *testing.T) {
		repo, s, p := harness(t, "native", goodSecrets())
		put(t, repo+"/orbit.yaml", orbitWithDB, 0o644)
		put(t, repo+"/"+schemaRel, schemaText, 0o644)
		put(t, repo+"/.env", "EXTRA=1\n", 0o644)
		apply(t, plan(t, p, s))
		if m := mode(t, repo+"/.env"); m != 0o600 {
			t.Fatalf("updated .env mode = %o, want 600", m)
		}
	})
	t.Run("nothing to add restricts loose mode", func(t *testing.T) {
		repo, s, p := harness(t, "native", goodSecrets())
		put(t, repo+"/orbit.yaml", "version: 1\nenv:\n  - schema: .env.schema.yaml\n    write: .env\n", 0o644)
		put(t, repo+"/"+schemaRel, "version: \"1\"\nvariables:\n  - name: APP_PORT\n    type: integer\n    default: 3000\n", 0o644)
		put(t, repo+"/.env", "APP_PORT=3000\n", 0o644)
		got := plan(t, p, s)
		if want := []string{"Restrict clients/alpha/.env to 0600"}; strings.Join(summaries(got), "|") != strings.Join(want, "|") {
			t.Fatalf("actions = %v, want %v", summaries(got), want)
		}
		apply(t, got)
		if m := mode(t, repo+"/.env"); m != 0o600 {
			t.Fatalf("restricted .env mode = %o, want 600", m)
		}
		if content(t, repo+"/.env") != "APP_PORT=3000\n" {
			t.Fatalf("restrict changed the content")
		}
	})
}

func TestEnvDefaultOutputWhenNoRepoSpec(t *testing.T) {
	repo, s, p := harness(t, "native", goodSecrets())
	put(t, repo+"/"+schemaRel, "version: \"1\"\nvariables:\n  - name: APP_PORT\n    type: integer\n    default: 3000\n", 0o644)

	got := plan(t, p, s)
	if want := []string{"Write clients/alpha/.env"}; strings.Join(summaries(got), "|") != strings.Join(want, "|") {
		t.Fatalf("actions = %v, want %v", summaries(got), want)
	}
	apply(t, got)
	if !hasLine(content(t, repo+"/.env"), "APP_PORT=3000") {
		t.Fatalf("default output lacks APP_PORT")
	}
}

func TestEnvSecondRunNoActions(t *testing.T) {
	repo, s, p := harness(t, "native", goodSecrets())
	put(t, repo+"/orbit.yaml", orbitWithDB, 0o644)
	put(t, repo+"/"+schemaRel, schemaText, 0o644)
	apply(t, plan(t, p, s))

	got := plan(t, p, s)
	if len(got.Actions) != 0 {
		t.Fatalf("second run actions = %v, want none", summaries(got))
	}
	if n := len(findings(got, engine.SeverityError)); n != 0 {
		t.Fatalf("second run errors: %+v", got.Findings)
	}
}

func TestEnvRefusesEscapingPaths(t *testing.T) {
	for _, tc := range []struct{ name, orbit string }{
		{"schema escapes", "version: 1\nenv:\n  - schema: ../secret.yaml\n    write: .env\n"},
		{"schema absolute", "version: 1\nenv:\n  - schema: /etc/schema.yaml\n    write: .env\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, s, p := harness(t, "native", goodSecrets())
			put(t, repo+"/orbit.yaml", tc.orbit, 0o644)
			put(t, repo+"/"+schemaRel, schemaText, 0o644)
			got := plan(t, p, s)
			errs := findings(got, engine.SeverityError)
			if len(errs) != 1 || !strings.Contains(errs[0].Message, "escapes the repository") {
				t.Fatalf("errors = %+v, want one 'escapes the repository' error", errs)
			}
			if len(got.Actions) != 0 {
				t.Fatalf("actions = %v, want none", summaries(got))
			}
		})
	}
}

func TestEnvRequiredWithoutValueIsError(t *testing.T) {
	repo, s, p := harness(t, "native", goodSecrets())
	put(t, repo+"/orbit.yaml", "version: 1\nenv:\n  - schema: .env.schema.yaml\n    write: .env\n", 0o644)
	put(t, repo+"/"+schemaRel, "version: \"1\"\nvariables:\n  - name: API_KEY\n    type: string\n    required: true\n", 0o644)
	got := plan(t, p, s)
	errs := findings(got, engine.SeverityError)
	if len(errs) != 1 || errs[0].Message != "API_KEY is required in clients/alpha/.env but has no value" {
		t.Fatalf("errors = %+v", errs)
	}
}

func TestEnvNativeMissingPasswordIsError(t *testing.T) {
	repo, s, p := harness(t, "native", "")
	put(t, repo+"/orbit.yaml", orbitWithDB, 0o644)
	put(t, repo+"/"+schemaRel, schemaText, 0o644)
	got := plan(t, p, s)
	errs := findings(got, engine.SeverityError)
	if len(errs) != 1 || errs[0].Message != "no password for role app_api; run o setup again after services" {
		t.Fatalf("errors = %+v", errs)
	}
	apply(t, got)
	if strings.Contains(content(t, repo+"/.env"), "postgres://") {
		t.Fatalf("a URL without a password was written")
	}
}

func TestEnvExternalPostgresWarnsAndWritesNoURL(t *testing.T) {
	repo, s, p := harness(t, "external", "")
	put(t, repo+"/orbit.yaml", orbitWithDB, 0o644)
	put(t, repo+"/"+schemaRel, "version: \"1\"\nvariables:\n  - name: DATABASE_URL\n    type: url\n    from: postgres:app_api\n    description: Local Postgres\n", 0o644)
	got := plan(t, p, s)
	if n := len(findings(got, engine.SeverityError)); n != 0 {
		t.Fatalf("external postgres errors: %+v", got.Findings)
	}
	warns := findings(got, engine.SeverityWarn)
	if len(warns) != 1 || warns[0].Message != "set DATABASE_URL by hand in clients/alpha/.env" {
		t.Fatalf("warnings = %+v", warns)
	}
	if len(got.Actions) != 0 {
		t.Fatalf("external with nothing to write produced actions %v", summaries(got))
	}
}

func TestEnvUnknownFromIsError(t *testing.T) {
	repo, s, p := harness(t, "native", goodSecrets())
	put(t, repo+"/orbit.yaml", "version: 1\nenv:\n  - schema: .env.schema.yaml\n    write: .env\n", 0o644)
	put(t, repo+"/"+schemaRel, "version: \"1\"\nvariables:\n  - name: WEIRD\n    type: string\n    from: carrier-pigeon\n", 0o644)
	got := plan(t, p, s)
	errs := findings(got, engine.SeverityError)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "WEIRD") {
		t.Fatalf("errors = %+v, want one error naming WEIRD", errs)
	}
}
