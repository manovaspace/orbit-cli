package domain

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
	"golang.org/x/crypto/ssh"
)

// TunnelLocalBind is the loopback address every tunnel forward listens on.
const TunnelLocalBind = "127.0.0.1"

// tunnelRuns is the registry runs value that the developer tunnel forwards.
const tunnelRuns = "shared"

// tunnelHostAlias is the Host name `orbit dev tunnel --ssh-config` writes.
const tunnelHostAlias = "orbit-dev"

// tunnelCommandNote is the forced command the server-side authorized_keys line
// runs for the tunnel key. It is informational; the key only gets forwarding.
const tunnelCommandNote = "echo orbit dev tunnel only"

// Forward is one registry entry the developer tunnel exposes on loopback.
// Bind is the address on the server that the forward reaches.
type Forward struct {
	Name string `json:"name"`
	Port int    `json:"port"`
	Bind string `json:"bind"`
}

// LocalSpec is the -L argument: 127.0.0.1:<port>:<bind>:<port>.
func (f Forward) LocalSpec() string {
	return fmt.Sprintf("%s:%d:%s:%d", TunnelLocalBind, f.Port, f.Bind, f.Port)
}

// SharedForwards lists every registry service and exception with runs shared,
// sorted by port. A service without a bind listens on loopback.
func SharedForwards(reg *portsdomain.Registry) []Forward {
	var out []Forward
	for _, s := range reg.Services {
		if s.Runs != tunnelRuns {
			continue
		}
		bind := s.Bind
		if bind == "" {
			bind = TunnelLocalBind
		}
		out = append(out, Forward{Name: s.Name, Port: s.Port, Bind: bind})
	}
	for _, e := range reg.Exceptions {
		if e.Runs != tunnelRuns {
			continue
		}
		out = append(out, Forward{Name: e.Name, Port: e.Port, Bind: TunnelLocalBind})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// SelectForwards returns the shared forwards, restricted to the names in only
// when it is not empty. A name that is not a shared entry is an error that
// lists the valid names. A registry with no shared entries is an error.
func SelectForwards(reg *portsdomain.Registry, only []string) ([]Forward, error) {
	all := SharedForwards(reg)
	if len(all) == 0 {
		return nil, errors.New("the port registry lists no service with runs: shared")
	}
	if len(only) == 0 {
		return all, nil
	}

	valid := make(map[string]bool, len(all))
	names := make([]string, 0, len(all))
	for _, f := range all {
		if !valid[f.Name] {
			names = append(names, f.Name)
		}
		valid[f.Name] = true
	}
	sort.Strings(names)

	want := make(map[string]bool, len(only))
	var unknown []string
	for _, name := range only {
		name = strings.TrimSpace(name)
		if name == "" || want[name] {
			continue
		}
		want[name] = true
		if !valid[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown tunnel service %s; valid names: %s",
			strings.Join(unknown, ", "), strings.Join(names, ", "))
	}

	var out []Forward
	for _, f := range all {
		if want[f.Name] {
			out = append(out, f)
		}
	}
	return out, nil
}

// ParseTunnelHost splits user@host into its user (empty when absent) and host.
func ParseTunnelHost(raw string) (user, host string, err error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", errors.New("tunnel host is empty")
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", "", fmt.Errorf("tunnel host %q must not contain spaces", raw)
	}
	if i := strings.LastIndex(value, "@"); i >= 0 {
		user, host = value[:i], value[i+1:]
		if user == "" {
			return "", "", fmt.Errorf("tunnel host %q has an empty user before @", raw)
		}
	} else {
		host = value
	}
	if host == "" {
		return "", "", fmt.Errorf("tunnel host %q has no host name", raw)
	}
	return user, host, nil
}

// ExpandHome replaces a leading ~ (alone or followed by /) with home. Other
// paths are returned unchanged. An empty home leaves the path unchanged.
func ExpandHome(path, home string) string {
	if home == "" || (path != "~" && !strings.HasPrefix(path, "~/")) {
		return path
	}
	return home + path[1:]
}

// SSHArgs is the ssh command that forwards every entry in forwards. A non-empty
// identity adds -i and IdentitiesOnly. Ctrl-C ends it when it runs in the
// foreground.
func SSHArgs(host string, port int, identity string, forwards []Forward) []string {
	args := []string{
		"ssh",
		"-N",
		"-p", strconv.Itoa(port),
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
	}
	if identity != "" {
		args = append(args, "-i", identity, "-o", "IdentitiesOnly=yes")
	}
	for _, f := range forwards {
		args = append(args, "-L", f.LocalSpec())
	}
	return append(args, host)
}

// SSHConfigBlock is a Host block for ~/.ssh/config that carries the same
// forwards. host is user@host (or a bare host). A non-empty identity adds
// IdentityFile and IdentitiesOnly after Port.
func SSHConfigBlock(host string, port int, identity string, forwards []Forward) (string, error) {
	user, hostName, err := ParseTunnelHost(host)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Host %s\n", tunnelHostAlias)
	fmt.Fprintf(&b, "    HostName %s\n", hostName)
	if user != "" {
		fmt.Fprintf(&b, "    User %s\n", user)
	}
	fmt.Fprintf(&b, "    Port %d\n", port)
	if identity != "" {
		fmt.Fprintf(&b, "    IdentityFile %s\n", identity)
		b.WriteString("    IdentitiesOnly yes\n")
	}
	b.WriteString("    ExitOnForwardFailure yes\n")
	b.WriteString("    ServerAliveInterval 30\n")
	for _, f := range forwards {
		fmt.Fprintf(&b, "    LocalForward %s:%d %s:%d\n", TunnelLocalBind, f.Port, f.Bind, f.Port)
	}
	return b.String(), nil
}

// AuthorizedKeyLine is the server-side authorized_keys entry for keyFile
// content. It must hold exactly one ssh public key line without options. The
// entry allows port forwarding only to the forwards' targets.
func AuthorizedKeyLine(forwards []Forward, keyFile string) (string, error) {
	var lines []string
	for _, line := range strings.Split(keyFile, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) != 1 {
		return "", fmt.Errorf("the key file must hold exactly one ssh public key line, found %d", len(lines))
	}
	_, _, options, _, err := ssh.ParseAuthorizedKey([]byte(lines[0]))
	if err != nil {
		return "", fmt.Errorf("not an ssh public key: %w", err)
	}
	if len(options) > 0 {
		return "", errors.New("the key line must not carry authorized_keys options")
	}

	opts := []string{"restrict", "port-forwarding"}
	for _, f := range forwards {
		opts = append(opts, fmt.Sprintf("permitopen=%q", fmt.Sprintf("%s:%d", f.Bind, f.Port)))
	}
	opts = append(opts, fmt.Sprintf("command=%q", tunnelCommandNote))
	return strings.Join(opts, ",") + " " + lines[0], nil
}
