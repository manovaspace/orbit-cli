package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	portsapp "github.com/manovaspace/orbit-cli/pkg/ports/application"
	"github.com/manovaspace/orbit-cli/pkg/render/rendertest"
)

// batch1Probe answers every port probe deterministically.
type batch1Probe struct{}

func (batch1Probe) Available(int) bool         { return true }
func (batch1Probe) Scan(int, int) map[int]bool { return map[int]bool{} }

// normalizeRoot replaces the throwaway workspace root so the golden files do
// not depend on the machine or test run.
func normalizeRoot(out, root string) string {
	return strings.ReplaceAll(out, root, "<workspace>")
}

func writeBatch1Manifest(t *testing.T, root, body string) string {
	t.Helper()
	path := filepath.Join(root, "workspace.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const batch1Manifest = `version: "1"
workspace: "batch1"
groups:
  test:
    path: ""
    repositories:
      - name: "missing-repo"
        path: "missing-repo"
`

// TestBatch1JSONStability pins the JSON output of every command this batch
// migrates to pkg/render. The goldens were recorded before the text output
// changed; the batch must not move a single byte of JSON.
func TestBatch1JSONStability(t *testing.T) {
	t.Run("status_success", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ORBIT_WORKSPACE", root)
		manifest := writeBatch1Manifest(t, root, batch1Manifest)
		out, err := runRootForTest(t, "status", "all", "--manifest", manifest, "--json")
		if err != nil {
			t.Fatalf("status --json: %v\n%s", err, out)
		}
		rendertest.Golden(t, "json/status_success", normalizeRoot(out, root))
	})

	t.Run("status_missing_manifest", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ORBIT_WORKSPACE", root)
		out, err := runRootForTest(t, "status", "all", "--manifest", filepath.Join(root, "absent.yaml"), "--json")
		if err == nil {
			t.Fatal("expected a missing manifest to fail")
		}
		rendertest.Golden(t, "json/status_missing_manifest", normalizeRoot(out, root))
	})

	t.Run("sync_failure", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ORBIT_WORKSPACE", root)
		manifest := writeBatch1Manifest(t, root, batch1Manifest)
		out, err := runRootForTest(t, "sync", "all", "--manifest", manifest, "--json")
		if err == nil {
			t.Fatal("expected sync of a missing repository to fail")
		}
		rendertest.Golden(t, "json/sync_failure", normalizeRoot(out, root))
	})

	t.Run("migrate_status_success", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ORBIT_WORKSPACE", root)
		out, err := runRootForTest(t, "migrate", "status", "--json")
		if err != nil {
			t.Fatalf("migrate status --json: %v\n%s", err, out)
		}
		rendertest.Golden(t, "json/migrate_status_success", normalizeRoot(out, root))
	})

	t.Run("port_list_success", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		out, err := runRootForTest(t, "port", "list", "--json", "--limit", "1")
		if err != nil {
			t.Fatalf("port list --json: %v\n%s", err, out)
		}
		rendertest.Golden(t, "json/port_list_success", out)
	})

	t.Run("port_allocate_success", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		previous := portProbe
		portProbe = batch1Probe{}
		t.Cleanup(func() { portProbe = previous })

		out, err := runRootForTest(t, "port", "allocate", "0", "fixture", "--json")
		if err != nil {
			t.Fatalf("port allocate --json: %v\n%s", err, out)
		}
		rendertest.Golden(t, "json/port_allocate_success", out)
	})

	t.Run("assets_status_success", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repo, _ := setupAssetsTest(t)
		payload := []byte("pdf")
		writeAssetManifest(t, repo, "docs/a.pdf", payload)
		if err := os.WriteFile(filepath.Join(repo, "docs", "a.pdf"), payload, 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := runRootForTest(t, "assets", "status", "--json")
		if err != nil {
			t.Fatalf("assets status --json: %v\n%s", err, out)
		}
		rendertest.Golden(t, "json/assets_status_success", out)
	})

	t.Run("assets_status_failure", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repo, _ := setupAssetsTest(t)
		writeAssetManifest(t, repo, "docs/a.pdf", []byte("pdf"))
		out, err := runRootForTest(t, "assets", "status", "--json")
		if err == nil {
			t.Fatal("expected missing assets to fail")
		}
		rendertest.Golden(t, "json/assets_status_failure", out)
	})

	t.Run("env_check_success", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ORBIT_WORKSPACE", root)
		if err := os.WriteFile(filepath.Join(root, ".env.schema.yaml"), []byte("version: '1'\nvariables:\n - name: PORT\n   type: integer\n   required: true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".env"), []byte("PORT=10050\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := runRootForTest(t, "env", "check", root, "--json")
		if err != nil {
			t.Fatalf("env check --json: %v\n%s", err, out)
		}
		rendertest.Golden(t, "json/env_check_success", normalizeRoot(out, root))
	})

	t.Run("env_check_failure", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ORBIT_WORKSPACE", root)
		if err := os.WriteFile(filepath.Join(root, ".env.schema.yaml"), []byte("version: '1'\nvariables:\n - name: REQUIRED_VALUE\n   type: string\n   required: true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := runRootForTest(t, "env", "check", root, "--json")
		if err == nil {
			t.Fatal("expected a missing required variable to fail")
		}
		rendertest.Golden(t, "json/env_check_failure", normalizeRoot(out, root))
	})

	t.Run("doctor_success", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		previousService, previousPaths := doctorNewService, doctorWorkspacePaths
		t.Cleanup(func() {
			doctorNewService, doctorWorkspacePaths = previousService, previousPaths
		})
		doctorNewService = healthyDoctorService(stubProbe{}, nil)
		doctorWorkspacePaths = func() (string, string) { return "", "" }
		out, err := runRootForTest(t, "doctor", "--json")
		if err != nil {
			t.Fatalf("doctor --json: %v\n%s", err, out)
		}
		rendertest.Golden(t, "json/doctor_success", out)
	})

	t.Run("doctor_failure", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		previousService, previousPaths := doctorNewService, doctorWorkspacePaths
		t.Cleanup(func() {
			doctorNewService, doctorWorkspacePaths = previousService, previousPaths
		})
		doctorNewService = healthyDoctorService(stubProbe{gitMissing: true}, nil)
		doctorWorkspacePaths = func() (string, string) { return "", "" }
		out, err := runRootForTest(t, "doctor", "--json")
		if err == nil {
			t.Fatal("expected doctor findings to fail")
		}
		rendertest.Golden(t, "json/doctor_failure", out)
	})
}

var _ portsapp.Prober = batch1Probe{}
