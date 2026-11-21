package domain

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Scan window for `o port check`: every number in this inclusive range found
// in a scannable file is a port occurrence. Numbers outside it are ignored.
const (
	ScanLow  = 10000
	ScanHigh = 17999
)

// Role says how a file uses a port: BIND means the repository listens on it,
// USE means it only connects to it.
type Role string

const (
	RoleBind Role = "bind"
	RoleUse  Role = "use"
)

// Occurrence is one port number found in one file. Open marks a compose
// publish without a loopback address (no IP, or 0.0.0.0).
type Occurrence struct {
	File string
	Line int
	Port int
	Role Role
	Open bool
}

type fileKind int

const (
	kindNone fileKind = iota
	kindCompose
	kindPackage
	kindEnvExample
	kindGo
	kindCaddy
	kindScript
)

var composeName = regexp.MustCompile(`^(docker-)?compose.*\.ya?ml$`)

// kindOf classifies a workspace-relative slash path. Test files are never scanned.
func kindOf(rel string) fileKind {
	rel = path.Clean(rel)
	base := path.Base(rel)
	switch {
	case composeName.MatchString(base):
		return kindCompose
	case base == "package.json":
		return kindPackage
	case base == ".env.example", strings.HasPrefix(base, ".env.") && strings.HasSuffix(base, ".example"):
		return kindEnvExample
	case strings.HasSuffix(base, "_test.go"):
		return kindNone
	case strings.HasSuffix(base, ".go"):
		return kindGo
	case strings.HasSuffix(base, ".caddy"), base == "Caddyfile", strings.HasPrefix(base, "Caddyfile."):
		return kindCaddy
	case strings.HasSuffix(base, ".mjs"):
		return kindScript
	case strings.HasSuffix(base, ".ts") && strings.Contains("/"+rel, "/src/"):
		return kindScript
	}
	return kindNone
}

// Scannable reports whether `o port check` reads rel.
func Scannable(rel string) bool {
	return kindOf(rel) != kindNone
}

// Extract returns every in-window port occurrence in one scannable file. A
// file that is not scannable yields nothing.
func Extract(rel string, content string) []Occurrence {
	rel = path.Clean(rel)
	lines := strings.Split(content, "\n")
	var hits []Occurrence
	switch kindOf(rel) {
	case kindCompose:
		hits = composeHits(lines)
	case kindPackage:
		hits = packageHits(content, lines)
	case kindEnvExample:
		hits = envHits(lines)
	case kindGo:
		hits = goHits(lines)
	case kindCaddy:
		hits = matchLines(lines, loopbackRef, RoleUse)
	case kindScript:
		hits = matchLines(lines, localhostRef, RoleUse)
	}
	out := make([]Occurrence, 0, len(hits))
	for _, h := range hits {
		if h.Port < ScanLow || h.Port > ScanHigh {
			continue
		}
		h.File = rel
		out = append(out, h)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// composeHits reads "H:C", "IP:H:C" and ${VAR:-H}:C list items. The host
// number is the bind; a mapping with no IP or with 0.0.0.0 is also open.
func composeHits(lines []string) []Occurrence {
	var out []Occurrence
	for i, raw := range lines {
		item, ok := composeItem(raw)
		if !ok {
			continue
		}
		tokens := splitTopLevel(item)
		var ipTok, hostTok string
		hasIP := false
		switch len(tokens) {
		case 2:
			hostTok = tokens[0]
		case 3:
			ipTok, hostTok, hasIP = tokens[0], tokens[1], true
		default:
			continue
		}
		if len(tokens) == 3 && strings.TrimSpace(tokens[2]) == "" {
			continue
		}
		if len(tokens) == 2 && strings.TrimSpace(tokens[1]) == "" {
			continue
		}
		port, err := strconv.Atoi(resolveVar(hostTok))
		if err != nil {
			continue
		}
		open := !hasIP
		if hasIP {
			ip := resolveVar(ipTok)
			open = ip == "" || ip == "0.0.0.0" || ip == "::" || ip == "[::]"
		}
		out = append(out, Occurrence{Line: i + 1, Port: port, Role: RoleBind, Open: open})
	}
	return out
}

// composeItem returns the text of a YAML list item, without quotes or a
// trailing comment.
func composeItem(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "-") {
		return "", false
	}
	item := strings.TrimSpace(trimmed[1:])
	if idx := strings.Index(item, " #"); idx >= 0 {
		item = strings.TrimSpace(item[:idx])
	}
	item = strings.Trim(item, `"'`)
	if item == "" {
		return "", false
	}
	return item, true
}

// splitTopLevel splits a mapping on colons that are not inside ${...}, and
// drops a "/tcp" protocol suffix from the last token.
func splitTopLevel(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '$' && i+1 < len(s) && s[i+1] == '{':
			depth++
			i++
		case s[i] == '}' && depth > 0:
			depth--
		case s[i] == ':' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	if last := len(parts) - 1; last >= 0 {
		if idx := strings.IndexByte(parts[last], '/'); idx >= 0 {
			parts[last] = parts[last][:idx]
		}
	}
	return parts
}

// resolveVar returns the default of ${NAME:-default}; any other token is
// returned as written, and an unresolved ${NAME} with no default is returned
// unchanged so it never parses as a number.
func resolveVar(tok string) string {
	if strings.HasPrefix(tok, "${") && strings.HasSuffix(tok, "}") {
		inner := tok[2 : len(tok)-1]
		if idx := strings.Index(inner, ":-"); idx >= 0 {
			return inner[idx+2:]
		}
		return tok
	}
	return tok
}

var (
	scriptPatterns = []*regexp.Regexp{
		regexp.MustCompile(`--port[=\s]+(\d+)`),
		regexp.MustCompile(`(?:^|\s)-p[=\s]+(\d+)`),
		regexp.MustCompile(`\bPORT=(\d+)`),
	}
	localhostRef = regexp.MustCompile(`localhost:(\d+)`)
	loopbackRef  = regexp.MustCompile(`(?:host\.docker\.internal|localhost|127\.0\.0\.1):(\d+)`)
	envAssign    = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*['"]?(\d+)['"]?\s*(?:#.*)?$`)
	envLoopback  = regexp.MustCompile(`(?:host\.docker\.internal|localhost|127\.0\.0\.1):(\d+)`)
	goLiteral    = regexp.MustCompile("\"((?:[^\"\\\\]|\\\\.)*)\"|`([^`]*)`")
	goLoopback   = regexp.MustCompile(`(?:localhost|127\.0\.0\.1):(\d+)`)
	goBindLit    = regexp.MustCompile(`^:(\d+)$`)
	goNamedPort  = regexp.MustCompile(`(?i)\b[A-Za-z_]*port[A-Za-z_0-9]*\s*(?::=|=|:)\s*(\d+)\b`)
	goEnvDefault = regexp.MustCompile(`(?i)"([A-Za-z_]*port[A-Za-z_]*)"\s*,\s*"(\d+)"`)
)

// dependencyKeyPrefixes names env keys whose *PORT* value is the port of a
// dependency the repository connects to. Such a value is a USE, not a BIND, so
// a repository that only reads SMTP_PORT is not the owner of mailpit-smtp.
// Any other *PORT* key is a BIND (the repository listens on it).
var dependencyKeyPrefixes = []string{
	"SMTP", "MAIL", "MAILPIT", "DB", "DATABASE", "POSTGRES", "PG", "REDIS", "NATS",
	"OTLP", "OTEL", "PROMETHEUS", "LLDAP", "KEYCLOAK", "AUTH_", "NOTIFICATIONS_",
	"GATEWAY_", "FLAGS_",
}

// portKeyRole classifies a *PORT* env key name: a dependency prefix is USE, any
// other key is BIND. The match is case-insensitive.
func portKeyRole(key string) Role {
	upper := strings.ToUpper(key)
	for _, prefix := range dependencyKeyPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return RoleUse
		}
	}
	return RoleBind
}

