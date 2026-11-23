// Package services runs the local services the selected repositories declare. In native mode it
// keeps an orbit-owned Postgres 18 cluster on 127.0.0.1:10000 with one role and database per
// declared database, and Mailpit on macOS. In external mode it only checks that the ports answer.
// Container mode is not built yet. Nothing is started by Plan; Apply starts what the plan needs and
// leaves it running until `o down`.
package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"syscall"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const partName = "services"

// dialTimeout bounds every TCP probe of a local port.
const dialTimeout = 500 * time.Millisecond

// dialer probes a TCP address. The production value is net.DialTimeout; tests inject their own.
type dialer func(addr string, timeout time.Duration) error

// Service is one native local service.
type Service interface {
	Name() string
	Status(ctx context.Context) (Status, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Status is what a service reports. Owner is "orbit" when this machine's orbit runs it, "other"
// when something else listens on its port, and "" when nothing answers.
type Status struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	Owner   string `json:"owner"`
}

// deps holds the process seams the services read or act through. production returns the real ones.
type deps struct {
	dataDir     func() (string, error)
	secretsPath func() (string, error)
	stateDir    func() (string, error)
	user        func() (string, error)
	dial        dialer
	alive       func(pid int) bool
	kill        func(pid int, sig syscall.Signal) error
}

func production() deps {
	return deps{
		dataDir:     func() (string, error) { return orbitPath("XDG_DATA_HOME", ".local/share", "postgres") },
		secretsPath: func() (string, error) { return orbitPath("XDG_CONFIG_HOME", ".config", "local.env") },
		stateDir:    func() (string, error) { return orbitPath("XDG_STATE_HOME", ".local/state", "") },
		user: func() (string, error) {
			u, err := user.Current()
			if err != nil {
				return "", clierr.Wrap(err, clierr.Failed("cannot read the OS user name"))
			}
			return u.Username, nil
		},
		dial:  netDial,
		alive: processAlive,
		kill:  syscall.Kill,
	}
}

// orbitPath is $<env>/orbit/<leaf> when env is set, else ~/<fallback>/orbit/<leaf>. An empty leaf
// returns the orbit directory itself.
func orbitPath(env, fallback, leaf string) (string, error) {
	base := os.Getenv(env)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", clierr.Wrap(err, clierr.Failed("cannot find the home directory"))
		}
		base = filepath.Join(home, fallback)
	}
	if leaf == "" {
		return filepath.Join(base, "orbit"), nil
	}
	return filepath.Join(base, "orbit", leaf), nil
}

