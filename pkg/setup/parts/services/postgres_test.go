package services

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

func TestEnsureDatabaseCreatesRoleAndDbOnce(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.mkCluster()
	sim := newSim(t, r.dataDir)
	sim.running = true
	pg := r.postgres(sim)
	db := spec.DatabaseSpec{Name: "app_api", Role: "app_api"}

	created, err := pg.EnsureDatabase(ctx, db)
	if err != nil || !created {
		t.Fatalf("first EnsureDatabase = %v, %v; want created", created, err)
	}
	created, err = pg.EnsureDatabase(ctx, db)
	if err != nil || created {
		t.Fatalf("second EnsureDatabase = %v, %v; want not created", created, err)
	}
	if n := sim.count("CREATE ROLE"); n != 1 {
		t.Errorf("CREATE ROLE ran %d times, want 1", n)
	}
	if n := sim.count("CREATE DATABASE"); n != 1 {
		t.Errorf("CREATE DATABASE ran %d times, want 1", n)
	}
	if pw := readSecrets(t, r.secrets)[passwordKey("app_api")]; len(pw) != 32 {
		t.Fatalf("saved password length %d, want 32", len(pw))
	}
}

func TestPasswordNeverInArgs(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.mkCluster()
	sim := newSim(t, r.dataDir)
	sim.running = true
	pg := r.postgres(sim)

	if _, err := pg.EnsureDatabase(ctx, spec.DatabaseSpec{Name: "app_api", Role: "app_api"}); err != nil {
		t.Fatal(err)
	}
	pw := readSecrets(t, r.secrets)[passwordKey("app_api")]
	if pw == "" {
		t.Fatal("no password saved")
	}
	sawFile := false
	for _, c := range sim.calls {
		for _, a := range c.args {
			if strings.Contains(a, pw) {
				t.Fatalf("generated password appears in argv of %s: %q", c.name, c.args)
			}
			if a == "-f" {
				sawFile = true
			}
		}
	}
	if !sawFile {
		t.Fatal("SQL was not passed through a file (-f)")
	}
}

func TestPostgresBinDir(t *testing.T) {
	ctx := context.Background()

	t.Run("darwin asks brew for the prefix", func(t *testing.T) {
		fake := &runnertest.Fake{}
		fake.On("brew --prefix postgresql@18", runner.Result{Stdout: "/opt/homebrew/opt/postgresql@18\n"}, nil)
		got, err := PostgresBinDir(ctx, fake, "darwin", fakeFS{present: []string{darwinBin}})
		if err != nil || got != darwinBin {
			t.Fatalf("PostgresBinDir = %q, %v; want %q", got, err, darwinBin)
		}
	})

	t.Run("linux missing binaries is a tool error", func(t *testing.T) {
		_, err := PostgresBinDir(ctx, &runnertest.Fake{}, "linux", fakeFS{})
		if err == nil || clierr.ExitCode(err) != clierr.ExitTool {
			t.Fatalf("err = %v, want tool error", err)
		}
		if !strings.Contains(err.Error(), "postgres 18 binaries not found in "+linuxBin) {
			t.Fatalf("err = %q", err)
		}
	})
}

func TestRunningServerNeedsOnlyMissingDatabases(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.addRepo("app/api", apiOrbit)
	r.mkCluster()
	sim := newSim(t, r.dataDir)
	sim.running = true
	sim.seed("app_api", "app_api")
	st := r.state("linux", "native", sim, "app/api")

	// The role and database exist but no password is saved: one fix-up action.
	plan, err := r.part().Plan(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Create database app_api"}; !slices.Equal(summaries(plan), want) {
		t.Fatalf("actions = %q, want %q", summaries(plan), want)
	}
}

func TestPsqlErrorIsReported(t *testing.T) {
	r := newRig(t)
	r.mkCluster()
	fake := &runnertest.Fake{}
	fake.On(linuxBin+"/psql", runner.Result{ExitCode: 2}, errors.New("psql: connection refused"))
	pg := r.postgres(fake)
	if _, err := pg.EnsureDatabase(context.Background(), spec.DatabaseSpec{Name: "app_api", Role: "app_api"}); err == nil {
		t.Fatal("EnsureDatabase succeeded though psql failed")
	}
	if left, _ := filepath.Glob(filepath.Join(r.dataDir, "orbit-setup-*.sql")); len(left) != 0 {
		t.Fatalf("SQL temp file left behind: %q", left)
	}
}
