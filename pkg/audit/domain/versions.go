package domain

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Pins are the CI tool versions declared by manovaspace/ci/ci/versions.env.
type Pins struct {
	Go     string
	Bun    string
	Node   string
	Next   string
	React  string
	Alpine string
}

// ParseVersionsEnv reads the plain KEY=value lines of versions.env. Comments
// and blank lines are ignored.
func ParseVersionsEnv(text string) Pins {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		if key != "" {
			values[key] = value
		}
	}
	return Pins{
		Go:     values["GO_VERSION"],
		Bun:    values["BUN_VERSION"],
		Node:   values["NODE_VERSION"],
		Next:   values["NEXT_VERSION"],
		React:  values["REACT_VERSION"],
		Alpine: values["ALPINE_VERSION"],
	}
}

// GoModule is the version-relevant part of a go.mod file.
type GoModule struct {
	Directive string
	Toolchain string
}

// ParseGoMod reads the go directive and the optional toolchain line.
func ParseGoMod(text string) GoModule {
	var module GoModule
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "go":
			if module.Directive == "" {
				module.Directive = strings.TrimPrefix(fields[1], "go")
			}
		case "toolchain":
			module.Toolchain = strings.TrimPrefix(fields[1], "go")
		}
	}
	return module
}

// Declaration is one dependency entry of a package.json file.
type Declaration struct {
	Section string
	Name    string
	Spec    string
}

// PackageJSON is the version-relevant part of a package.json file.
type PackageJSON struct {
	PackageManager string
	Engines        map[string]string
	Declarations   []Declaration
}