// packageHits reads --port N, -p N and PORT=N inside package.json scripts.
func packageHits(content string, lines []string) []Occurrence {
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(content), &pkg); err != nil {
		return nil
	}
	keys := make([]string, 0, len(pkg.Scripts))
	for k := range pkg.Scripts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Occurrence
	for _, key := range keys {
		line := lineContaining(lines, `"`+key+`"`)
		for _, re := range scriptPatterns {
			for _, m := range re.FindAllStringSubmatch(pkg.Scripts[key], -1) {
				if port, err := strconv.Atoi(m[1]); err == nil {
					out = append(out, Occurrence{Line: line, Port: port, Role: RoleBind})
				}
			}
		}
	}
	return out
}

func lineContaining(lines []string, needle string) int {
	for i, l := range lines {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	return 1
}

// envHits reads *PORT*=N assignments (role from portKeyRole) and loopback URLs as uses.
func envHits(lines []string) []Occurrence {
	var out []Occurrence
	for i, line := range lines {
		if m := envAssign.FindStringSubmatch(line); m != nil && strings.Contains(strings.ToUpper(m[1]), "PORT") {
			if port, err := strconv.Atoi(m[2]); err == nil {
				out = append(out, Occurrence{Line: i + 1, Port: port, Role: portKeyRole(m[1])})
			}
		}
		out = append(out, numbersIn(i+1, line, envLoopback, RoleUse)...)
	}
	return out
}

// goHits reads string literals: loopback addresses are uses, ":N" literals are
// binds. Names containing Port with a number default are binds.
func goHits(lines []string) []Occurrence {
	var out []Occurrence
	for i, line := range lines {
		lineNo := i + 1
		for _, m := range goLiteral.FindAllStringSubmatch(line, -1) {
			content := m[1] + m[2]
			out = append(out, numbersIn(lineNo, content, goLoopback, RoleUse)...)
			if b := goBindLit.FindStringSubmatch(content); b != nil {
				if port, err := strconv.Atoi(b[1]); err == nil {
					out = append(out, Occurrence{Line: lineNo, Port: port, Role: RoleBind})
				}
			}
		}
		for _, m := range goNamedPort.FindAllStringSubmatch(line, -1) {
			if port, err := strconv.Atoi(m[1]); err == nil {
				out = append(out, Occurrence{Line: lineNo, Port: port, Role: RoleBind})
			}
		}
		for _, m := range goEnvDefault.FindAllStringSubmatch(line, -1) {
			if port, err := strconv.Atoi(m[2]); err == nil {
				out = append(out, Occurrence{Line: lineNo, Port: port, Role: portKeyRole(m[1])})
			}
		}
	}
	return out
}

// matchLines reports every capture of re on each line as a role occurrence.
func matchLines(lines []string, re *regexp.Regexp, role Role) []Occurrence {
	var out []Occurrence
	for i, line := range lines {
		out = append(out, numbersIn(i+1, line, re, role)...)
	}
	return out
}

func numbersIn(line int, text string, re *regexp.Regexp, role Role) []Occurrence {
	var out []Occurrence
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if port, err := strconv.Atoi(m[1]); err == nil {
			out = append(out, Occurrence{Line: line, Port: port, Role: role})
		}
	}
	return out
}
