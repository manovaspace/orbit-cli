package host

import (
	"strings"
	"testing"
)

func okInput() Input {
	return Input{
		GOOS:       "linux",
		GOARCH:     "amd64",
		OSRelease:  map[string]string{"ID": "ubuntu", "VERSION_ID": "24.04"},
		Virt:       "native",
		LoginShell: "/usr/bin/zsh",
		Home:       "/home/dev",
		Path:       "/usr/bin:/home/dev/.local/bin",
	}
}

func TestEvaluate_OK(t *testing.T) {
	for _, in := range []Input{
		okInput(),
		func() Input { i := okInput(); i.OSRelease["VERSION_ID"] = "26.04"; return i }(),
		func() Input { i := okInput(); i.OSRelease["VERSION_ID"] = "24.04.1"; return i }(),
		func() Input { i := okInput(); i.Virt = "wsl2"; return i }(),
		func() Input { i := okInput(); i.LoginShell = "/bin/zsh"; return i }(),
	} {
		r := Evaluate(in)
		if !r.OK {
			t.Fatalf("expected OK, got failures %#v for %+v", r.Failures, in)
		}
	}
}

func TestEvaluate_Failures(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Input)
		code string
	}{
		{"darwin_386", func(i *Input) { i.GOOS = "darwin"; i.GOARCH = "386" }, "arch"},
		{"linux_arm64_still_fails", func(i *Input) { i.GOARCH = "arm64" }, "arch"},
		{"windows", func(i *Input) { i.GOOS = "windows" }, "os"},
		{"debian", func(i *Input) { i.OSRelease["ID"] = "debian" }, "os"},
		{"ubuntu2204", func(i *Input) { i.OSRelease["VERSION_ID"] = "22.04" }, "os"},
		{"empty_osrelease", func(i *Input) { i.OSRelease = map[string]string{} }, "os"},
		{"arm64", func(i *Input) { i.GOARCH = "arm64" }, "arch"},
		{"wsl1", func(i *Input) { i.Virt = "wsl1" }, "virt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := okInput()
			tc.mut(&in)
			r := Evaluate(in)
			if r.OK {
				t.Fatal("expected failure")
			}
			found := false
			for _, f := range r.Failures {
				if f.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected code %s, got %#v", tc.code, r.Failures)
			}
		})
	}
}

func darwinInput() Input {
	return Input{
		GOOS:       "darwin",
		GOARCH:     "arm64",
		OSRelease:  map[string]string{},
		Virt:       "native",
		LoginShell: "/bin/zsh",
		Home:       "/Users/dev",
		Path:       "/usr/bin:/Users/dev/.local/bin",
	}
}

func TestEvaluate_DarwinOK(t *testing.T) {
	for _, arch := range []string{"arm64", "amd64"} {
		in := darwinInput()
		in.GOARCH = arch
		r := Evaluate(in)
		if !r.OK || r.Platform != "darwin" {
			t.Fatalf("darwin/%s: expected OK on darwin, got %#v", arch, r)
		}
	}
}

// TestEvaluate_DarwinBashAndNoLocalBinPass: the login shell and ~/.local/bin PATH are no longer host failures.
func TestEvaluate_DarwinBashAndNoLocalBinPass(t *testing.T) {
	in := darwinInput()
	in.LoginShell = "/bin/bash"
	in.Path = "/usr/bin"
	if r := Evaluate(in); !r.OK || len(r.Failures) != 0 {
		t.Fatalf("expected OK on darwin with bash and no ~/.local/bin, got %#v", r.Failures)
	}
}

func TestEvaluate_LinuxNonUbuntuFails(t *testing.T) {
	in := okInput()
	in.OSRelease = map[string]string{"ID": "debian", "VERSION_ID": "12"}
	r := Evaluate(in)
	if r.OK || r.Platform != "linux" || r.Failures[0].Code != "os" {
		t.Fatalf("linux debian must fail os, got %#v", r)
	}
}

func TestEvaluate_PlatformSet(t *testing.T) {
	if r := Evaluate(okInput()); r.Platform != "linux" {
		t.Fatalf("linux platform: %q", r.Platform)
	}
	if r := Evaluate(darwinInput()); r.Platform != "darwin" {
		t.Fatalf("darwin platform: %q", r.Platform)
	}
}

func TestFormat_ListsOnlyFailures(t *testing.T) {
	in := okInput()
	in.GOOS = "darwin"
	in.GOARCH = "386"
	msg := Format(Evaluate(in))
	if msg == "" || !strings.Contains(msg, "Orbit supports Ubuntu 24.04 or 26.04 LTS (amd64) and macOS (arm64 or amd64)") || !strings.Contains(msg, "orbit doctor") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if strings.Contains(msg, "shell") || strings.Contains(msg, "~/.local/bin") || !strings.Contains(msg, "  - arch:") {
		t.Fatalf("message lists retired checks or misses the arch failure: %q", msg)
	}
}
