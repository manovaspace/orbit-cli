// Package envfiles writes the dev env files the selected repositories declare. Each repository's
// orbit.yaml lists env outputs (schema and write path); without a list, a repository with a
// .env.schema.yaml at its root gets one output, .env. Values come from from: sources (the local
// Postgres role password, Mailpit), then default. Keys already in a file are kept as they are;
// only missing keys are added. A file is written 0600.
package envfiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/env/domain"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/services"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const (
	partName      = "env"
	defaultSchema = ".env.schema.yaml"
	defaultWrite  = ".env"
	devEnv        = "dev"
	postgresAddr  = "127.0.0.1:10000"
	mailURL       = "smtp://127.0.0.1:10024"
	fromPostgres  = "postgres:"
	fromMail      = "mail"
	fromSecret    = "secret:"
	secretsFile   = "local.env"
)

// part writes the env files. secretsPath locates the local secrets file; production reads
// $XDG_CONFIG_HOME/orbit/local.env, tests inject their own.
type part struct {
	secretsPath func() (string, error)
}

// New returns the env part. It needs services, which create the Postgres roles whose passwords
// the postgres: sources read.
func New() engine.Part {
	return part{secretsPath: productionSecretsPath}
}

func (part) Name() string    { return partName }
func (part) Needs() []string { return []string{"services"} }

// productionSecretsPath is the same local.env the services part writes.
func productionSecretsPath() (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "orbit", secretsFile), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", clierr.Wrap(err, clierr.Failed("cannot find the home directory"))
	}
	return filepath.Join(home, ".config", "orbit", secretsFile), nil
}

// secretCache reads the local secrets file at most once per Plan.
type secretCache struct {
	path   func() (string, error)
	loaded bool
	values map[string]string
}

func (c *secretCache) get() (map[string]string, error) {
	if c.loaded {
		return c.values, nil
	}
	path, err := c.path()
	if err != nil {
		return nil, err
	}
	values, err := services.LocalSecrets(path)
	if err != nil {
		return nil, err
	}
	c.loaded = true
	c.values = values
	return values, nil
}

// output is one env file of one repository, with what it needs to resolve values.
type output struct {
	repo     string // workspace path of the repository, e.g. clients/alpha
	dir      string // absolute repository directory
	display  string // repo/write, for messages
	write    string // write path relative to the repository
	rs       spec.RepoSpec
	external bool
	secrets  *secretCache
}

// Plan reads every selected repository's env outputs and plans one write, update or restriction
// per file. Nothing is written by Plan.
func (p part) Plan(_ context.Context, s *engine.State) (engine.Plan, error) {
	selected, err := s.Selected()
	if err != nil {
		return engine.Plan{}, err
	}
	secrets := &secretCache{path: p.secretsPath}
	var plan engine.Plan
	for _, target := range selected {
		if err := p.planRepo(s, target, secrets, &plan); err != nil {
			return engine.Plan{}, err
		}
	}
	return plan, nil
}

// planRepo plans the env outputs of one repository. Without an env list, a root schema is the
// default output.
func (p part) planRepo(s *engine.State, target manifest.RepoTarget, secrets *secretCache, plan *engine.Plan) error {
	dir := filepath.Join(s.Root, target.Path)
	rs, _, err := spec.LoadRepoSpec(dir)
	if err != nil {
		return err
	}
	outs := rs.Env
	if len(outs) == 0 {
		_, err := s.FS.Stat(filepath.Join(dir, defaultSchema))
		switch {
		case err == nil:
			outs = []spec.EnvOutput{{Schema: defaultSchema, Write: defaultWrite}}
		case errors.Is(err, fs.ErrNotExist):
		default:
			return clierr.Wrap(err, clierr.Failed("cannot read "+filepath.Join(dir, defaultSchema)))
		}
	}
	for _, out := range outs {
		o := &output{
			repo:     target.Path,
			dir:      dir,
			display:  target.Path + "/" + out.Write,
			write:    out.Write,
			rs:       rs,
			external: s.Machine.Services == "external",
			secrets:  secrets,
		}
		if err := p.planOutput(s, out, o, plan); err != nil {
			return err
		}
	}
	return nil
}

