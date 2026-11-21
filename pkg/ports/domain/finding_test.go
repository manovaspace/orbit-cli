package domain

import "testing"

func fixtureRegistry() *Registry {
	return &Registry{
		Services: []Service{
			{Port: 10010, Project: "orbit-platform", Name: "orbit-auth-grpc", Repos: []string{"orbit/orbit-auth"}},
		},
		Reserved:   []Reserved{{Port: 10007, Project: "orbit-platform", Name: "caddy-ingress"}},
		Exceptions: []Exception{{Port: 10334, Name: "redis", Repos: []string{"orbit/orbit-infra"}}},
		Retired:    []Retired{{Port: 10026, Was: "unleash", Retired: "2026-11-21"}},
	}
}

func kindsOf(findings []Finding) []Kind {
	var out []Kind
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
}

func TestClassifyEachKind(t *testing.T) {
	reg := fixtureRegistry()
	tests := []struct {
		name string
		occ  Occurrence
		repo string
		want []Kind
	}{
		{"registered bind in listed repo", Occurrence{Port: 10010, Role: RoleBind}, "orbit/orbit-auth", nil},
		{"registered bind in other repo", Occurrence{Port: 10010, Role: RoleBind}, "clients/x/y", []Kind{KindWrongRepo}},
		{"registered use anywhere", Occurrence{Port: 10010, Role: RoleUse}, "clients/x/y", nil},
		{"unknown number", Occurrence{Port: 10999, Role: RoleUse}, "clients/x/y", []Kind{KindUnregistered}},
		{"retired number", Occurrence{Port: 10026, Role: RoleUse}, "clients/x/y", []Kind{KindRetired}},
		{"reserved bind", Occurrence{Port: 10007, Role: RoleBind}, "orbit/orbit-infra", []Kind{KindReserved}},
		{"reserved use is not a bind", Occurrence{Port: 10007, Role: RoleUse}, "orbit/orbit-infra", nil},
		{"exception bind in listed repo", Occurrence{Port: 10334, Role: RoleBind}, "orbit/orbit-infra", nil},
		{"open compose publish adds OPEN_BIND", Occurrence{Port: 10010, Role: RoleBind, Open: true}, "orbit/orbit-auth", []Kind{KindOpenBind}},
		{"unknown bind with no repo owner", Occurrence{Port: 10010, Role: RoleBind}, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kindsOf(Classify(reg, tt.occ, tt.repo))
			if len(got) != len(tt.want) {
				t.Fatalf("kinds = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("kinds = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestClassifySeverityAndName(t *testing.T) {
	reg := fixtureRegistry()
	findings := Classify(reg, Occurrence{File: "a.go", Line: 3, Port: 10010, Role: RoleBind, Open: true}, "clients/x/y")
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	wrong := findings[0]
	if wrong.Kind != KindWrongRepo || wrong.Severity != SeverityError || wrong.Name != "orbit-auth-grpc" || wrong.File != "a.go" || wrong.Line != 3 {
		t.Errorf("wrong repo finding = %+v", wrong)
	}
	if findings[1].Kind != KindOpenBind || findings[1].Severity != SeverityWarning {
		t.Errorf("open bind finding = %+v", findings[1])
	}
}
