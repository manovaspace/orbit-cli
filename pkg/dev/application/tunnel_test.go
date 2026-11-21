package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/dev/domain"
	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
)

func tunnelTestRegistry() *portsdomain.Registry {
	return &portsdomain.Registry{
		Services: []portsdomain.Service{
			{Port: 10000, Name: "postgres", Runs: "shared"},
			{Port: 10006, Name: "nats", Runs: "shared", Bind: "10.0.0.1"},
			{Port: 10001, Name: "gateway", Runs: "mac", Localhost: "gateway"},
		},
		Exceptions: []portsdomain.Exception{{Port: 10334, Name: "redis", Runs: "shared"}},
	}
}

type fakeAttached struct {
	argv []string
	err  error
}

func (f *fakeAttached) Run(_ context.Context, argv []string) error {
	f.argv = argv
	return f.err
}

type fakeTextFiles struct {
	read    map[string][]byte
	readErr error
	written map[string][]byte
}

func (f *fakeTextFiles) ReadFile(path string) ([]byte, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	data, ok := f.read[path]
	if !ok {
		return nil, errors.New("no such file")
	}
	return data, nil
}

func (f *fakeTextFiles) WriteFile(path string, data []byte) error {
	if f.written == nil {
		f.written = map[string][]byte{}
	}
	f.written[path] = data
	return nil
}

func TestPlanTunnelBuildsCommand(t *testing.T) {
	plan, err := PlanTunnel(TunnelInput{Registry: tunnelTestRegistry(), Host: " demo@203.0.113.10 ", Port: 2222})
	if err != nil {
		t.Fatalf("PlanTunnel: %v", err)
	}
	if plan.Host != "demo@203.0.113.10" || plan.Port != 2222 {
		t.Errorf("host/port = %q %d", plan.Host, plan.Port)
	}
	if len(plan.Forwards) != 3 || plan.Forwards[0].Name != "postgres" || plan.Forwards[2].Name != "redis" {
		t.Errorf("forwards = %+v", plan.Forwards)
	}
	if plan.Command[0] != "ssh" || plan.Command[len(plan.Command)-1] != "demo@203.0.113.10" {
		t.Errorf("command = %q", plan.Command)
	}
}

func TestPlanTunnelOnlyRestrictsForwards(t *testing.T) {
	plan, err := PlanTunnel(TunnelInput{Registry: tunnelTestRegistry(), Host: "demo@203.0.113.10", Port: 22, Only: []string{"nats"}})
	if err != nil {
		t.Fatalf("PlanTunnel: %v", err)
	}
	if len(plan.Forwards) != 1 || plan.Forwards[0].Name != "nats" || plan.Forwards[0].Bind != "10.0.0.1" {
		t.Errorf("forwards = %+v", plan.Forwards)
	}
}

func TestPlanTunnelErrors(t *testing.T) {
	tests := []struct {
		name     string
		in       TunnelInput
		exit     int
		contains string
	}{
		{
			name:     "missing host",
			in:       TunnelInput{Registry: tunnelTestRegistry(), Port: 22},
			exit:     clierr.ExitUsage,
			contains: "no tunnel host: run orbit config set dev.tunnel_host user@host",
		},
		{
			name:     "bad port",
			in:       TunnelInput{Registry: tunnelTestRegistry(), Host: "demo@203.0.113.10", Port: 70000},
			exit:     clierr.ExitUsage,
			contains: "between 1 and 65535",
		},
		{
			name:     "unknown only name",
			in:       TunnelInput{Registry: tunnelTestRegistry(), Host: "demo@203.0.113.10", Port: 22, Only: []string{"bogus"}},
			exit:     clierr.ExitUsage,
			contains: "valid names: nats, postgres, redis",
		},
		{
			name:     "no registry",
			in:       TunnelInput{Host: "demo@203.0.113.10", Port: 22},
			exit:     clierr.ExitUnverified,
			contains: "no port registry",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PlanTunnel(tt.in)
			if err == nil {
				t.Fatal("want error")
			}
			if got := clierr.ExitCode(err); got != tt.exit {
				t.Errorf("exit = %d, want %d (%v)", got, tt.exit, err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("error = %q, want it to contain %q", err, tt.contains)
			}
		})
	}
}

func TestTunnelServiceRunUsesPlanCommand(t *testing.T) {
	plan, err := PlanTunnel(TunnelInput{Registry: tunnelTestRegistry(), Host: "demo@203.0.113.10", Port: 22})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAttached{}
	svc := &TunnelService{Attached: fake}
	if err := svc.Run(context.Background(), plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(fake.argv, plan.Command) {
		t.Errorf("argv = %q, want %q", fake.argv, plan.Command)
	}
}

func TestTunnelServiceSSHConfig(t *testing.T) {
	plan, err := PlanTunnel(TunnelInput{Registry: tunnelTestRegistry(), Host: "demo@203.0.113.10", Port: 2222, Only: []string{"postgres"}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := (&TunnelService{}).SSHConfig(plan)
	if err != nil {
		t.Fatalf("SSHConfig: %v", err)
	}
	if !strings.HasPrefix(text, "Host orbit-dev\n") || !strings.Contains(text, "LocalForward 127.0.0.1:10000 127.0.0.1:10000\n") {
		t.Errorf("block = %q", text)
	}
}

func TestAuthorizedKeyLineReadAndValidate(t *testing.T) {
	forwards := []domain.Forward{{Name: "postgres", Port: 10000, Bind: "127.0.0.1"}}

	missing := &TunnelService{Files: &fakeTextFiles{}}
	if _, err := missing.AuthorizedKeyLine(forwards, "/nope.pub"); clierr.ExitCode(err) != clierr.ExitFailed {
		t.Errorf("missing file exit = %d, want %d (%v)", clierr.ExitCode(err), clierr.ExitFailed, err)
	}

	bad := &TunnelService{Files: &fakeTextFiles{read: map[string][]byte{"/bad.pub": []byte("not a key\n")}}}
	_, err := bad.AuthorizedKeyLine(forwards, "/bad.pub")
	if clierr.ExitCode(err) != clierr.ExitUsage || !strings.Contains(err.Error(), "/bad.pub") {
		t.Errorf("bad file = %d %v, want usage error naming the file", clierr.ExitCode(err), err)
	}
}

func TestSelectForwardsNilRegistryIsUnverified(t *testing.T) {
	if _, err := SelectForwards(nil, nil); clierr.ExitCode(err) != clierr.ExitUnverified {
		t.Errorf("exit = %d, want %d", clierr.ExitCode(err), clierr.ExitUnverified)
	}
}

func TestPlanTunnelCarriesIdentity(t *testing.T) {
	plan, err := PlanTunnel(TunnelInput{Registry: tunnelTestRegistry(), Host: "demo@203.0.113.10", Port: 22, Identity: "/k/id", Only: []string{"postgres"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Identity != "/k/id" {
		t.Errorf("identity = %q", plan.Identity)
	}
	want := []string{"ssh", "-N", "-p", "22", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3", "-i", "/k/id", "-o", "IdentitiesOnly=yes", "-L", "127.0.0.1:10000:127.0.0.1:10000", "demo@203.0.113.10"}
	if !reflect.DeepEqual(plan.Command, want) {
		t.Errorf("command = %q", plan.Command)
	}
	text, err := (&TunnelService{}).SSHConfig(plan)
	if err != nil || !strings.Contains(text, "    IdentityFile /k/id\n    IdentitiesOnly yes\n") {
		t.Errorf("ssh config = %q, %v", text, err)
	}
}
