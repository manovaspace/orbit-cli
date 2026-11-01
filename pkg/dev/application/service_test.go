package application

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/dev/domain"
)

type commandCall struct {
	stream bool
	dir    string
	name   string
	args   []string
}

type fakeCommands struct {
	calls []commandCall
	err   error
}

func (f *fakeCommands) Run(_ context.Context, dir, name string, args ...string) error {
	f.calls = append(f.calls, commandCall{dir: dir, name: name, args: args})
	return f.err
}

func (f *fakeCommands) Stream(_ context.Context, dir, name string, args ...string) error {
	f.calls = append(f.calls, commandCall{stream: true, dir: dir, name: name, args: args})
	return f.err
}

type fakeFiles struct {
	infraDir string
	exists   map[string]bool
	dirs     map[string]bool
}

func (f *fakeFiles) InfraDir(string) string { return f.infraDir }

func (f *fakeFiles) Exists(path string) bool { return f.exists[path] }

func (f *fakeFiles) IsDir(path string) bool { return f.dirs[path] }

type fakeProbe struct {
	ready map[string]bool
}

func (p *fakeProbe) Wait(_ context.Context, host string, _ int, _ time.Duration) bool {
	return p.ready[host]
}

func newTestService(out *bytes.Buffer, commands *fakeCommands, files *fakeFiles, probe *fakeProbe) *Service {
	return &Service{Commands: commands, Files: files, Probe: probe, Out: out, ErrOut: out}
}

func TestUpPrefersStartScript(t *testing.T) {
	dir := "/workspace/orbit/orbit-infra"
	commands := &fakeCommands{}
	files := &fakeFiles{
		infraDir: dir,
		exists:   map[string]bool{dir + "/scripts/start-dev-stack.sh": true},
		dirs:     map[string]bool{dir: true},
	}
	out := new(bytes.Buffer)

	res, err := newTestService(out, commands, files, &fakeProbe{}).Up(context.Background(), UpInput{
		WorkspaceRoot: "/workspace",
		Modules:       []string{"postgres"},
	})
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(commands.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(commands.calls))
	}
	call := commands.calls[0]
	if !call.stream {
		t.Error("expected the start script to stream")
	}
	if call.name != dir+"/scripts/start-dev-stack.sh" || len(call.args) != 1 || call.args[0] != "postgres" {
		t.Errorf("unexpected call: %+v", call)
	}
	if res.Command.Name != call.name {
		t.Errorf("result command = %q, want %q", res.Command.Name, call.name)
	}
	if !strings.Contains(out.String(), "Starting Orbit Dev Stack") {
		t.Errorf("missing title: %s", out.String())
	}
	if !strings.Contains(out.String(), "Orbit Local Developer Endpoints") {
		t.Errorf("missing banner: %s", out.String())
	}
	if len(res.Services) != len(domain.CoreServices()) {
		t.Errorf("got %d services, want %d", len(res.Services), len(domain.CoreServices()))
	}
}

func TestUpFallsBackToComposeThenDocker(t *testing.T) {
	for _, tc := range []struct {
		name       string
		compose    bool
		wantName   string
		wantArgs   []string
		wantStream bool
	}{
		{name: "compose script", compose: true, wantName: "/infra/compose.sh", wantArgs: []string{"up", "-d", "nats"}, wantStream: true},
		{name: "docker compose", compose: false, wantName: "docker", wantArgs: []string{"compose", "up", "-d", "nats"}, wantStream: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := "/infra"
			files := &fakeFiles{infraDir: dir, exists: map[string]bool{}, dirs: map[string]bool{dir: true}}
			if tc.compose {
				files.exists[dir+"/compose.sh"] = true
			}
			commands := &fakeCommands{}
			_, err := newTestService(new(bytes.Buffer), commands, files, &fakeProbe{}).Up(context.Background(), UpInput{
				WorkspaceRoot: "/workspace",
				Modules:       []string{"nats"},
			})
			if err != nil {
				t.Fatalf("Up: %v", err)
			}
			call := commands.calls[0]
			if call.name != tc.wantName || strings.Join(call.args, " ") != strings.Join(tc.wantArgs, " ") {
				t.Errorf("call = %+v, want %s %v", call, tc.wantName, tc.wantArgs)
			}
			if call.stream != tc.wantStream {
				t.Errorf("stream = %v, want %v", call.stream, tc.wantStream)
			}
		})
	}
}

