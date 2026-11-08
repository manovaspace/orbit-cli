package application

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
)

// fakeProbe reports every port free unless listed, and records scans.
type fakeProbe struct {
	inUse map[int]bool
	scans [][2]int
}

func (f *fakeProbe) Available(port int) bool { return !f.inUse[port] }

func (f *fakeProbe) Scan(start, end int) map[int]bool {
	f.scans = append(f.scans, [2]int{start, end})
	results := make(map[int]bool, end-start+1)
	for port := start; port <= end; port++ {
		results[port] = f.inUse[port]
	}
	return results
}

func newService(out *bytes.Buffer, probe *fakeProbe) *Service {
	return &Service{Ports: probe, Out: out}
}

func TestListWithoutScanClaimsNoListenerKnowledge(t *testing.T) {
	out := new(bytes.Buffer)
	svc := newService(out, &fakeProbe{})
	res, err := svc.List(context.Background(), ListOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "[FREE]") || strings.Contains(text, "in use") {
		t.Errorf("disabled scan claims listener knowledge: %s", text)
	}
	if !strings.Contains(text, "UNVERIFIED") {
		t.Errorf("disabled scan missing unverified: %s", text)
	}
	if len(res.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(res.Projects))
	}
	if res.Projects[0].Scanned {
		t.Error("project marked scanned without --scan")
	}
	if len(res.Projects[0].Slots) != 10 {
		t.Errorf("slots = %d, want 10", len(res.Projects[0].Slots))
	}
}

func TestListScanMarksUnavailableSlots(t *testing.T) {
	out := new(bytes.Buffer)
	svc := newService(out, &fakeProbe{inUse: map[int]bool{10000: true}})
	res, err := svc.List(context.Background(), ListOptions{Scan: true, Limit: 1, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(res.Projects))
	}
	project := res.Projects[0]
	if !project.Scanned || project.Active != 1 {
		t.Errorf("project = %+v, want scanned with one active port", project)
	}
	if strings.Contains(out.String(), "UNVERIFIED") {
		t.Errorf("scanned output still claims unverified: %s", out.String())
	}
	if !strings.Contains(out.String(), "UNAVAILABLE AT PROBE") {
		t.Errorf("missing unavailable badge: %s", out.String())
	}
}

func TestListPagination(t *testing.T) {
	out := new(bytes.Buffer)
	svc := newService(out, &fakeProbe{})
	res, err := svc.List(context.Background(), ListOptions{Limit: 4, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalProjects != 6 || res.TotalPages != 2 {
		t.Fatalf("pagination = %+v", res)
	}
	if len(res.Projects) != 2 || res.Projects[0].ID != 4 {
		t.Fatalf("second page = %+v", res.Projects)
	}
	if !strings.Contains(out.String(), "Showing 5-6 of 6 projects") {
		t.Errorf("missing page summary: %s", out.String())
	}
}

func TestAllocateSuggestsWithoutReserving(t *testing.T) {
	out := new(bytes.Buffer)
	svc := newService(out, &fakeProbe{})
	res, err := svc.Allocate(context.Background(), AllocateInput{Project: "0", Service: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Port != 10010 || res.Slot != 10 || res.Reserved {
		t.Fatalf("allocation = %+v", res)
	}
	if !strings.Contains(out.String(), "not reserved") {
		t.Errorf("assignment implies reservation: %s", out.String())
	}
	if res.EnvVariable != "export FIXTURE_PORT=10010" {
		t.Errorf("export = %q", res.EnvVariable)
	}
}

func TestAllocateUnknownProjectIsUsage(t *testing.T) {
	_, err := newService(new(bytes.Buffer), &fakeProbe{}).Allocate(context.Background(), AllocateInput{Project: "nope", Service: "x"})
	if err == nil {
		t.Fatal("unknown project accepted")
	}
	if clierr.ExitCode(err) != clierr.ExitUsage {
		t.Errorf("exit = %d, want usage", clierr.ExitCode(err))
	}
}

func TestAllocateExhaustionIsFailed(t *testing.T) {
	probe := &fakeProbe{inUse: map[int]bool{}}
	for port := 10110; port <= 10149; port++ {
		probe.inUse[port] = true
	}
	_, err := newService(new(bytes.Buffer), probe).Allocate(context.Background(), AllocateInput{Project: "falak", Service: "x"})
	if err == nil {
		t.Fatal("exhausted block allocated")
	}
	if clierr.ExitCode(err) != clierr.ExitFailed {
		t.Errorf("exit = %d, want failed", clierr.ExitCode(err))
	}
}