// ParsePackageJSON reads packageManager, engines and every dependency section.
func ParsePackageJSON(data []byte) (PackageJSON, error) {
	var raw struct {
		PackageManager   string            `json:"packageManager"`
		Engines          map[string]string `json:"engines"`
		Dependencies     map[string]string `json:"dependencies"`
		DevDependencies  map[string]string `json:"devDependencies"`
		PeerDependencies map[string]string `json:"peerDependencies"`
		OptionalDeps     map[string]string `json:"optionalDependencies"`
		Workspaces       json.RawMessage   `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return PackageJSON{}, err
	}
	pkg := PackageJSON{PackageManager: raw.PackageManager, Engines: raw.Engines}
	// workspaces is an object here but an array in older manifests; only the
	// catalog list carries version ranges.
	var workspaces struct {
		Catalog map[string]string `json:"catalog"`
	}
	_ = json.Unmarshal(raw.Workspaces, &workspaces)
	sections := []struct {
		name string
		deps map[string]string
	}{
		{"dependencies", raw.Dependencies},
		{"devDependencies", raw.DevDependencies},
		{"peerDependencies", raw.PeerDependencies},
		{"optionalDependencies", raw.OptionalDeps},
		{"workspaces.catalog", workspaces.Catalog},
	}
	for _, section := range sections {
		for name, spec := range section.deps {
			pkg.Declarations = append(pkg.Declarations, Declaration{Section: section.name, Name: name, Spec: spec})
		}
	}
	return pkg, nil
}

// Spec returns the first declared spec for name.
func (p PackageJSON) Spec(name string) (string, bool) {
	for _, decl := range p.Declarations {
		if decl.Name == name {
			return decl.Spec, true
		}
	}
	return "", false
}

// BunPin extracts the version from a packageManager value such as
// "bun@1.4.0+abc123".
func BunPin(packageManager string) string {
	if !strings.HasPrefix(packageManager, "bun@") {
		return ""
	}
	pin := strings.TrimPrefix(packageManager, "bun@")
	if i := strings.IndexAny(pin, "+-"); i > 0 {
		pin = pin[:i]
	}
	return pin
}

// ImageRef is one container image reference read from a Dockerfile or Compose
// file.
type ImageRef struct {
	Repository string
	Tag        string
}

var (
	dockerfileFrom = regexp.MustCompile(`(?mi)^\s*FROM\s+(?:--platform=\S+\s+)?(\S+)`)
	composeImage   = regexp.MustCompile(`(?m)^\s*image:\s*["']?([^"'\s#]+)`)
	numericTag     = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)+$`)
)

// ParseDockerfileImages reads every FROM image.
func ParseDockerfileImages(text string) []ImageRef {
	var refs []ImageRef
	for _, match := range dockerfileFrom.FindAllStringSubmatch(text, -1) {
		if ref, ok := parseImage(match[1]); ok {
			refs = append(refs, ref)
		}
	}
	return refs
}

// ParseComposeImages reads every image: entry.
func ParseComposeImages(text string) []ImageRef {
	var refs []ImageRef
	for _, match := range composeImage.FindAllStringSubmatch(text, -1) {
		if ref, ok := parseImage(match[1]); ok {
			refs = append(refs, ref)
		}
	}
	return refs
}

func parseImage(value string) (ImageRef, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "scratch" {
		return ImageRef{}, false
	}
	if i := strings.Index(value, "@"); i > 0 {
		value = value[:i]
	}
	// The tag follows the last colon; a registry port has a slash after it.
	colon := strings.LastIndex(value, ":")
	if colon <= strings.LastIndex(value, "/") {
		return ImageRef{Repository: value}, false
	}
	return ImageRef{Repository: value[:colon], Tag: value[colon+1:]}, true
}

// ComparableTag returns the tag when it pins a full version. Floating tags
// such as "24-alpine" are left alone: they are not a component drift.
func ComparableTag(tag string) (string, bool) {
	tag = strings.TrimSpace(tag)
	if numericTag.MatchString(tag) {
		return strings.TrimPrefix(tag, "v"), true
	}
	return "", false
}

// ToolForImage maps a container image repository to the versions.env pin it
// follows.
func ToolForImage(repository string) (tool, pinKey string) {
	switch {
	case repository == "oven/bun" || strings.HasSuffix(repository, "/bun"):
		return "bun", "bun"
	case repository == "golang" || strings.HasSuffix(repository, "/golang"):
		return "go", "go"
	case repository == "node" || strings.HasSuffix(repository, "/node"):
		return "node", "node"
	case repository == "alpine" || strings.HasSuffix(repository, "/alpine"):
		return "alpine", "alpine"
	}
	return "", ""
}

// ExactPin reports whether spec is an exact version, as versions.env pins one.
func ExactPin(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	return numericTag.MatchString(spec)
}

// ForbiddenSpec reports dependency specs the audit refuses: file:, "*" and
// "latest" are not reproducible.
func ForbiddenSpec(spec string) bool {
	spec = strings.TrimSpace(spec)
	switch {
	case spec == "", spec == "*", spec == "latest":
		return true
	case strings.HasPrefix(spec, "file:"), strings.HasPrefix(spec, "link:"):
		return true
	}
	return false
}

// AllowsVersion reports whether version satisfies constraint. Unparseable
// constraints are treated as satisfied so the audit never invents drift.
func AllowsVersion(constraint, version string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return true
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return true
	}
	v, err := semver.NewVersion(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	if err != nil {
		return true
	}
	return c.Check(v)
}

// CompareVersions compares numeric version components: -1 when a < b, 0 when
// they are equal and 1 when a > b. Missing components count as zero, so
// "1.26" equals "1.26.0" but not "1.26.7".
func CompareVersions(a, b string) int {
	left := versionComponents(a)
	right := versionComponents(b)
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for i := 0; i < length; i++ {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l < r {
			return -1
		}
		if l > r {
			return 1
		}
	}
	return 0
}

var versionPrefix = regexp.MustCompile(`^[vV]?(?:go)?([0-9]+(?:\.[0-9]+)*)`)

func versionComponents(value string) []int {
	value = strings.TrimSpace(value)
	match := versionPrefix.FindStringSubmatch(value)
	if match == nil {
		return nil
	}
	parts := strings.Split(match[1], ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}