func TestUpWaitProbesWithTimeout(t *testing.T) {
	dir := "/infra"
	files := &fakeFiles{infraDir: dir, exists: map[string]bool{dir + "/compose.sh": true}, dirs: map[string]bool{dir: true}}
	out := new(bytes.Buffer)

	_, err := newTestService(out, &fakeCommands{}, files, &fakeProbe{}).Up(context.Background(), UpInput{
		WorkspaceRoot: "/workspace",
		Wait:          true,
		Timeout:       time.Second,
	})
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !strings.Contains(out.String(), "Probing service readiness (timeout: 1s)...") {
		t.Errorf("missing probe message: %s", out.String())
	}
	if !strings.Contains(out.String(), "Service Readiness:") {
		t.Errorf("missing readiness block: %s", out.String())
	}
	if !strings.Contains(out.String(), "NOT READY") {
		t.Errorf("expected unready services: %s", out.String())
	}
}

func TestUpMissingInfraDir(t *testing.T) {
	files := &fakeFiles{infraDir: "/missing", exists: map[string]bool{}, dirs: map[string]bool{}}
	_, err := newTestService(new(bytes.Buffer), &fakeCommands{}, files, &fakeProbe{}).Up(context.Background(), UpInput{WorkspaceRoot: "/workspace"})
	if err == nil || !strings.Contains(err.Error(), "orbit-infra directory not found at /missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownUsesComposeScriptOrDocker(t *testing.T) {
	for _, tc := range []struct {
		name     string
		compose  bool
		wantName string
		wantArgs string
	}{
		{name: "compose script", compose: true, wantName: "/infra/compose.sh", wantArgs: "down"},
		{name: "docker compose", compose: false, wantName: "docker", wantArgs: "compose down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := "/infra"
			files := &fakeFiles{infraDir: dir, exists: map[string]bool{}, dirs: map[string]bool{dir: true}}
			if tc.compose {
				files.exists[dir+"/compose.sh"] = true
			}
			commands := &fakeCommands{}
			_, err := newTestService(new(bytes.Buffer), commands, files, &fakeProbe{}).Down(context.Background(), "/workspace")
			if err != nil {
				t.Fatalf("Down: %v", err)
			}
			call := commands.calls[0]
			if call.stream {
				t.Error("expected down to use the buffered runner")
			}
			if call.name != tc.wantName || strings.Join(call.args, " ") != tc.wantArgs {
				t.Errorf("call = %+v, want %s %s", call, tc.wantName, tc.wantArgs)
			}
		})
	}
}

func TestTier2RequiresScript(t *testing.T) {
	dir := "/infra"
	files := &fakeFiles{infraDir: dir, exists: map[string]bool{}, dirs: map[string]bool{dir: true}}
	_, err := newTestService(new(bytes.Buffer), &fakeCommands{}, files, &fakeProbe{}).Tier2(context.Background(), "/workspace", nil)
	if err == nil || !strings.Contains(err.Error(), "start-tier2.sh script not found at /infra/scripts/start-tier2.sh") {
		t.Fatalf("unexpected error: %v", err)
	}

	files.exists[dir+"/scripts/start-tier2.sh"] = true
	commands := &fakeCommands{}
	res, err := newTestService(new(bytes.Buffer), commands, files, &fakeProbe{}).Tier2(context.Background(), "/workspace", []string{"orbit-auth"})
	if err != nil {
		t.Fatalf("Tier2: %v", err)
	}
	if res.Command.Name != dir+"/scripts/start-tier2.sh" {
		t.Errorf("command = %q", res.Command.Name)
	}
	if len(commands.calls) != 1 || commands.calls[0].stream {
		t.Errorf("unexpected calls: %+v", commands.calls)
	}
}

