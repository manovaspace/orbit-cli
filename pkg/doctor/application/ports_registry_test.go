package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/doctor/domain"
	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
	portregistry "github.com/manovaspace/orbit-cli/pkg/ports/infrastructure/registry"
)

// listenerPorts reports the given ports as bound and records each scan range.
type listenerPorts struct {
	bound  map[int]bool
	ranges [][2]int
}

func (l *listenerPorts) Scan(start, end int) map[int]bool {
	l.ranges = append(l.ranges, [2]int{start, end})
	out := make(map[int]bool, end-start+1)
	for p := start; p <= end; p++ {
		if l.bound[p] {
			out[p] = true
		}
	}
	return out
}

func loadFixtureRegistry(root string) (*portsdomain.Registry, error) {
	return portregistry.Load("../../ports/infrastructure/registry/testdata/ports.yaml")
}

func TestPortsCheckWithRegistryFlagsUnlistedListeners(t *testing.T) {
	ports := &listenerPorts{bound: map[int]bool{10500: true, 10010: true, 10026: true}}
	svc := &Service{Ports: ports, Registry: loadFixtureRegistry}
	result := svc.portsCheck("/workspace").Run(context.Background())
	if len(result) != 1 {
		t.Fatalf("results = %+v", result)
	}
	got := result[0]
	if got.Status != domain.StatusWarning {
		t.Fatalf("status = %v, want warning: %+v", got.Status, got)
	}
	if !strings.Contains(got.Message, "10500") || !strings.Contains(got.Message, "10026") {
		t.Errorf("message must name 10500 and 10026: %s", got.Message)
	}
	if strings.Contains(got.Message, "10010") {
		t.Errorf("registered listener 10010 reported: %s", got.Message)
	}
	if len(ports.ranges) != 1 || ports.ranges[0] != [2]int{10000, 17999} {
		t.Errorf("scan ranges = %v, want [10000 17999]", ports.ranges)
	}
}

func TestPortsCheckWithRegistryAllListedIsOK(t *testing.T) {
	ports := &listenerPorts{bound: map[int]bool{10010: true}}
	svc := &Service{Ports: ports, Registry: loadFixtureRegistry}
	got := svc.portsCheck("/workspace").Run(context.Background())[0]
	if got.Status != domain.StatusOK {
		t.Fatalf("status = %v: %+v", got.Status, got)
	}
}

func TestPortsCheckWithoutRegistryKeepsFixedRange(t *testing.T) {
	ports := &listenerPorts{bound: map[int]bool{10500: true}}
	svc := &Service{Ports: ports, Registry: func(string) (*portsdomain.Registry, error) { return nil, nil }}
	got := svc.portsCheck("/workspace").Run(context.Background())[0]
	if len(ports.ranges) != 1 || ports.ranges[0] != [2]int{10000, 10300} {
		t.Fatalf("scan ranges = %v, want the fixed [10000 10300]", ports.ranges)
	}
	if got.Status != domain.StatusOK {
		t.Fatalf("10500 is outside the fixed range; status = %v", got.Status)
	}
}

func TestPortsCheckUnreadableRegistryWarns(t *testing.T) {
	ports := &listenerPorts{}
	svc := &Service{Ports: ports, Registry: func(string) (*portsdomain.Registry, error) {
		return nil, errors.New("bad yaml")
	}}
	got := svc.portsCheck("/workspace").Run(context.Background())[0]
	if got.Status != domain.StatusWarning || !strings.Contains(got.Message, "bad yaml") {
		t.Fatalf("result = %+v", got)
	}
}

func TestPortsCheckWithoutRootUsesFixedRange(t *testing.T) {
	ports := &listenerPorts{}
	svc := &Service{Ports: ports, Registry: loadFixtureRegistry}
	svc.portsCheck("").Run(context.Background())
	if len(ports.ranges) != 1 || ports.ranges[0] != [2]int{10000, 10300} {
		t.Fatalf("scan ranges = %v", ports.ranges)
	}
}
