package localdev

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
)

type fakeFiles struct {
	files map[string]string
}

func (f *fakeFiles) ReadFile(path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return []byte(data), nil
}

func (f *fakeFiles) Exists(path string) bool {
	_, ok := f.files[path]
	return ok
}

func (f *fakeFiles) ReadDir(string) ([]string, error) { return nil, nil }

const devURL = "postgres://orbit:secret@localhost:5432/dev?sslmode=disable"
const testURL = "postgres://orbit:secret@localhost:5432/test_abc123?sslmode=disable"

func testLocal() (*Local, *runnertest.Fake) {
	files := &fakeFiles{files: map[string]string{
		"/ws/orbit/orbit-cli/.env": "DATABASE_URL=" + devURL + "\nREDIS_URL=redis://localhost:6379\nNATS_URL=nats://localhost:4222\n",
	}}
	fake := &runnertest.Fake{}
	return New("/ws", fake, files), fake
}

func TestReachableCreateAndDropArgs(t *testing.T) {
	local, fake := testLocal()
	fake.On("psql "+devURL+" -tAc SELECT 1", runner.Result{Stdout: "1\n"}, nil)
	fake.On("psql "+devURL+" -v ON_ERROR_STOP=1 -c CREATE DATABASE test_abc123", runner.Result{}, nil)
	fake.On("psql "+devURL+" -v ON_ERROR_STOP=1 -c DROP DATABASE IF EXISTS test_abc123", runner.Result{}, nil)

	if err := local.Reachable(context.Background(), "/ws/orbit/orbit-cli"); err != nil {
		t.Fatalf("Reachable: %v", err)
	}
	if err := local.CreateDatabase(context.Background(), "/ws/orbit/orbit-cli", "test_abc123"); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	if err := local.DropDatabase(context.Background(), "/ws/orbit/orbit-cli", "test_abc123"); err != nil {
		t.Fatalf("DropDatabase: %v", err)
	}

	calls := fake.Calls()
	want := [][]string{
		{"-tAc", "SELECT 1"},
		{"-v", "ON_ERROR_STOP=1", "-c", "CREATE DATABASE test_abc123"},
		{"-v", "ON_ERROR_STOP=1", "-c", "DROP DATABASE IF EXISTS test_abc123 WITH (FORCE)"},
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d", len(calls), len(want))
	}
	for i := range want {
		args := append([]string{devURL}, want[i]...)
		if calls[i].Name != "psql" {
			t.Errorf("call %d command = %q", i, calls[i].Name)
		}
		if strings.Join(calls[i].Args, " ") != strings.Join(args, " ") {
			t.Errorf("call %d args = %v, want %v", i, calls[i].Args, args)
		}
	}
}

func TestEnvRewritesDatabaseAndPassesBusURLs(t *testing.T) {
	local, _ := testLocal()
	env, err := local.Env("/ws/orbit/orbit-cli", "test_abc123")
	if err != nil {
		t.Fatalf("Env: %v", err)
	}
	if env["TEST_DATABASE_URL"] != testURL {
		t.Errorf("TEST_DATABASE_URL = %q", env["TEST_DATABASE_URL"])
	}
	if env["CI_TIER"] != "release" {
		t.Errorf("CI_TIER = %q", env["CI_TIER"])
	}
	if env["TEST_REDIS_URL"] != "redis://localhost:6379" || env["BUS_REDIS_URL"] != "redis://localhost:6379" {
		t.Errorf("redis env = %v", env)
	}
	if env["TEST_NATS_URL"] != "nats://localhost:4222" || env["BUS_NATS_URL"] != "nats://localhost:4222" {
		t.Errorf("nats env = %v", env)
	}
	if strings.Contains(env["TEST_DATABASE_URL"], "/dev?") {
		t.Errorf("dev database name was not replaced: %q", env["TEST_DATABASE_URL"])
	}
}

func TestReachableMissingURLIsUnverified(t *testing.T) {
	local := New("/ws", &runnertest.Fake{}, &fakeFiles{files: map[string]string{}})
	err := local.Reachable(context.Background(), "/ws/orbit/orbit-cli")
	if err == nil || clierr.Code(err) != clierr.CodeUnverified {
		t.Fatalf("err = %v", err)
	}
}

func TestReachableUnreachableIsUnverifiedAndRedacted(t *testing.T) {
	local, fake := testLocal()
	fake.On("psql "+devURL+" -tAc SELECT 1", runner.Result{Stderr: "could not connect to " + devURL + "\n"}, errors.New("exit status 2"))

	err := local.Reachable(context.Background(), "/ws/orbit/orbit-cli")
	if err == nil || clierr.Code(err) != clierr.CodeUnverified {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), devURL) {
		t.Errorf("error leaked the connection URL: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Errorf("error did not mark the redacted URL: %v", err)
	}
}

func TestInvalidDatabaseNameIsRefused(t *testing.T) {
	local, fake := testLocal()
	if err := local.CreateDatabase(context.Background(), "/ws/orbit/orbit-cli", "prod; DROP DATABASE prod"); err == nil {
		t.Fatal("expected the invalid name to be refused")
	}
	if len(fake.Calls()) != 0 {
		t.Errorf("psql ran for an invalid name: %v", fake.Calls())
	}
}

func TestParseEnv(t *testing.T) {
	values := parseEnv("export DATABASE_URL='postgres://u:p@h/db'\n# comment\nNATS_URL=nats://h:4222\n")
	if values["DATABASE_URL"] != "postgres://u:p@h/db" {
		t.Errorf("DATABASE_URL = %q", values["DATABASE_URL"])
	}
	if values["NATS_URL"] != "nats://h:4222" {
		t.Errorf("NATS_URL = %q", values["NATS_URL"])
	}
}
