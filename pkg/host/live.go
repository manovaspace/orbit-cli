package host

import (
	"context"
	"os"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/runner"
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

// ParseDsclUserShell extracts the shell from `dscl . -read /Users/<name> UserShell`
// output, which looks like "UserShell: /bin/zsh".
func ParseDsclUserShell(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "UserShell:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// darwinLoginShell asks Directory Services for the user's login shell and falls
// back to $SHELL. macOS keeps user accounts out of /etc/passwd.
func darwinLoginShell(username, envShell string) string {
	if username != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		res, err := runner.Exec{}.Run(ctx, "", "dscl", ".", "-read", "/Users/"+username, "UserShell")
		if err == nil {
			if sh := ParseDsclUserShell(res.Stdout); sh != "" {
				return sh
			}
		}
	}
	return envShell
}

func Collect() Input {
	_, wslRunErr := os.Stat("/run/WSL")
	wslRunExists := wslRunErr == nil

	procVersion := ""
	if b, err := os.ReadFile("/proc/version"); err == nil {
		procVersion = string(b)
	}

	username := ""
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	loginShell := ""
	if runtime.GOOS == "darwin" {
		loginShell = darwinLoginShell(username, os.Getenv("SHELL"))
	} else if username != "" {
		if b, err := os.ReadFile("/etc/passwd"); err == nil {
			loginShell = LoginShellFromPasswd(string(b), username)
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
