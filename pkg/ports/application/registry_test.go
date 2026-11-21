package application

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/ports/domain"
)

func smallRegistry(retired ...int) *domain.Registry {
	reg := &domain.Registry{
		Projects: []domain.Project{{ID: 0, Name: "orbit-platform"}, {ID: 1, Name: "manova"}},
		Services: []domain.Service{
			{Port: 10030, Project: "orbit-platform", Name: "handbook", Runs: "mac", Repos: []string{"handbook"}},
			{Port: 10010, Project: "orbit-platform", Name: "orbit-auth-grpc", Runs: "mac", Repos: []string{"orbit/orbit-auth"}},
			{Port: 10011, Project: "orbit-platform", Name: "orbit-auth-health", Runs: "mac", Repos: []string{"orbit/orbit-auth"}, Note: "also serves GET /v1/flags"},
		},
	}
	for _, p := range retired {
		reg.Retired = append(reg.Retired, domain.Retired{Port: p, Was: "legacy", Retired: "2026-11-21"})
	}
	return reg
}

func TestAllocateWithoutRegistryUsesLowestFreeSlot(t *testing.T) {
	svc := newService(new(bytes.Buffer), &fakeProbe{})
	res, err := svc.Allocate(context.Background(), AllocateInput{Project: "orbit-platform", Service: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Port != 10010 {
		t.Fatalf("port = %d, want 10010 (no registry)", res.Port)
	}
}

func TestAllocateSkipsRegisteredAndRetired(t *testing.T) {
	svc := newService(new(bytes.Buffer), &fakeProbe{})
	svc.Registry = smallRegistry(10012)
	res, err := svc.Allocate(context.Background(), AllocateInput{Project: "orbit-platform", Service: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Port != 10013 {
		t.Fatalf("port = %d, want 10013 (10010-10011 registered, 10012 retired)", res.Port)
	}
}

func TestAllocateSkipsProbeBusyPorts(t *testing.T) {
	svc := newService(new(bytes.Buffer), &fakeProbe{inUse: map[int]bool{10010: true}})
	svc.Registry = smallRegistry()
	res, err := svc.Allocate(context.Background(), AllocateInput{Project: "0", Service: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Port != 10012 {
		t.Fatalf("port = %d, want 10012", res.Port)
	}
}

func TestAllocateUsesRegistryProjectMapping(t *testing.T) {
	svc := newService(new(bytes.Buffer), &fakeProbe{})
	svc.Registry = &domain.Registry{Projects: []domain.Project{{ID: 7, Name: "newproj"}}}
	res, err := svc.Allocate(context.Background(), AllocateInput{Project: "newproj", Service: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectID != 7 || res.Port != 10350+10 {
		t.Fatalf("result = %+v, want project 7 port 10360", res)
	}
	if _, err := svc.Allocate(context.Background(), AllocateInput{Project: "orbit-platform", Service: "fixture"}); err == nil {
		t.Fatal("registry without orbit-platform must not resolve it")
	}
}

func TestAllocateRejectsBlockPastMaxPort(t *testing.T) {
	svc := newService(new(bytes.Buffer), &fakeProbe{})
	_, err := svc.Allocate(context.Background(), AllocateInput{Project: "1110", Service: "fixture"})
	if err == nil {
		t.Fatal("project 1110 block exceeds 65535, want error")
	}
	if clierr.Code(err) != clierr.CodeUsage || !strings.Contains(err.Error(), "past port 65535") {
		t.Fatalf("err = %v (code %s)", err, clierr.Code(err))
	}
	res, err := svc.Allocate(context.Background(), AllocateInput{Project: "1109", Service: "fixture"})
	if err != nil {
		t.Fatalf("project 1109 is the largest valid ID: %v", err)
	}
	if res.Port != 65460 {
		t.Fatalf("port = %d, want 65460", res.Port)
	}
	if MaxProjectID() != 1109 {
		t.Fatalf("MaxProjectID = %d, want 1109", MaxProjectID())
	}
}

func TestListServicesPagesAndProbes(t *testing.T) {
	probe := &fakeProbe{inUse: map[int]bool{10011: true}}
	svc := newService(new(bytes.Buffer), probe)
	svc.Registry = smallRegistry()

	all := svc.ListServices(context.Background(), ListOptions{})
	if !all.Probed || all.TotalServices != 3 || len(all.Services) != 3 {
		t.Fatalf("all = %+v", all)
	}
	if all.Services[0].Port != 10010 || all.Services[2].Port != 10030 {
		t.Fatalf("services not sorted by port: %+v", all.Services)
	}
	if !all.Services[1].Listening || all.Services[0].Listening {
		t.Fatalf("listening flags wrong: %+v", all.Services)
	}
	if all.Services[1].Note != "also serves GET /v1/flags" || all.Services[2].Repos[0] != "handbook" {
		t.Fatalf("fields not carried through: %+v", all.Services)
	}

	page2 := svc.ListServices(context.Background(), ListOptions{Page: 2, Limit: 2})
	if page2.TotalPages != 2 || len(page2.Services) != 1 || page2.Services[0].Port != 10030 {
		t.Fatalf("page 2 = %+v", page2)
	}
	beyond := svc.ListServices(context.Background(), ListOptions{Page: 9, Limit: 2})
	if len(beyond.Services) != 0 || beyond.Services == nil {
		t.Fatalf("beyond page = %+v", beyond)
	}
}

func TestListServicesWithoutRegistryIsEmpty(t *testing.T) {
	svc := newService(new(bytes.Buffer), &fakeProbe{})
	res := svc.ListServices(context.Background(), ListOptions{})
	if len(res.Services) != 0 || res.TotalServices != 0 {
		t.Fatalf("res = %+v", res)
	}
}
