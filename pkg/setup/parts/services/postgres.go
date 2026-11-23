package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

// postgresPort is the orbit-owned cluster's TCP port, bound to loopback only.
const postgresPort = 10000

// postgresMajor is the Postgres major version the binaries and the cluster must be.
const postgresMajor = "18"

// postgres is the orbit-owned native cluster. bin is the directory holding initdb, pg_ctl and
// psql; it is empty when the binaries could not be found, which leaves Status to the TCP probe.
type postgres struct {
	run         runner.Runner
	fs          engine.FS
	bin         string
	dataDir     string
	secretsPath string
	user        string
	dial        dialer
}

// PostgresBinDir returns the directory of the Postgres 18 binaries: the brew prefix of
// postgresql@18 plus /bin on darwin, /usr/lib/postgresql/18/bin on linux. A missing directory is a
// tool error.
func PostgresBinDir(ctx context.Context, run runner.Runner, platform string, fsys engine.FS) (string, error) {
	var dir string
	switch platform {
	case "darwin":
		res, err := run.Run(ctx, "", "brew", "--prefix", "postgresql@"+postgresMajor)
		if err != nil {
			return "", clierr.Wrap(err, clierr.Tool("cannot find postgresql@"+postgresMajor+" with brew"))
		}
		dir = filepath.Join(strings.TrimSpace(res.Stdout), "bin")
	case "linux":
		dir = "/usr/lib/postgresql/" + postgresMajor + "/bin"
	default:
		return "", clierr.Unverified(fmt.Sprintf("no Postgres layout for platform %q", platform))
	}
	if _, err := fsys.Stat(dir); err != nil {
		return "", clierr.Tool(fmt.Sprintf("postgres %s binaries not found in %s", postgresMajor, dir))
	}
	return dir, nil
}

func (p *postgres) Name() string { return "postgres" }

func (p *postgres) binary(name string) string { return filepath.Join(p.bin, name) }

func (p *postgres) addr() string { return fmt.Sprintf("127.0.0.1:%d", postgresPort) }

// Status asks pg_ctl whether the data directory's server runs (owner orbit). Otherwise a TCP
// probe of the port answers for a foreign listener (owner other). Neither means not running.
func (p *postgres) Status(ctx context.Context) (Status, error) {
	if p.bin != "" {
		_, err := p.run.Run(ctx, "", p.binary("pg_ctl"), "-D", p.dataDir, "status")
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Status{Port: postgresPort}, ctxErr
		}
		if err == nil {
			return Status{Running: true, Port: postgresPort, Owner: "orbit"}, nil
		}
	}
	if p.dial(p.addr(), dialTimeout) == nil {
		return Status{Running: true, Port: postgresPort, Owner: "other"}, nil
	}
	return Status{Port: postgresPort}, nil
}

// Start runs the cluster in the background and waits until it accepts connections.
func (p *postgres) Start(ctx context.Context) error {
	logPath := filepath.Join(p.dataDir, "server.log")
	if _, err := p.run.Run(ctx, "", p.binary("pg_ctl"), "-D", p.dataDir, "-l", logPath, "-w", "start"); err != nil {
		return clierr.Wrap(err, clierr.Tool("cannot start Postgres; see "+logPath))
	}
	return nil
}

// Stop shuts the cluster down. It touches only a server orbit owns; anything else is left alone.
func (p *postgres) Stop(ctx context.Context) error {
	st, err := p.Status(ctx)
	if err != nil {
		return err
	}
	if st.Owner != "orbit" {
		return nil
	}
	if _, err := p.run.Run(ctx, "", p.binary("pg_ctl"), "-D", p.dataDir, "-m", "fast", "stop"); err != nil {
		return clierr.Wrap(err, clierr.Tool("cannot stop Postgres"))
	}
	return nil
}

// initialized reports whether the data directory holds a cluster.
func (p *postgres) initialized() bool {
	_, err := p.fs.Stat(filepath.Join(p.dataDir, "PG_VERSION"))
	return err == nil
}

// initCluster creates the cluster with local peer auth and scram over TCP, then appends the
// loopback listener, port and socket directory to its configuration.
func (p *postgres) initCluster(ctx context.Context) error {
	if err := p.fs.MkdirAll(p.dataDir, 0o700); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot create "+p.dataDir))
	}
	args := []string{
		"-D", p.dataDir, "-U", p.user,
		"--auth-local=peer", "--auth-host=scram-sha-256",
		"-E", "UTF8", "--locale=C",
	}
	if _, err := p.run.Run(ctx, "", p.binary("initdb"), args...); err != nil {
		return clierr.Wrap(err, clierr.Tool("initdb failed"))
	}
	conf := filepath.Join(p.dataDir, "postgresql.conf")
	data, err := p.fs.ReadFile(conf)
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot read "+conf))
	}
	extra := fmt.Sprintf("\nport = %d\nlisten_addresses = '127.0.0.1'\nunix_socket_directories = '%s'\n",
		postgresPort, p.dataDir)
	if err := p.fs.WriteFile(conf, append(data, extra...), 0o600); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot write "+conf))
	}
	return nil
}

