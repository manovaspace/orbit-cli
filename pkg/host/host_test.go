package host

import (
	"strings"
	"testing"
)

func okInput() Input {
	return Input{
		GOOS:        "linux",
		GOARCH:      "amd64",
		OSRelease:   map[string]string{"ID": "ubuntu", "VERSION_ID": "24.04"},
		Virt:        "native",
		LoginShell:  "/usr/bin/zsh",
		Home:        "/home/dev",
		Path:        "/usr/bin:/home/dev/.local/bin",
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
		{"darwin", func(i *Input) { i.GOOS = "darwin" }, "os"},
		{"windows", func(i *Input) { i.GOOS = "windows" }, "os"},
		{"debian", func(i *Input) { i.OSRelease["ID"] = "debian" }, "os"},
		{"ubuntu2204", func(i *Input) { i.OSRelease["VERSION_ID"] = "22.04" }, "os"},
		{"empty_osrelease", func(i *Input) { i.OSRelease = map[string]string{} }, "os"},
		{"arm64", func(i *Input) { i.GOARCH = "arm64" }, "arch"},
		{"wsl1", func(i *Input) { i.Virt = "wsl1" }, "virt"},
		{"bash", func(i *Input) { i.LoginShell = "/bin/bash" }, "shell"},
		{"nopath", func(i *Input) { i.Path = "/usr/bin" }, "path"},
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

func TestFormat_ListsOnlyFailures(t *testing.T) {
	in := okInput()
	in.GOOS = "darwin"
	in.Path = "/usr/bin"
	msg := Format(Evaluate(in))
	if msg == "" || !strings.Contains(msg, "Orbit requires") || !strings.Contains(msg, "orbit doctor") {
		t.Fatalf("unexpected message: %q", msg)
	}
}
