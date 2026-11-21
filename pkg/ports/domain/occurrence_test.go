package domain

import "testing"

type want struct {
	line int
	port int
	role Role
	open bool
}

func checkOccurrences(t *testing.T, rel, content string, wants []want) {
	t.Helper()
	got := Extract(rel, content)
	if len(got) != len(wants) {
		t.Fatalf("Extract(%s) = %+v, want %d occurrences %+v", rel, got, len(wants), wants)
	}
	for i, w := range wants {
		g := got[i]
		if g.Line != w.line || g.Port != w.port || g.Role != w.role || g.Open != w.open {
			t.Errorf("occurrence %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestComposeMappings(t *testing.T) {
	content := "services:\n" +
		"  a:\n" +
		"    ports:\n" +
		"      - \"10000:5432\"\n" +
		"      - \"127.0.0.1:10004:3000\"\n" +
		"      - \"0.0.0.0:10025:8025\"\n" +
		"      - \"${PORT_VAR:-10099}:1025\"\n" +
		"      - \"${PORT_VAR}:1025\"\n" +
		"      - \"./data:/var/lib/data\"\n" +
		"      - \"5432\"\n"
	checkOccurrences(t, "orbit/orbit-infra/docker-compose.yml", content, []want{
		{4, 10000, RoleBind, true},
		{5, 10004, RoleBind, false},
		{6, 10025, RoleBind, true},
		{7, 10099, RoleBind, true},
	})
}

func TestComposeIgnoresOutOfWindowAndNonNumericHosts(t *testing.T) {
	content := "  - \"9999:80\"\n  - \"18000:80\"\n  - \"redis:6379\"\n"
	checkOccurrences(t, "orbit/x/compose.yml", content, nil)
}

func TestPackageScripts(t *testing.T) {
	content := `{
  "scripts": {
    "dev": "next dev --port 10051",
    "start": "PORT=10010 node dist/server.js",
    "lint": "eslint -p 10022 ."
  }
}`
	checkOccurrences(t, "clients/manova/waypoint-api/package.json", content, []want{
		{3, 10051, RoleBind, false},
		{4, 10010, RoleBind, false},
		{5, 10022, RoleBind, false},
	})
}

func TestEnvExample(t *testing.T) {
	content := "WAYPOINT_API_PORT=10051\nexport AUTH_HEALTH_PORT=\"10011\"\nDB_HOST=127.0.0.1:10000\nAPI_URL=http://localhost:10101\nNAME=10020\n"
	checkOccurrences(t, "clients/manova/waypoint-api/.env.example", content, []want{
		{1, 10051, RoleBind, false},
		{2, 10011, RoleUse, false},
		{3, 10000, RoleUse, false},
		{4, 10101, RoleUse, false},
	})
}

func TestGoLiteralsAndNamedDefaults(t *testing.T) {
	content := "package x\n" +
		"const defaultHealthPort = 10055\n" +
		"var addr = \":10050\"\n" +
		"var u = \"http://localhost:10101\"\n" +
		"v := envOr(\"PORT\", \"10096\")\n" +
		"const raw = `127.0.0.1:10110`\n"
	checkOccurrences(t, "clients/manova/waypoint-api/src/server.go", content, []want{
		{2, 10055, RoleBind, false},
		{3, 10050, RoleBind, false},
		{4, 10101, RoleUse, false},
		{5, 10096, RoleBind, false},
		{6, 10110, RoleUse, false},
	})
}

func TestGoTestFilesAreNotScanned(t *testing.T) {
	if Scannable("orbit/x/server_test.go") {
		t.Fatal("_test.go must not be scannable")
	}
	checkOccurrences(t, "orbit/x/server_test.go", "const a = \":10050\"\n", nil)
}

func TestCaddyAndScripts(t *testing.T) {
	checkOccurrences(t, "clients/manova/manova-infra/Caddyfile",
		"reverse_proxy host.docker.internal:10053\nreverse_proxy localhost:10026\n",
		[]want{{1, 10053, RoleUse, false}, {2, 10026, RoleUse, false}})
	checkOccurrences(t, "manovaspace/design-system/tools/sb.mjs", "const u = \"http://localhost:10027\";\n",
		[]want{{1, 10027, RoleUse, false}})
	checkOccurrences(t, "clients/manova/falak/src/app.ts", "fetch(\"http://localhost:10110\")\n",
		[]want{{1, 10110, RoleUse, false}})
}

func TestScannableSelection(t *testing.T) {
	yes := []string{
		"orbit/x/docker-compose.yml", "clients/a/b/compose.prod.yaml", "clients/a/b/package.json",
		"clients/a/b/.env.example", "clients/a/b/.env.local.example", "orbit/x/main.go",
		"clients/a/b/Caddyfile", "clients/a/b/site.caddy", "manovaspace/x/tools/a.mjs",
		"clients/a/b/src/app.ts",
	}
	no := []string{
		"orbit/x/main_test.go", "clients/a/b/.env", "clients/a/b/.env.local", "clients/a/b/lib/app.ts",
		"orbit/x/readme.md", "clients/a/b/compose.json",
	}
	for _, rel := range yes {
		if !Scannable(rel) {
			t.Errorf("Scannable(%s) = false, want true", rel)
		}
	}
	for _, rel := range no {
		if Scannable(rel) {
			t.Errorf("Scannable(%s) = true, want false", rel)
		}
	}
}

func TestPortKeyRole(t *testing.T) {
	tests := []struct {
		key  string
		want Role
	}{
		{"SMTP_PORT", RoleUse},
		{"smtp_port", RoleUse},
		{"PORT", RoleBind},
		{"HTTP_PORT", RoleBind},
		{"REDIS_PORT", RoleUse},
		{"DB_PORT", RoleUse},
		{"OTEL_PORT", RoleUse},
		{"AUTH_HEALTH_PORT", RoleUse},
		{"GRPC_PORT", RoleBind},
		{"WAYPOINT_API_PORT", RoleBind},
	}
	for _, tt := range tests {
		if got := portKeyRole(tt.key); got != tt.want {
			t.Errorf("portKeyRole(%s) = %s, want %s", tt.key, got, tt.want)
		}
	}
}

func TestEnvKeyRoleInExtract(t *testing.T) {
	content := "SMTP_PORT=10024\nPORT=10101\nHTTP_PORT=10011\nREDIS_PORT=10334\n"
	checkOccurrences(t, "orbit/orbit-notifications/.env.example", content, []want{
		{1, 10024, RoleUse, false},
		{2, 10101, RoleBind, false},
		{3, 10011, RoleBind, false},
		{4, 10334, RoleUse, false},
	})
}

func TestGoEnvDefaultUsesKeyRole(t *testing.T) {
	content := "port: envOr(\"SMTP_PORT\", \"10024\"),\n" +
		"x := envOr(\"HTTP_PORT\", \"10011\")\n"
	checkOccurrences(t, "orbit/orbit-notifications/internal/infrastructure/smtp/client.go", content, []want{
		{1, 10024, RoleUse, false},
		{2, 10011, RoleBind, false},
	})
}
