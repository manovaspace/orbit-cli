package services

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

func TestMailpitStartWritesPIDFile(t *testing.T) {
	r := newRig(t)
	r.listening["127.0.0.1:10024"] = true
	fake := &runnertest.Fake{}
	fake.On("sh -c", runner.Result{Stdout: "4242\n"}, nil)
	mp := r.mailpit(fake)

	if err := mp.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %+v, want one sh -c", calls)
	}
	cmd := calls[0].Args[len(calls[0].Args)-1]
	for _, want := range []string{"nohup mailpit", "--smtp 127.0.0.1:10024", "--listen 127.0.0.1:10025"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command %q lacks %q", cmd, want)
		}
	}

	pidPath := filepath.Join(r.stateDir, "mailpit.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	if pid, _ := strconv.Atoi(strings.TrimSpace(string(data))); pid != 4242 {
		t.Fatalf("pid file = %q, want 4242", data)
	}
	assertMode(t, pidPath, 0o600)
	assertMode(t, r.stateDir, 0o700)
}

// writePIDFile records pid in the PID file under the rig's state directory.
func writePIDFile(t *testing.T, r *rig, pid int) string {
	t.Helper()
	if err := os.MkdirAll(r.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(r.stateDir, "mailpit.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return pidPath
}

func TestMailpitStopKillsOwnProcess(t *testing.T) {
	r := newRig(t)
	pidPath := writePIDFile(t, r, 4242)
	r.alive[4242] = true
	fake := &runnertest.Fake{}
	fake.On("ps -p 4242", runner.Result{Stdout: "mailpit\n"}, nil)

	if err := r.mailpit(fake).Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.killed) != 1 || r.killed[0] != (killRecord{pid: 4242, sig: syscall.SIGTERM}) {
		t.Fatalf("kills = %+v, want SIGTERM to 4242", r.killed)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file still present: %v", err)
	}
}

func TestMailpitForeignListenerRefused(t *testing.T) {
	r := newRig(t)
	r.addRepo("app/mail", "version: 1\nservices:\n  mail: true\n")
	r.listening["127.0.0.1:10024"] = true
	st := r.state("darwin", "native", &runnertest.Fake{}, "app/mail")

	pr := (&engine.Engine{Parts: []engine.Part{r.part()}}).Check(context.Background(), st).Parts[0]
	if pr.Status != engine.StatusFailed || !findingContains(pr.Findings, "port 10024 is used by another process") {
		t.Fatalf("status %q findings %+v", pr.Status, pr.Findings)
	}
}

func TestMailpitStopNeverSignalsForeignPID(t *testing.T) {
	r := newRig(t)
	pidPath := writePIDFile(t, r, 4242)
	r.alive[4242] = true
	fake := &runnertest.Fake{}
	fake.On("ps -p 4242", runner.Result{Stdout: "sshd\n"}, nil)

	if err := r.mailpit(fake).Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.killed) != 0 {
		t.Fatalf("kills = %+v, want none for a foreign pid", r.killed)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pid file still present: %v", err)
	}
}

func TestMailpitStalePIDFileStartsMailpit(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	pidPath := writePIDFile(t, r, 4242)
	r.alive[4242] = true
	fake := &runnertest.Fake{}
	fake.On("ps -p 4242", runner.Result{Stdout: "sshd\n"}, nil)
	r.addRepo("app/mail", "version: 1\nservices:\n  mail: true\n")
	st := r.state("darwin", "native", fake, "app/mail")

	plan, err := r.part().Plan(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Start Mailpit"}; !slices.Equal(summaries(plan), want) {
		t.Fatalf("actions = %q, want %q", summaries(plan), want)
	}
	if len(r.killed) != 0 {
		t.Fatalf("kills = %+v, want none for a foreign pid", r.killed)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pid file still present: %v", err)
	}
}

func TestMailpitOwnedWithListenerPlansNothing(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	writePIDFile(t, r, 4242)
	r.alive[4242] = true
	r.listening["127.0.0.1:10024"] = true
	fake := &runnertest.Fake{}
	fake.On("ps -p 4242", runner.Result{Stdout: "mailpit\n"}, nil)
	r.addRepo("app/mail", "version: 1\nservices:\n  mail: true\n")
	st := r.state("darwin", "native", fake, "app/mail")

	plan, err := r.part().Plan(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 || len(plan.Findings) != 0 {
		t.Fatalf("plan = %+v, want nothing for a running orbit mailpit", plan)
	}
	st2, err := r.mailpit(fake).Status(ctx)
	if err != nil || st2.Owner != "orbit" || !st2.Running {
		t.Fatalf("status = %+v, %v; want running/orbit", st2, err)
	}
}

func TestMailpitStartFailsWithoutListener(t *testing.T) {
	r := newRig(t)
	old := mailStartWait
	mailStartWait = 200 * time.Millisecond
	t.Cleanup(func() { mailStartWait = old })
	fake := &runnertest.Fake{}
	fake.On("sh -c", runner.Result{Stdout: "4242\n"}, nil)

	err := r.mailpit(fake).Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), filepath.Join(r.stateDir, "mailpit.log")) {
		t.Fatalf("Start error = %v, want a tool error naming the log", err)
	}
	if _, statErr := os.Stat(filepath.Join(r.stateDir, "mailpit.pid")); !os.IsNotExist(statErr) {
		t.Fatalf("pid file written without a listener: %v", statErr)
	}
}

func TestProcessAliveIgnoresEPERM(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("own process reported not alive")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can signal pid 1")
	}
	if err := syscall.Kill(1, 0); err == nil {
		t.Skip("this user can signal pid 1")
	}
	if processAlive(1) {
		t.Fatal("EPERM from signal 0 counted as alive")
	}
}
