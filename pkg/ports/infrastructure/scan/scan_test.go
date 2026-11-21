package scan

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/ports/application"
	"github.com/manovaspace/orbit-cli/pkg/ports/domain"
	"github.com/manovaspace/orbit-cli/pkg/ports/infrastructure/registry"
)

const fixtureRoot = "testdata/checkroot"

func fixtureRegistry(t *testing.T) *domain.Registry {
	t.Helper()
	reg, err := registry.Load(filepath.Join(fixtureRoot, "handbook", "agents", "workspace", "ports.yaml"))
	if err != nil {
		t.Fatalf("load fixture registry: %v", err)
	}
	return reg
}

func TestFilesSkipsTestsAndNodeModulesAndMapsRepos(t *testing.T) {
	files, err := New().Files(fixtureRoot)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string, len(files))
	for _, f := range files {
		got[f.Rel] = f.Repo
	}
	want := map[string]string{
		"clients/manova/falak/src/app.ts":           "clients/manova/falak",
		"clients/manova/manova-infra/Caddyfile":     "clients/manova/manova-infra",
		"clients/manova/waypoint-api/.env.example":  "clients/manova/waypoint-api",
		"clients/manova/waypoint-api/package.json":  "clients/manova/waypoint-api",
		"clients/manova/waypoint-api/src/server.go": "clients/manova/waypoint-api",
		"manovaspace/design-system/tools/sb.mjs":    "manovaspace/design-system",
		"orbit/orbit-infra/cmd/ingress/main.go":     "orbit/orbit-infra",
		"orbit/orbit-infra/docker-compose.yml":      "orbit/orbit-infra",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v\nwant    %v", got, want)
	}
}

func TestCheckerFindsEveryKindInFixture(t *testing.T) {
	reg := fixtureRegistry(t)
	result, err := application.Checker{Source: New()}.Run(fixtureRoot, "ports.yaml", reg, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 8 {
		t.Errorf("files = %d, want 8", result.Files)
	}
	wantCounts := map[string]int{"UNREGISTERED": 4, "RETIRED": 2, "WRONG_REPO": 1, "RESERVED": 1, "OPEN_BIND": 4}
	if !reflect.DeepEqual(result.Counts, wantCounts) {
		t.Errorf("counts = %v, want %v", result.Counts, wantCounts)
	}
	if result.Failing != 8 || result.Warnings != 4 {
		t.Errorf("failing = %d warnings = %d, want 8 and 4", result.Failing, result.Warnings)
	}

	strict, err := application.Checker{Source: New()}.Run(fixtureRoot, "ports.yaml", reg, true)
	if err != nil {
		t.Fatal(err)
	}
	if strict.Failing != 12 || strict.Warnings != 0 || !strict.Strict {
		t.Errorf("strict failing = %d warnings = %d", strict.Failing, strict.Warnings)
	}

	for _, f := range result.Findings {
		if f.Repo == "" {
			t.Errorf("finding without repository: %+v", f)
		}
		if f.File == "clients/manova/waypoint-api/server_test.go" || f.Port == 10999 {
			t.Errorf("excluded file or path reported: %+v", f)
		}
	}
}

func TestCheckerWithoutRegistryClassifiesEverythingUnknown(t *testing.T) {
	empty := &domain.Registry{}
	result, err := application.Checker{Source: New()}.Run(fixtureRoot, "", empty, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts["UNREGISTERED"] == 0 || result.Counts["WRONG_REPO"] != 0 {
		t.Errorf("counts = %v", result.Counts)
	}
}
