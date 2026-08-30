package host

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Input struct {
	GOOS       string
	GOARCH     string
	OSRelease  map[string]string
	Virt       string // native | wsl2 | wsl1
	LoginShell string
	Home       string
	Path       string
}

type Failure struct {
	Code    string
	Message string
}

type Report struct {
	OK       bool
	Failures []Failure
}

func Evaluate(in Input) Report {
	var fs []Failure
	if in.GOOS != "linux" {
		fs = append(fs, Failure{Code: "os", Message: fmt.Sprintf("%s is not supported", in.GOOS)})
	} else {
		id := strings.ToLower(strings.TrimSpace(in.OSRelease["ID"]))
		ver := strings.TrimSpace(in.OSRelease["VERSION_ID"])
		if id != "ubuntu" || !(strings.HasPrefix(ver, "24.04") || strings.HasPrefix(ver, "26.04")) {
			label := ver
			if id != "" && id != "ubuntu" {
				label = id + " " + ver
			}
			if strings.TrimSpace(label) == "" {
				label = "unknown Linux"
			}
			fs = append(fs, Failure{Code: "os", Message: fmt.Sprintf("%s is not supported", strings.TrimSpace(label))})
		}
	}
	if in.GOARCH != "amd64" {
		fs = append(fs, Failure{Code: "arch", Message: fmt.Sprintf("%s is not supported (amd64 required)", in.GOARCH)})
	}
	if in.Virt == "wsl1" {
		fs = append(fs, Failure{Code: "virt", Message: "WSL1 is not supported (WSL2 required)"})
	}
	shell := filepath.Base(strings.TrimSpace(in.LoginShell))
	if shell != "zsh" {
		fs = append(fs, Failure{Code: "shell", Message: "login shell must be zsh"})
	}
	want := filepath.Clean(filepath.Join(in.Home, ".local", "bin"))
	if !pathHasDir(in.Path, want) {
		fs = append(fs, Failure{Code: "path", Message: "~/.local/bin is not on PATH (add export PATH=\"$HOME/.local/bin:$PATH\" to ~/.zshrc)"})
	}
	return Report{OK: len(fs) == 0, Failures: fs}
}

func pathHasDir(pathEnv, dir string) bool {
	dir = filepath.Clean(dir)
	for _, p := range strings.Split(pathEnv, ":") {
		if p == "" {
			continue
		}
		if filepath.Clean(p) == dir {
			return true
		}
	}
	return false
}

func Format(r Report) string {
	if r.OK {
		return ""
	}
	var b strings.Builder
	b.WriteString("Orbit requires Ubuntu 24.04 or 26.04 LTS (amd64), a zsh login shell, and ~/.local/bin on PATH.\n")
	for _, f := range r.Failures {
		fmt.Fprintf(&b, "  - %s: %s\n", f.Code, f.Message)
	}
	b.WriteString("Run: orbit doctor\n")
	return b.String()
}
