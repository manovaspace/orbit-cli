package spec

import "testing"

func TestPackagesEmbedded(t *testing.T) {
	pkgs, err := Packages()
	if err != nil {
		t.Fatalf("packages: %v", err)
	}
	for _, key := range []string{"git", "gh", "curl", "go", "node", "bun", "postgres", "mailpit", "caddy"} {
		if _, ok := pkgs[key]; !ok {
			t.Fatalf("missing package %q", key)
		}
	}
	if len(pkgs) != 9 {
		t.Fatalf("want 9 packages, got %d", len(pkgs))
	}
	pg := pkgs["postgres"]
	if pg.Brew != "postgresql@18" || pg.Apt != "postgresql-18" || pg.AptRepo != "pgdg" || pg.Detect != "" {
		t.Fatalf("postgres: %+v", pg)
	}
	if bun := pkgs["bun"]; bun.Script != "https://bun.sh/install" || bun.Detect != "bun" {
		t.Fatalf("bun: %+v", bun)
	}
	if gh := pkgs["gh"]; gh.AptRepo != "github-cli" {
		t.Fatalf("gh: %+v", gh)
	}
}