// psqlArgs connects over the cluster's unix socket as the OS user, which is a superuser.
func (p *postgres) psqlArgs() []string {
	return []string{
		"-h", p.dataDir, "-p", strconv.Itoa(postgresPort), "-d", "postgres",
		"-v", "ON_ERROR_STOP=1", "-X", "-q",
	}
}

// exists reports whether a row of catalog has column equal to name. Names are validated by
// spec.LoadRepoSpec, so they carry no quote characters.
func (p *postgres) exists(ctx context.Context, catalog, column, name string) (bool, error) {
	query := fmt.Sprintf("SELECT 1 FROM %s WHERE %s='%s'", catalog, column, name)
	res, err := p.run.Run(ctx, "", p.binary("psql"), append(p.psqlArgs(), "-tAc", query)...)
	if err != nil {
		return false, clierr.Wrap(err, clierr.Tool("cannot query Postgres"))
	}
	return strings.TrimSpace(res.Stdout) == "1", nil
}

// dbState reports whether db's role exists, whether its database exists and whether the secrets
// file holds the role's password.
func (p *postgres) dbState(ctx context.Context, db spec.DatabaseSpec) (roleOK, dbOK, passwordOK bool, err error) {
	if roleOK, err = p.exists(ctx, "pg_roles", "rolname", db.Role); err != nil {
		return false, false, false, err
	}
	if dbOK, err = p.exists(ctx, "pg_database", "datname", db.Name); err != nil {
		return false, false, false, err
	}
	secrets, err := LocalSecrets(p.secretsPath)
	if err != nil {
		return false, false, false, err
	}
	return roleOK, dbOK, secrets[passwordKey(db.Role)] != "", nil
}

// EnsureDatabase makes db's role and database exist and keeps a password for the role in the
// secrets file. It reports whether this call created the database. The SQL, which carries the new
// password, reaches psql only as a 0600 file in the data directory, removed before return; no
// password is ever on a command line.
func (p *postgres) EnsureDatabase(ctx context.Context, db spec.DatabaseSpec) (bool, error) {
	roleOK, dbOK, passwordOK, err := p.dbState(ctx, db)
	if err != nil {
		return false, err
	}
	if roleOK && dbOK && passwordOK {
		return false, nil
	}

	var sql strings.Builder
	var password string
	if !roleOK || !passwordOK {
		if password, err = generatePassword(); err != nil {
			return false, err
		}
		if roleOK {
			fmt.Fprintf(&sql, "ALTER ROLE \"%s\" WITH LOGIN PASSWORD '%s';\n", db.Role, password)
		} else {
			fmt.Fprintf(&sql, "CREATE ROLE \"%s\" LOGIN PASSWORD '%s';\n", db.Role, password)
		}
	}
	if !dbOK {
		fmt.Fprintf(&sql, "CREATE DATABASE \"%s\" OWNER \"%s\";\n", db.Name, db.Role)
	}
	if err := p.runSQL(ctx, sql.String()); err != nil {
		return false, err
	}

	if password != "" {
		secrets, err := LocalSecrets(p.secretsPath)
		if err != nil {
			return false, err
		}
		secrets[passwordKey(db.Role)] = password
		if err := SaveLocalSecrets(p.secretsPath, secrets); err != nil {
			return false, err
		}
	}
	return !dbOK, nil
}

// runSQL writes sql to a 0600 file in the data directory, runs it with psql -f and removes the
// file. The error never includes psql's output, which can echo a statement and its password.
func (p *postgres) runSQL(ctx context.Context, sql string) error {
	f, err := os.CreateTemp(p.dataDir, "orbit-setup-*.sql")
	if err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot create an SQL file in "+p.dataDir))
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(sql); err != nil {
		f.Close()
		return clierr.Failed("cannot write the SQL file")
	}
	if err := f.Close(); err != nil {
		return clierr.Failed("cannot close the SQL file")
	}
	if _, err := p.run.Run(ctx, "", p.binary("psql"), append(p.psqlArgs(), "-f", f.Name())...); err != nil {
		return clierr.Tool("cannot run SQL on Postgres; check the server log in " + p.dataDir)
	}
	return nil
}
