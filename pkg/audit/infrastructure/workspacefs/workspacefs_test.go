package workspacefs

import (
	"path/filepath"
	"strings"
	"testing"
)

// fixtureWorkspace is the audit workspace fixture.
func fixtureWorkspace(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "workspace"))
	if err != nil {
		t.Fatalf("fixture root: %v", err)
	}
	return root
}

func TestInstalledExportsResolvesWorkspaceMember(t *testing.T) {
	repoDir := filepath.Join(fixtureWorkspace(t), "clients", "demo", "workspace-member")
	fromFile := filepath.Join(repoDir, "apps", "web", "src", "app", "login", "page.tsx")

	got, ok := New().InstalledExports(repoDir, fromFile, "@manovaspace/versioning")
	if !ok {
		t.Fatal("@manovaspace/versioning workspace member not resolved")
	}
	want := filepath.Join(repoDir, "packages", "versioning", "src", "index.ts")
	if got != want {
		t.Errorf("resolved %q, want %q", got, want)
	}
}

func TestInstalledExportsResolvesSubpathExport(t *testing.T) {
	repoDir := filepath.Join(fixtureWorkspace(t), "clients", "demo", "subpath")
	fromFile := filepath.Join(repoDir, "apps", "template", "src", "app", "persistent-providers.tsx")

	got, ok := New().InstalledExports(repoDir, fromFile, "@manovaspace/pwa/react")
	if !ok {
		t.Fatal("@manovaspace/pwa/react subpath export not resolved")
	}
	want := filepath.Join(repoDir, "apps", "template", "node_modules", "@manovaspace", "pwa", "dist", "react", "serwist-shell.d.ts")
	if got != want {
		t.Errorf("resolved %q, want %q", got, want)
	}
}

func TestInstalledExportsReadsImportingPackageNodeModules(t *testing.T) {
	repoDir := filepath.Join(fixtureWorkspace(t), "clients", "demo", "subpath")
	fromFile := filepath.Join(repoDir, "apps", "template", "src", "app", "persistent-providers.tsx")

	got, ok := New().InstalledExports(repoDir, fromFile, "@manovaspace/pwa")
	if !ok {
		t.Fatal("@manovaspace/pwa not resolved from the importing package")
	}
	if want := filepath.Join(repoDir, "apps", "template", "node_modules"); !strings.HasPrefix(got, want+string(filepath.Separator)) {
		t.Errorf("resolved %q, want a path under %q", got, want)
	}
}

func TestInstalledExportsPrefersNewestStoredCopy(t *testing.T) {
	repoDir := filepath.Join(fixtureWorkspace(t), "clients", "demo", "stale-store")
	fromFile := filepath.Join(repoDir, "spec", "source.ts")

	got, ok := New().InstalledExports(repoDir, fromFile, "@manovaspace/ui")
	if !ok {
		t.Fatal("@manovaspace/ui not resolved from a node_modules store copy")
	}
	want := filepath.Join(repoDir, "zzz", "node_modules", "@manovaspace", "ui", "dist", "index.d.ts")
	if got != want {
		t.Errorf("resolved %q, want the newer copy %q", got, want)
	}
}