func netDial(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

// processAlive reports whether pid exists. EPERM means it exists under another user.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// postgres builds the cluster handle for s; bin is the resolved binary directory or "".
func (d deps) postgres(s *engine.State, bin string) (*postgres, error) {
	dataDir, err := d.dataDir()
	if err != nil {
		return nil, err
	}
	secrets, err := d.secretsPath()
	if err != nil {
		return nil, err
	}
	name, err := d.user()
	if err != nil {
		return nil, err
	}
	return &postgres{
		run:         s.Run,
		fs:          s.FS,
		bin:         bin,
		dataDir:     dataDir,
		secretsPath: secrets,
		user:        name,
		dial:        d.dial,
	}, nil
}

// mailpit builds the Mailpit handle for s.
func (d deps) mailpit(s *engine.State) (*mailpit, error) {
	stateDir, err := d.stateDir()
	if err != nil {
		return nil, err
	}
	return &mailpit{
		run:      s.Run,
		fs:       s.FS,
		stateDir: stateDir,
		dial:     d.dial,
		alive:    d.alive,
		kill:     d.kill,
	}, nil
}

type part struct {
	deps deps
}

// New returns the services part with the production seams.
func New() engine.Part {
	return part{deps: production()}
}

func (part) Name() string    { return partName }
func (part) Needs() []string { return []string{"toolchains"} }

// needs is what the selected repositories declare.
type needs struct {
	postgres  bool
	databases []spec.DatabaseSpec // by name; the first declaration of a name wins
	mail      bool
}

// collectNeeds reads orbit.yaml of every selected repository. A malformed file is an error.
func collectNeeds(root string, selected []manifest.RepoTarget) (needs, error) {
	var n needs
	seen := make(map[string]bool)
	for _, target := range selected {
		rs, _, err := spec.LoadRepoSpec(filepath.Join(root, target.Path))
		if err != nil {
			return needs{}, err
		}
		if rs.Services.Postgres != nil {
			n.postgres = true
			for _, db := range rs.Services.Postgres.Databases {
				if !seen[db.Name] {
					seen[db.Name] = true
					n.databases = append(n.databases, db)
				}
			}
		}
		if rs.Services.Mail {
			n.mail = true
		}
	}
	return n, nil
}

// Plan returns the actions for the declared services in the machine's mode.
func (p part) Plan(ctx context.Context, s *engine.State) (engine.Plan, error) {
	if s.Machine.Services == "container" {
		return engine.Plan{Findings: []engine.Finding{{
			Part:     partName,
			Subject:  "services",
			Message:  "container mode arrives in phase 2",
			Fix:      "set services: native or external in machine.yaml",
			Severity: engine.SeverityError,
		}}}, nil
	}
	selected, err := s.Selected()
	if err != nil {
		return engine.Plan{}, err
	}
	n, err := collectNeeds(s.Root, selected)
	if err != nil {
		return engine.Plan{}, err
	}
	if s.Machine.Services == "external" {
		return p.planExternal(n), nil
	}
	return p.planNative(ctx, s, n)
}

// planExternal only checks that the declared ports answer. It never creates or starts anything.
func (p part) planExternal(n needs) engine.Plan {
	var plan engine.Plan
	if n.postgres {
		p.requireListener(&plan, postgresPort, "postgres")
	}
	if n.mail {
		p.requireListener(&plan, mailSMTPPort, "mailpit")
	}
	return plan
}

func (p part) requireListener(plan *engine.Plan, port int, name string) {
	if err := p.deps.dial(fmt.Sprintf("127.0.0.1:%d", port), dialTimeout); err == nil {
		return
	}
	plan.Findings = append(plan.Findings, engine.Finding{
		Part:     partName,
		Subject:  name,
		Message:  fmt.Sprintf("nothing accepts TCP on 127.0.0.1:%d; external mode never starts %s", port, name),
		Fix:      fmt.Sprintf("start your %s on 127.0.0.1:%d, or set services: native in machine.yaml", name, port),
		Severity: engine.SeverityError,
	})
}

func (p part) planNative(ctx context.Context, s *engine.State, n needs) (engine.Plan, error) {
	var plan engine.Plan
	if n.postgres {
		if err := p.planPostgres(ctx, s, n.databases, &plan); err != nil {
			return engine.Plan{}, err
		}
	}
	if n.mail {
		if err := p.planMailpit(ctx, s, &plan); err != nil {
			return engine.Plan{}, err
		}
	}
	return plan, nil
}

// planPostgres plans the cluster creation and start, then the databases. A server that is not
// running cannot be checked, so one "Ensure databases" action covers every declared database. A
// running server gets one action per database that is missing or lacks its password.
func (p part) planPostgres(ctx context.Context, s *engine.State, dbs []spec.DatabaseSpec, plan *engine.Plan) error {
	bin, err := PostgresBinDir(ctx, s.Run, s.Platform, s.FS)
	if err != nil {
		return err
	}
	pg, err := p.deps.postgres(s, bin)
	if err != nil {
		return err
	}
	st, err := pg.Status(ctx)
	if err != nil {
		return err
	}
	if st.Owner == "other" {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  "postgres",
			Message:  fmt.Sprintf("port %d is used by another Postgres; set services: external in ~/.config/orbit/machine.yaml", postgresPort),
			Severity: engine.SeverityError,
		})
		return nil
	}

	if !st.Running {
		if !pg.initialized() {
			plan.Actions = append(plan.Actions, engine.Action{
				Part:    partName,
				Subject: "postgres",
				Summary: "Create Postgres cluster",
				Do:      pg.initCluster,
			})
		}
		plan.Actions = append(plan.Actions, engine.Action{
			Part:    partName,
			Subject: "postgres",
			Summary: "Start Postgres",
			Do:      pg.Start,
		})
	}

	if len(dbs) == 0 {
		return nil
	}
	if !st.Running {
		plan.Actions = append(plan.Actions, engine.Action{
			Part:    partName,
			Subject: "postgres",
			Summary: "Ensure databases",
			Do: func(ctx context.Context) error {
				for _, db := range dbs {
					if err := ensureDatabase(ctx, s, pg, db); err != nil {
						return err
					}
				}
				return nil
			},
		})
		return nil
	}
	for _, db := range dbs {
		roleOK, dbOK, passwordOK, err := pg.dbState(ctx, db)
		if err != nil {
			return err
		}
		if roleOK && dbOK && passwordOK {
			continue
		}
		plan.Actions = append(plan.Actions, engine.Action{
			Part:    partName,
			Subject: db.Name,
			Summary: "Create database " + db.Name,
			Do: func(ctx context.Context) error {
				return ensureDatabase(ctx, s, pg, db)
			},
		})
	}
	return nil
}

