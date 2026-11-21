package registry

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/ports/domain"
)

const fixturePath = "testdata/ports.yaml"

func TestLoadFixtureRegistry(t *testing.T) {
	reg, err := Load(fixturePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reg.Version != 1 || reg.Base != 10000 || reg.BlockSize != 50 || reg.FixedSlots != 10 {
		t.Fatalf("header = version %d base %d block %d fixed %d", reg.Version, reg.Base, reg.BlockSize, reg.FixedSlots)
	}
	if len(reg.Projects) != 6 || len(reg.Services) == 0 || len(reg.Reserved) != 2 || len(reg.Exceptions) != 3 || len(reg.Retired) != 5 {
		t.Fatalf("counts projects=%d services=%d reserved=%d exceptions=%d retired=%d",
			len(reg.Projects), len(reg.Services), len(reg.Reserved), len(reg.Exceptions), len(reg.Retired))
	}
	if got := reg.Retired[0].Retired; got != "2026-11-21" {
		t.Errorf("retired date = %q, want 2026-11-21", got)
	}
	var postgres domain.Service
	for _, s := range reg.Services {
		if s.Name == "postgres" {
			postgres = s
		}
	}
	if postgres.Port != 10000 || postgres.Runs != "shared" || !reflect.DeepEqual(postgres.Repos, []string{"orbit/orbit-infra"}) {
		t.Errorf("postgres = %+v", postgres)
	}
}

func TestProjectMappingAgreesWithDefault(t *testing.T) {
	reg, err := Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	got := reg.ProjectMapping()
	if !reflect.DeepEqual(map[string]int(got), map[string]int(domain.DefaultProjectMapping)) {
		t.Errorf("registry mapping %v != DefaultProjectMapping %v", got, domain.DefaultProjectMapping)
	}
	if got["falak"] != 2 || got["jouybari"] != 3 || got["jtash"] != 4 || got["kohan_kherad"] != 5 {
		t.Errorf("client projects = %v", got)
	}
}

func TestLookupEachClass(t *testing.T) {
	reg, err := Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		port int
		kind domain.OwnerKind
		name string
	}{
		{10010, domain.OwnerService, "orbit-auth-grpc"},
		{10005, domain.OwnerReserved, "health-aggregate"},
		{10334, domain.OwnerException, "redis"},
		{10026, domain.OwnerRetired, "unleash"},
		{10999, domain.OwnerNone, ""},
	}
	for _, tt := range tests {
		got := reg.Lookup(tt.port)
		if got.Kind != tt.kind || got.Name != tt.name || got.Port != tt.port {
			t.Errorf("Lookup(%d) = %+v, want kind %s name %q", tt.port, got, tt.kind, tt.name)
		}
	}
	if owner := reg.Lookup(10010); len(owner.Repos) == 0 || owner.Service == nil {
		t.Errorf("service owner lacks repos or entry: %+v", owner)
	}
}

func TestTakenCoversEveryClass(t *testing.T) {
	reg, err := Load(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	taken := reg.Taken()
	for _, port := range []int{10010, 10005, 10334, 10026} {
		if !taken[port] {
			t.Errorf("Taken missing %d", port)
		}
	}
	if taken[10033] {
		t.Errorf("Taken claims unregistered 10033")
	}
}

func TestFindMissingIsErrNotFound(t *testing.T) {
	if _, err := Find(t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find(empty root) err = %v, want ErrNotFound", err)
	}
	if _, err := Find(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find(\"\") err = %v, want ErrNotFound", err)
	}
}

func TestFindReadsRegistryUnderRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "handbook", "agents", "workspace")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ports.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(reg.Services) == 0 {
		t.Fatal("Find returned an empty registry")
	}
}

func TestParseRejectsDuplicatePort(t *testing.T) {
	_, err := Parse([]byte(`version: 1
services:
  - { port: 10010, project: orbit-platform, name: a, proto: http, runs: mac, repos: [x] }
retired:
  - { port: 10010, was: b, retired: 2026-11-21 }
`))
	if err == nil || !strings.Contains(err.Error(), "listed twice") {
		t.Fatalf("Parse duplicate err = %v", err)
	}
}
