package application

import (
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
)

func TestRoutesForNilRegistryIsUnverified(t *testing.T) {
	_, err := RoutesFor(nil)
	if clierr.ExitCode(err) != clierr.ExitUnverified {
		t.Errorf("exit = %d, want %d (%v)", clierr.ExitCode(err), clierr.ExitUnverified, err)
	}
}

func TestRouteServiceWritesCaddyfileToPath(t *testing.T) {
	reg := &portsdomain.Registry{Services: []portsdomain.Service{
		{Port: 10001, Name: "orbit-api-gateway", Runs: "mac", Localhost: "gateway"},
	}}
	routes, err := RoutesFor(reg)
	if err != nil {
		t.Fatal(err)
	}
	files := &fakeTextFiles{}
	svc := &RouteService{Files: files}
	if err := svc.WriteCaddyfile("/out/Caddyfile", routes); err != nil {
		t.Fatalf("WriteCaddyfile: %v", err)
	}
	got := string(files.written["/out/Caddyfile"])
	if got != svc.Caddyfile(routes) || !strings.Contains(got, "http://gateway.localhost {\n\treverse_proxy 127.0.0.1:10001\n}\n") {
		t.Errorf("written = %q", got)
	}
}
