package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

func TestPostgresInitWhenNoPGVersion(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.addRepo("app/api", apiOrbit)
	st := r.state("linux", "native", newSim(t, r.dataDir), "app/api")
	p := r.part()

	plan, err := p.Plan(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Create Postgres cluster", "Start Postgres", "Ensure databases"}
	if got := summaries(plan); !slices.Equal(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}

	r.mkCluster()
	plan, err = p.Plan(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if got := summaries(plan); slices.Contains(got, "Create Postgres cluster") {
		t.Fatalf("cluster with PG_VERSION planned again: %q", got)
	}
}

func TestPostgresConfigWritten(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.addRepo("app/api", apiOrbit)
	sim := newSim(t, r.dataDir)
	st := r.state("linux", "native", sim, "app/api")

	plan, err := r.part().Plan(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range plan.Actions {
		if err := a.Do(ctx); err != nil {
			t.Fatalf("action %q: %v", a.Summary, err)
		}
	}

	conf, err := os.ReadFile(filepath.Join(r.dataDir, "postgresql.conf"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(conf), "\n")
	for _, want := range []string{
		"port = 10000",
		"listen_addresses = '127.0.0.1'",
		"unix_socket_directories = '" + r.dataDir + "'",
	} {
		if !slices.Contains(lines, want) {
			t.Errorf("postgresql.conf lacks line %q:\n%s", want, conf)
		}
	}

	var initArgs []string
	for _, c := range sim.calls {
		if filepath.Base(c.name) == "initdb" {
			initArgs = c.args
		}
	}
	if initArgs == nil {
		t.Fatalf("initdb never ran; calls = %+v", sim.calls)
	}
	for _, flag := range []string{"--auth-local=peer", "--auth-host=scram-sha-256", "-U", "tester"} {
		if !slices.Contains(initArgs, flag) {
			t.Errorf("initdb args %q lack %q", initArgs, flag)
		}
	}
	if !st.NewDatabases["app_api"] {
		t.Errorf("NewDatabases = %v, want app_api recorded as created", st.NewDatabases)
	}
}

func TestServicesSecondRunNoActions(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.addRepo("app/api", apiOrbit)
	r.mkCluster()
	sim := newSim(t, r.dataDir)
	sim.running = true
	sim.seed("app_api", "app_api")
	if err := SaveLocalSecrets(r.secrets, map[string]string{passwordKey("app_api"): "s3cret"}); err != nil {
		t.Fatal(err)
	}
	st := r.state("linux", "native", sim, "app/api")

	e := engine.Engine{Parts: []engine.Part{r.part()}}
	res := e.Check(ctx, st)
	pr := res.Parts[0]
	if pr.Status != engine.StatusOK || len(pr.Actions) != 0 || res.Changed != 0 {
		t.Fatalf("second run: status %q actions %q changed %d, want ok with no actions", pr.Status, pr.Actions, res.Changed)
	}
}

func TestNativeRefusesForeignListener(t *testing.T) {
	r := newRig(t)
	r.addRepo("app/api", apiOrbit)
	r.listening["127.0.0.1:10000"] = true
	fake := &runnertest.Fake{}
	fake.On(linuxBin+"/pg_ctl", runner.Result{ExitCode: 3}, errors.New("no server running"))
	st := r.state("linux", "native", fake, "app/api")

	e := engine.Engine{Parts: []engine.Part{r.part()}}
	pr := e.Check(context.Background(), st).Parts[0]
	if pr.Status != engine.StatusFailed {
		t.Fatalf("status = %q, want failed", pr.Status)
	}
	if !findingContains(pr.Findings, "port 10000 is used by another Postgres") {
		t.Fatalf("findings = %+v, want the foreign listener refusal", pr.Findings)
	}
}

func TestExternalModeCreatesNothing(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.addRepo("app/api", apiMailOrbit)
	fake := &runnertest.Fake{}
	r.listening["127.0.0.1:10000"] = true
	r.listening["127.0.0.1:10024"] = true
	st := r.state("darwin", "external", fake, "app/api")

	plan, err := r.part().Plan(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 || len(plan.Findings) != 0 {
		t.Fatalf("external plan = %+v, want nothing", plan)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("external mode ran %+v", calls)
	}
	if _, err := os.Stat(r.dataDir); !os.IsNotExist(err) {
		t.Fatalf("external mode touched the data dir: %v", err)
	}
	if want := []string{"127.0.0.1:10000", "127.0.0.1:10024"}; !slices.Equal(r.dialed, want) {
		t.Fatalf("dialed %q, want %q", r.dialed, want)
	}

	r.listening = map[string]bool{}
	pr := (&engine.Engine{Parts: []engine.Part{r.part()}}).Check(ctx, st).Parts[0]
	if pr.Status != engine.StatusFailed {
		t.Fatalf("external with no listener: status %q, want failed", pr.Status)
	}
}

func TestContainerModeNotYet(t *testing.T) {
	r := newRig(t)
	st := r.state("linux", "container", &runnertest.Fake{})
	pr := (&engine.Engine{Parts: []engine.Part{r.part()}}).Check(context.Background(), st).Parts[0]
	if pr.Status != engine.StatusFailed || !findingContains(pr.Findings, "container mode arrives in phase 2") {
		t.Fatalf("container mode: status %q findings %+v", pr.Status, pr.Findings)
	}
}

func TestMailpitLinuxWarnsOnly(t *testing.T) {
	r := newRig(t)
	r.addRepo("app/mail", "version: 1\nservices:\n  mail: true\n")
	fake := &runnertest.Fake{}
	st := r.state("linux", "native", fake, "app/mail")

	res := (&engine.Engine{Parts: []engine.Part{r.part()}}).Check(context.Background(), st)
	pr := res.Parts[0]
	if pr.Status != engine.StatusOK {
		t.Fatalf("status = %q, want ok (warnings never fail)", pr.Status)
	}
	if !findingContains(pr.Findings, "mailpit is not packaged for Linux") {
		t.Fatalf("findings = %+v, want the Linux mailpit warning", pr.Findings)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("linux mail planned commands: %+v", calls)
	}
}

func TestDownStopsOwnedServices(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.mkCluster()
	sim := newSim(t, r.dataDir)
	sim.running = true
	st := r.state("linux", "native", sim)

	stopped, err := downWith(ctx, st, r.deps())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"postgres"}; !slices.Equal(stopped, want) {
		t.Fatalf("stopped = %q, want %q", stopped, want)
	}
	if sim.running {
		t.Fatal("postgres still running after down")
	}

	sim.running = true
	ext := r.state("darwin", "external", sim)
	stopped, err = downWith(ctx, ext, r.deps())
	if err != nil || len(stopped) != 0 || !sim.running {
		t.Fatalf("external down: stopped %q err %v running %v; want nothing", stopped, err, sim.running)
	}
}

func findingContains(findings []engine.Finding, text string) bool {
	for _, f := range findings {
		if strings.Contains(f.Message, text) {
			return true
		}
	}
	return false
}
