package host

import (
	"os"
	"os/user"
	"runtime"
	"strings"
)

func ParseVirt(procVersion string, wslRunExists bool) string {
	v := strings.ToLower(procVersion)
	if !strings.Contains(v, "microsoft") {
		return "native"
	}
	if wslRunExists || strings.Contains(v, "wsl2") {
		return "wsl2"
	}
	return "wsl1"
}

func LoginShellFromPasswd(passwd, username string) string {
	for _, line := range strings.Split(passwd, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 2 {
			continue
		}
		if fields[0] == username {
			return fields[len(fields)-1]
		}
	}
	return ""
}

func parseOSRelease(content string) map[string]string {
	data := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"'")
		data[key] = val
	}
	return data
}

func readOSRelease() map[string]string {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		content, err := os.ReadFile(path)
		if err == nil {
			return parseOSRelease(string(content))
		}
	}
	return map[string]string{}
}

func Collect() Input {
	_, wslRunErr := os.Stat("/run/WSL")
	wslRunExists := wslRunErr == nil

	procVersion := ""
	if b, err := os.ReadFile("/proc/version"); err == nil {
		procVersion = string(b)
	}

	loginShell := ""
	if u, err := user.Current(); err == nil {
		if b, err := os.ReadFile("/etc/passwd"); err == nil {
			loginShell = LoginShellFromPasswd(string(b), u.Username)
		}
	}

	home, _ := os.UserHomeDir()

	return Input{
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		OSRelease:  readOSRelease(),
		Virt:       ParseVirt(procVersion, wslRunExists),
		LoginShell: loginShell,
		Home:       home,
		Path:       os.Getenv("PATH"),
	}
}

func Live() Report {
	return Evaluate(Collect())
}
