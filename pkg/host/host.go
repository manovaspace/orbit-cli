package host

import (
	"fmt"
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
	Platform string // "linux" or "darwin" (the GOOS the report was evaluated for)
	Failures []Failure
}

func Evaluate(in Input) Report {
	var fs []Failure
	switch in.GOOS {
	case "linux":
		id := strings.ToLower(strings.TrimSpace(in.OSRelease["ID"]))
		ver := strings.TrimSpace(in.OSRelease["VERSION_ID"])
		if id != "ubuntu" || (!strings.HasPrefix(ver, "24.04") && !strings.HasPrefix(ver, "26.04")) {
			label := ver
			if id != "" && id != "ubuntu" {
				label = id + " " + ver
			}
			if strings.TrimSpace(label) == "" {
				label = "unknown Linux"
			}
			fs = append(fs, Failure{Code: "os", Message: fmt.Sprintf("%s is not supported", strings.TrimSpace(label))})
		}
		if in.GOARCH != "amd64" {
			fs = append(fs, Failure{Code: "arch", Message: fmt.Sprintf("%s is not supported (amd64 required)", in.GOARCH)})
		}
	case "darwin":
		// macOS has no /etc/os-release; any Darwin release with arm64 or amd64 is accepted.
		if in.GOARCH != "arm64" && in.GOARCH != "amd64" {
			fs = append(fs, Failure{Code: "arch", Message: fmt.Sprintf("%s is not supported (arm64 or amd64 required)", in.GOARCH)})
		}
	default:
		fs = append(fs, Failure{Code: "os", Message: fmt.Sprintf("%s is not supported", in.GOOS)})
	}
	if in.Virt == "wsl1" {
		fs = append(fs, Failure{Code: "virt", Message: "WSL1 is not supported (WSL2 required)"})
	}
	return Report{OK: len(fs) == 0, Platform: in.GOOS, Failures: fs}
}

func Format(r Report) string {
	if r.OK {
		return ""
	}
	var b strings.Builder
	b.WriteString("Orbit supports Ubuntu 24.04 or 26.04 LTS (amd64) and macOS (arm64 or amd64), .\n")
	for _, f := range r.Failures {
		fmt.Fprintf(&b, "  - %s: %s\n", f.Code, f.Message)
	}
	b.WriteString("Run: orbit doctor\n")
	return b.String()
}