// ensureDatabase runs EnsureDatabase and records a created database in s.NewDatabases.
func ensureDatabase(ctx context.Context, s *engine.State, pg *postgres, db spec.DatabaseSpec) error {
	created, err := pg.EnsureDatabase(ctx, db)
	if err != nil {
		return err
	}
	if created {
		if s.NewDatabases == nil {
			s.NewDatabases = make(map[string]bool)
		}
		s.NewDatabases[db.Name] = true
	}
	return nil
}

// planMailpit plans Mailpit on macOS only. On Linux it warns and plans nothing.
func (p part) planMailpit(ctx context.Context, s *engine.State, plan *engine.Plan) error {
	if s.Platform != "darwin" {
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  "mailpit",
			Message:  "mailpit is not packaged for Linux; install it from https://mailpit.axllent.org or set services: external",
			Severity: engine.SeverityWarn,
		})
		return nil
	}
	mp, err := p.deps.mailpit(s)
	if err != nil {
		return err
	}
	st, err := mp.Status(ctx)
	if err != nil {
		return err
	}
	switch {
	case st.Owner == "other":
		plan.Findings = append(plan.Findings, engine.Finding{
			Part:     partName,
			Subject:  "mailpit",
			Message:  fmt.Sprintf("port %d is used by another process; set services: external in ~/.config/orbit/machine.yaml", mailSMTPPort),
			Severity: engine.SeverityError,
		})
	case !st.Running:
		plan.Actions = append(plan.Actions, engine.Action{
			Part:    partName,
			Subject: "mailpit",
			Summary: "Start Mailpit",
			Do:      mp.Start,
		})
	}
	return nil
}

// Row is one service line for `o services`. Err is set when the service cannot be queried.
type Row struct {
	Name   string
	Status Status
	Err    error
}

// Services reports the status of postgres and mailpit as this machine is configured.
func Services(ctx context.Context, s *engine.State) ([]Row, error) {
	return servicesWith(ctx, s, production())
}

func servicesWith(ctx context.Context, s *engine.State, d deps) ([]Row, error) {
	var bin string
	var binErr error
	if s.Machine.Services == "native" {
		bin, binErr = PostgresBinDir(ctx, s.Run, s.Platform, s.FS)
	}
	pg, err := d.postgres(s, bin)
	if err != nil {
		return nil, err
	}
	mp, err := d.mailpit(s)
	if err != nil {
		return nil, err
	}

	var rows []Row
	if binErr != nil {
		rows = append(rows, Row{Name: pg.Name(), Err: binErr})
	} else {
		st, err := pg.Status(ctx)
		rows = append(rows, Row{Name: pg.Name(), Status: st, Err: err})
	}
	st, err := mp.Status(ctx)
	rows = append(rows, Row{Name: mp.Name(), Status: st, Err: err})
	return rows, nil
}

// Down stops the services this orbit owns: Mailpit, then Postgres. External and container
// machines have nothing to stop. It returns the names it stopped.
func Down(ctx context.Context, s *engine.State) ([]string, error) {
	return downWith(ctx, s, production())
}

func downWith(ctx context.Context, s *engine.State, d deps) ([]string, error) {
	if s.Machine.Services != "native" {
		return nil, nil
	}
	// A missing binary directory leaves bin empty. Then Status cannot report orbit ownership,
	// so Stop is never reached for postgres and nothing is stopped.
	bin, _ := PostgresBinDir(ctx, s.Run, s.Platform, s.FS)
	pg, err := d.postgres(s, bin)
	if err != nil {
		return nil, err
	}
	mp, err := d.mailpit(s)
	if err != nil {
		return nil, err
	}

	var stopped []string
	for _, svc := range []Service{mp, pg} {
		st, err := svc.Status(ctx)
		if err != nil {
			return stopped, err
		}
		if st.Owner != "orbit" {
			continue
		}
		if err := svc.Stop(ctx); err != nil {
			return stopped, err
		}
		stopped = append(stopped, svc.Name())
	}
	return stopped, nil
}
