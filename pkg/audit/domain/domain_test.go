package domain

import (
	"reflect"
	"testing"
)

func TestParseVersionsEnv(t *testing.T) {
	pins := ParseVersionsEnv("# comment\nBUN_VERSION=1.4.0\nNODE_VERSION=\"24.18.0\"\nGO_VERSION=1.26.7\nNEXT_VERSION=16.4.3\nREACT_VERSION=19.3.0\n")
	want := Pins{Go: "1.26.7", Bun: "1.4.0", Node: "24.18.0", Next: "16.4.3", React: "19.3.0"}
	if pins != want {
		t.Errorf("pins = %+v, want %+v", pins, want)
	}
}

func TestParseGoModReadsDirectiveAndToolchain(t *testing.T) {
	module := ParseGoMod("module fixture\n\ngo 1.26.0\n\ntoolchain go1.26.7\n")
	if module.Directive != "1.26.0" || module.Toolchain != "1.26.7" {
		t.Errorf("module = %+v", module)
	}
}

func TestParsePackageJSONFlattensSections(t *testing.T) {
	pkg, err := ParsePackageJSON([]byte(`{
		"packageManager": "bun@1.4.0+abc",
		"engines": {"node": ">=24"},
		"dependencies": {"next": "^16.4.3"},
		"devDependencies": {"@types/node": "*"},
		"workspaces": {"catalog": {"tailwindcss": "^4.3.3"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if BunPin(pkg.PackageManager) != "1.4.0" {
		t.Errorf("bun pin = %q", BunPin(pkg.PackageManager))
	}
	if spec, ok := pkg.Spec("next"); !ok || spec != "^16.4.3" {
		t.Errorf("next spec = %q, %v", spec, ok)
	}
	if spec, ok := pkg.Spec("@types/node"); !ok || spec != "*" {
		t.Errorf("dev dependency missing: %q, %v", spec, ok)
	}
	if spec, ok := pkg.Spec("tailwindcss"); !ok || spec != "^4.3.3" {
		t.Errorf("catalog entry missing: %q, %v", spec, ok)
	}
}

func TestParseBunLockReadsNestedResolutions(t *testing.T) {
	lock := `{
  "packages": {
    "@manovaspace/ui": ["@manovaspace/ui@0.11.1", "", {}],
    "@manovaspace/versioning/@manovaspace/ui": ["@manovaspace/ui@0.8.0", "", {}],
    "react": ["react@19.2.8", "", {}],
  }
}`
	resolved := ParseBunLock(lock, "@manovaspace/ui")
	if len(resolved) != 2 {
		t.Fatalf("resolved = %+v", resolved)
	}
	if resolved[0].Version != "0.11.1" || resolved[1].Version != "0.8.0" {
		t.Errorf("versions = %+v", resolved)
	}
	if got := ParseBunLock(lock, "react"); len(got) != 1 || got[0].Version != "19.2.8" {
		t.Errorf("plain package = %+v", got)
	}
}

func TestParseImportsSeparatesNamedAndNamespaces(t *testing.T) {
	source := `import {
  Badge,
  Sheet as SheetRoot,
  type BadgeProps,
} from "@manovaspace/ui";
import * as Kit from "@manovaspace/tokens";
import { cn } from "./utils";`
	named, namespaces := ParseImports(source)
	wanted := []Import{
		{Path: "@manovaspace/ui", Symbol: "Badge"},
		{Path: "@manovaspace/ui", Symbol: "Sheet"},
		{Path: "@manovaspace/ui", Symbol: "BadgeProps"},
		{Path: "./utils", Symbol: "cn"},
	}
	if !reflect.DeepEqual(named, wanted) {
		t.Errorf("named = %+v, want %+v", named, wanted)
	}
	if len(namespaces) != 1 || namespaces[0].Path != "@manovaspace/tokens" || namespaces[0].Local != "Kit" {
		t.Errorf("namespaces = %+v", namespaces)
	}
}

func TestParseExports(t *testing.T) {
	kit := ParseKitExports("export { Badge, badgeVariants } from './badge.js';\nexport { GraduationCap as AcademicCapSolidIcon } from 'lucide-react';")
	for _, want := range []string{"Badge", "badgeVariants", "AcademicCapSolidIcon"} {
		if !contains(kit, want) {
			t.Errorf("kit export %q missing from %v", want, kit)
		}
	}
	local := ParseLocalExports("export function Badge() {}\nexport default function Sheet() {}\nexport const items = [];")
	for _, want := range []string{"Badge", "Sheet", "items"} {
		if !contains(local, want) {
			t.Errorf("local export %q missing from %v", want, local)
		}
	}
}

func TestExactPinAndForbiddenSpec(t *testing.T) {
	for _, spec := range []string{"16.4.3", "v16.4.3", "1.26"} {
		if !ExactPin(spec) {
			t.Errorf("%q should be an exact pin", spec)
		}
	}
	for _, spec := range []string{"^16.4.3", "~1.2.3", ">=24", "latest", "*", ""} {
		if ExactPin(spec) {
			t.Errorf("%q should not be an exact pin", spec)
		}
	}
	for _, spec := range []string{"file:../ui", "*", "latest", "", "link:../x"} {
		if !ForbiddenSpec(spec) {
			t.Errorf("%q should be forbidden", spec)
		}
	}
	if ForbiddenSpec("^1.2.3") {
		t.Error("a caret range is not forbidden")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.26.0", "1.26", 0},
		{"1.26.0", "1.26.7", -1},
		{"1.26.7", "1.26", 1},
		{"v19.3.0", "19.3.0", 0},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParseImageRefs(t *testing.T) {
	from := ParseDockerfileImages("FROM --platform=linux/amd64 oven/bun:1.4.0 AS build\nFROM alpine:3.20\n")
	if len(from) != 2 || from[0].Repository != "oven/bun" || from[0].Tag != "1.4.0" {
		t.Errorf("dockerfile refs = %+v", from)
	}
	compose := ParseComposeImages("services:\n  db:\n    image: \"postgres:16\"\n  app:\n    image: node:24.18.0\n")
	if len(compose) != 2 || compose[1].Repository != "node" || compose[1].Tag != "24.18.0" {
		t.Errorf("compose refs = %+v", compose)
	}
	if tag, ok := ComparableTag("24-alpine"); ok {
		t.Errorf("floating tag compared: %q", tag)
	}
	if tag, ok := ComparableTag("1.26.7"); !ok || tag != "1.26.7" {
		t.Errorf("pinned tag = %q, %v", tag, ok)
	}
}

func TestSkipPath(t *testing.T) {
	for _, path := range []string{"orbit/a/node_modules/b", "clients/x/.next/y", ".worktrees/z", "orbit/foo-wt-bar"} {
		if !SkipPath(path) {
			t.Errorf("%q should be skipped", path)
		}
	}
	if SkipPath("orbit/orbit-cli") {
		t.Error("a normal repository path was skipped")
	}
}

func TestApplyExceptions(t *testing.T) {
	findings := []Finding{
		{ID: "versions.react", Severity: SeverityError, Subject: "react", Actual: "^19.3.0", Expected: "19.3.0", Fix: "pin react"},
		{ID: "versions.go", Severity: SeverityError, Subject: "go", Actual: "1.26.0", Expected: "1.26.7"},
	}
	exceptions := []Exception{{Subject: "react", Allowed: "^19.3.0", Reason: "deliberate caret"}}
	out := ApplyExceptions(findings, exceptions)
	if out[0].Severity != SeverityInfo {
		t.Errorf("excepted finding = %+v, want INFO", out[0])
	}
	if out[1].Severity != SeverityError {
		t.Errorf("unmatched finding = %+v, want ERROR", out[1])
	}
}

func TestReportExitCode(t *testing.T) {
	report := NewReport()
	if report.ExitCode() != 0 {
		t.Errorf("clean exit = %d", report.ExitCode())
	}
	report.Add(Finding{ID: "clone.behind", Severity: SeverityWarn})
	if report.ExitCode() != 0 {
		t.Errorf("warning exit = %d", report.ExitCode())
	}
	report.AddUnverified("orbit/ghost: repository not cloned")
	if report.ExitCode() != 2 {
		t.Errorf("unverified exit = %d", report.ExitCode())
	}
	report.Add(Finding{ID: "versions.go", Severity: SeverityError})
	if report.ExitCode() != 1 {
		t.Errorf("error exit = %d", report.ExitCode())
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