func TestCaddyActions(t *testing.T) {
	dir := "/infra"
	files := &fakeFiles{infraDir: dir, exists: map[string]bool{dir + "/compose.sh": true}, dirs: map[string]bool{dir: true}}

	t.Run("reload uses compose script", func(t *testing.T) {
		commands := &fakeCommands{}
		_, err := newTestService(new(bytes.Buffer), commands, files, &fakeProbe{}).Caddy(context.Background(), "/workspace", "reload")
		if err != nil {
			t.Fatalf("Caddy reload: %v", err)
		}
		if commands.calls[0].name != dir+"/compose.sh" {
			t.Errorf("name = %q", commands.calls[0].name)
		}
	})

	t.Run("restart uses docker", func(t *testing.T) {
		commands := &fakeCommands{}
		_, err := newTestService(new(bytes.Buffer), commands, files, &fakeProbe{}).Caddy(context.Background(), "/workspace", "restart")
		if err != nil {
			t.Fatalf("Caddy restart: %v", err)
		}
		if got := strings.Join(append([]string{commands.calls[0].name}, commands.calls[0].args...), " "); got != "docker compose restart caddy" {
			t.Errorf("call = %q", got)
		}
	})

	t.Run("logs streams", func(t *testing.T) {
		commands := &fakeCommands{}
		_, err := newTestService(new(bytes.Buffer), commands, files, &fakeProbe{}).Caddy(context.Background(), "/workspace", "logs")
		if err != nil {
			t.Fatalf("Caddy logs: %v", err)
		}
		if !commands.calls[0].stream {
			t.Error("expected caddy logs to stream")
		}
	})

	t.Run("unknown action", func(t *testing.T) {
		_, err := newTestService(new(bytes.Buffer), &fakeCommands{}, files, &fakeProbe{}).Caddy(context.Background(), "/workspace", "bogus")
		if err == nil || !strings.Contains(err.Error(), `unknown caddy action "bogus"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestPortalOutput(t *testing.T) {
	out := new(bytes.Buffer)
	res := newTestService(out, &fakeCommands{}, &fakeFiles{}, &fakeProbe{}).Portal(context.Background())

	for _, want := range []string{
		"Orbit Local Developer Portal",
		"http://localhost:10007",
		"http://auth.internal.manova.space:10000",
		"http://git.internal.manova.space:10000",
		"http://mail.internal.manova.space:10000",
		"http://grafana.internal.manova.space:10000",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if res.URL != domain.PortalURL || len(res.Endpoints) != len(domain.Endpoints()) {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestLogsPlanDoesNotRun(t *testing.T) {
	dir := "/infra"
	files := &fakeFiles{infraDir: dir, exists: map[string]bool{}, dirs: map[string]bool{dir: true}}
	commands := &fakeCommands{}

	res, err := newTestService(new(bytes.Buffer), commands, files, &fakeProbe{}).Logs(context.Background(), LogsInput{
		WorkspaceRoot: "/workspace",
		Services:      []string{"postgres"},
		Plan:          true,
	})
	if err != nil {
		t.Fatalf("Logs plan: %v", err)
	}
	if len(commands.calls) != 0 {
		t.Errorf("plan ran commands: %+v", commands.calls)
	}
	if res.Streaming {
		t.Error("plan should not report streaming")
	}
	if got := strings.Join(append([]string{res.Command.Name}, res.Command.Args...), " "); got != "docker compose logs -f postgres" {
		t.Errorf("command = %q", got)
	}
}

func TestLogsStreams(t *testing.T) {
	dir := "/infra"
	files := &fakeFiles{infraDir: dir, exists: map[string]bool{}, dirs: map[string]bool{dir: true}}
	commands := &fakeCommands{}

	res, err := newTestService(new(bytes.Buffer), commands, files, &fakeProbe{}).Logs(context.Background(), LogsInput{WorkspaceRoot: "/workspace"})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if !res.Streaming || len(commands.calls) != 1 || !commands.calls[0].stream {
		t.Errorf("expected a streaming call, got %+v", commands.calls)
	}
}

func TestProbeServicesUsesFallbackPort(t *testing.T) {
	probe := &fakeProbe{ready: map[string]bool{"127.0.0.1": true}}
	// The fake cannot distinguish ports, so report the primary port as ready.
	svc := newTestService(new(bytes.Buffer), &fakeCommands{}, &fakeFiles{}, probe)
	statuses := svc.probeServices(context.Background(), time.Millisecond)
	for _, status := range statuses {
		if !status.Ready {
			t.Errorf("service %s not ready", status.Name)
		}
		if status.Message != "Ready" {
			t.Errorf("message = %q", status.Message)
		}
	}
}