// planOutput plans one env file: its schema is read, every missing key is resolved, and the file
// is written, updated or restricted.
func (p part) planOutput(s *engine.State, out spec.EnvOutput, o *output, plan *engine.Plan) error {
	for _, check := range []struct{ kind, rel string }{{"schema", out.Schema}, {"write", out.Write}} {
		if msg := pathProblem(check.rel); msg != "" {
			plan.Findings = append(plan.Findings, engine.Finding{
				Part:     partName,
				Subject:  o.repo,
				Message:  fmt.Sprintf("%s/orbit.yaml env %s path %s", o.repo, check.kind, msg),
				Fix:      "use a path inside the repository",
				Severity: engine.SeverityError,
			})
			return nil
		}
	}

	schemaPath := filepath.Join(o.dir, out.Schema)
	data, err := s.FS.ReadFile(schemaPath)
	if errors.Is(err, fs.ErrNotExist) {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  o.repo,
			Message:  fmt.Sprintf("schema %s/%s not found; its env file is not written", o.repo, out.Schema),
			Fix:      "clone the repository or fix the env schema path in orbit.yaml",
			Severity: engine.SeverityWarn,
		})
		return nil
	}
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot read "+schemaPath))
	}
	schema, err := domain.ParseSchema(data)
	if err != nil {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  o.repo,
			Message:  fmt.Sprintf("%s/%s: %v", o.repo, out.Schema, err),
			Fix:      "fix the schema file",
			Severity: engine.SeverityError,
		})
		return nil
	}

	writePath := filepath.Join(o.dir, out.Write)
	exists, mode, content, err := readExisting(s.FS, writePath)
	if err != nil {
		return err
	}
	existing := map[string]string{}
	if exists {
		existing, err = domain.ParseEnvContent(content)
		if err != nil {
			plan.Findings = append(plan.Findings, engine.Finding{
				Part:     partName,
				Subject:  o.repo,
				Message:  fmt.Sprintf("%s is malformed: %v", o.display, err),
				Fix:      "fix the env file by hand",
				Severity: engine.SeverityError,
			})
			return nil
		}
	}

	values := map[string]string{}
	comments := map[string]string{}
	for _, v := range schema.Variables {
		if !appliesToDev(v.For) {
			continue
		}
		if _, ok := existing[v.Name]; ok {
			continue
		}
		val, hasValue, found, err := o.resolve(v)
		if err != nil {
			return err
		}
		plan.Findings = append(plan.Findings, found...)
		if hasValue {
			values[v.Name] = val
			comments[v.Name] = v.Description
			continue
		}
		if v.Required && len(found) == 0 && !strings.HasPrefix(v.From, fromSecret) {
			plan.Findings = append(plan.Findings, engine.Finding{
				Part:     partName,
				Subject:  v.Name,
				Message:  fmt.Sprintf("%s is required in %s but has no value", v.Name, o.display),
				Fix:      fmt.Sprintf("set %s by hand in %s", v.Name, o.display),
				Severity: engine.SeverityError,
			})
		}
	}

	switch {
	case len(values) > 0 && exists:
		next := joinContent(content, domain.RenderEnv(values, comments))
		plan.Actions = append(plan.Actions, engine.Action{
			Part:    partName,
			Subject: o.repo,
			Summary: "Update " + o.display,
			Do: func(context.Context) error {
				return writeAtomic(s.FS, writePath, []byte(next))
			},
		})
	case len(values) > 0:
		next := domain.RenderEnv(values, comments)
		plan.Actions = append(plan.Actions, engine.Action{
			Part:    partName,
			Subject: o.repo,
			Summary: "Write " + o.display,
			Do: func(context.Context) error {
				if err := s.FS.MkdirAll(filepath.Dir(writePath), 0o700); err != nil {
					return clierr.Wrap(err, clierr.Failed("cannot create "+filepath.Dir(writePath)))
				}
				return writeAtomic(s.FS, writePath, []byte(next))
			},
		})
	case exists && mode&^0o600 != 0:
		plan.Actions = append(plan.Actions, engine.Action{
			Part:    partName,
			Subject: o.repo,
			Summary: "Restrict " + o.display + " to 0600",
			Do: func(context.Context) error {
				return writeAtomic(s.FS, writePath, []byte(content))
			},
		})
	}
	return nil
}

