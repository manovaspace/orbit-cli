package services

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/runner/runnertest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

func TestMailpitStartWritesPIDFile(t *testing.T) {
	r := newRig(t)
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

func TestMailpitStopKillsOwnProcess(t *testing.T) {
	r := newRig(t)
	if err := os.MkdirAll(r.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(r.stateDir, "mailpit.pid")
	if err := os.WriteFile(pidPath, []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.alive[4242] = true

	if err := r.mailpit(&runnertest.Fake{}).Stop(context.Background()); err != nil {
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