// resolve returns the value of one variable that is missing from its file. A from: source that
// yields no value is final: its Default is not used. found holds the warnings and errors of the
// resolution.
func (o *output) resolve(v domain.VariableDef) (string, bool, []engine.Finding, error) {
	finding := func(sev engine.Severity, msg, fix string) engine.Finding {
		return engine.Finding{Part: partName, Subject: v.Name, Message: msg, Fix: fix, Severity: sev}
	}
	switch {
	case v.From == "":
		if v.Default != "" {
			return v.Default, true, nil, nil
		}
		return "", false, nil, nil
	case v.From == fromMail:
		return mailURL, true, nil, nil
	case strings.HasPrefix(v.From, fromPostgres):
		return o.postgres(v, strings.TrimPrefix(v.From, fromPostgres))
	case strings.HasPrefix(v.From, fromSecret):
		desc := v.Description
		if desc == "" {
			desc = "the feature it serves"
		}
		return "", false, []engine.Finding{finding(engine.SeverityWarn,
			fmt.Sprintf("%s not granted; %s stays off", v.Name, desc),
			"nothing to do in phase 1")}, nil
	default:
		return "", false, []engine.Finding{finding(engine.SeverityError,
			fmt.Sprintf("%s has unknown from: %q", v.Name, v.From),
			"use from: postgres:<database>, mail or secret:<name>")}, nil
	}
}

// postgres builds the connection URL of database db for the role that orbit.yaml declares for it.
func (o *output) postgres(v domain.VariableDef, db string) (string, bool, []engine.Finding, error) {
	var declared *spec.DatabaseSpec
	if o.rs.Services.Postgres != nil {
		for i := range o.rs.Services.Postgres.Databases {
			if o.rs.Services.Postgres.Databases[i].Name == db {
				declared = &o.rs.Services.Postgres.Databases[i]
				break
			}
		}
	}
	if declared == nil {
		return "", false, []engine.Finding{{
			Part:     partName,
			Subject:  v.Name,
			Message:  fmt.Sprintf("%s names database %s, which %s/orbit.yaml does not declare", v.Name, db, o.repo),
			Fix:      "declare the database under services.postgres.databases",
			Severity: engine.SeverityError,
		}}, nil
	}
	if o.external {
		return "", false, []engine.Finding{{
			Part:     partName,
			Subject:  v.Name,
			Message:  fmt.Sprintf("set %s by hand in %s", v.Name, o.display),
			Fix:      "point it at your own Postgres, or set services: native in machine.yaml",
			Severity: engine.SeverityWarn,
		}}, nil
	}
	role := declared.Role
	if role == "" {
		role = declared.Name
	}
	secrets, err := o.secrets.get()
	if err != nil {
		return "", false, nil, err
	}
	password := secrets["PG_"+strings.ToUpper(role)+"_PASSWORD"]
	if password == "" {
		return "", false, []engine.Finding{{
			Part:     partName,
			Subject:  v.Name,
			Message:  fmt.Sprintf("no password for role %s; run o setup again after services", role),
			Fix:      "run o setup so the services part creates the role",
			Severity: engine.SeverityError,
		}}, nil
	}
	return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", role, url.QueryEscape(password), postgresAddr, db), true, nil, nil
}

// appliesToDev reports whether a variable belongs in the dev env file: a variable with a For list
// that does not name dev is build-only and skipped.
func appliesToDev(forList []string) bool {
	if len(forList) == 0 {
		return true
	}
	for _, env := range forList {
		if env == devEnv {
			return true
		}
	}
	return false
}

// pathProblem returns why rel may not name a file inside its repository, or "" when it may. It
// rejects empty, absolute and escaping paths.
func pathProblem(rel string) string {
	switch {
	case rel == "":
		return "is empty"
	case filepath.IsAbs(rel) || strings.HasPrefix(rel, "/"):
		return "escapes the repository"
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "escapes the repository"
	}
	return ""
}

// readExisting reports whether path exists, with its permission bits and content.
func readExisting(fsys engine.FS, path string) (bool, fs.FileMode, string, error) {
	info, err := fsys.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, 0, "", nil
	}
	if err != nil {
		return false, 0, "", clierr.Wrap(err, clierr.Failed("cannot read "+path))
	}
	data, err := fsys.ReadFile(path)
	if err != nil {
		return false, 0, "", clierr.Wrap(err, clierr.Failed("cannot read "+path))
	}
	return true, info.Mode().Perm(), string(data), nil
}

// joinContent appends add to content, starting a new line first when content lacks one.
func joinContent(content, add string) string {
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + add
}

// writeAtomic writes data to a temporary file beside path with mode 0600 and renames it over
// path, so the final file is 0600 whatever mode it had before.
func writeAtomic(fsys engine.FS, path string, data []byte) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".orbit-tmp")
	_ = fsys.Remove(tmp)
	if err := fsys.WriteFile(tmp, data, 0o600); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot write "+path))
	}
	if err := fsys.Rename(tmp, path); err != nil {
		_ = fsys.Remove(tmp)
		return clierr.Wrap(err, clierr.Failed("cannot replace "+path))
	}
	return nil
}
