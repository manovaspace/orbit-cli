// Package toolchains installs the language toolchains and local services the selected repositories
// declare, pins Bun to the version their package.json names, warns about Go and Node versions that
// are too old, puts the user tool directories on this run's PATH, and writes the orbit block into
// the login shell's rc files. Only declared tools are checked: a repository that does not list a
// tool in its orbit.yaml contributes nothing for it.
package toolchains

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/pkgmgr"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
)

const partName = "toolchains"

// minGo is the oldest Go this part accepts. Exact releases come from GOTOOLCHAIN=auto in the block.
var minGo = []int{1, 21}

// nodeMajorRe finds the first integer of an engines.node range such as ">=24" or "^24.1".
var nodeMajorRe = regexp.MustCompile(`\d+`)

type part struct {
	lookPath pkgmgr.LookPath
	setenv   func(key, value string) error
	getenv   func(key string) string
	home     func() (string, error)
}

// New returns the toolchains part, looking commands up on PATH, setting PATH in this process and
// reading the home directory and environment from the process.
func New() engine.Part {
	return part{lookPath: exec.LookPath, setenv: os.Setenv, getenv: os.Getenv, home: os.UserHomeDir}
}

func (part) Name() string    { return partName }
func (part) Needs() []string { return []string{"root"} }

// needs is what the selected repositories declare.
type needs struct {
	tools     map[string]bool // go, bun, node: declared by some selected repository
	nodeMajor int             // highest integer in an engines.node range; 0 when none
	bunPin    string          // highest packageManager bun version; "" when none
	postgres  bool
	mail      bool
}

// packageNames lists the logical package names to check, in a fixed order. Postgres and Mailpit
// are only installed for native services.
func (n needs) packageNames(services string) []string {
	var names []string
	for _, tc := range []string{"go", "node", "bun"} {
		if n.tools[tc] {
			names = append(names, tc)
		}
	}
	if services == "native" {
		if n.postgres {
			names = append(names, "postgres")
		}
		if n.mail {
			names = append(names, "mailpit")
		}
	}
	return names
}

// packageJSON holds the package.json fields this part reads.
type packageJSON struct {
	Engines        map[string]string `json:"engines"`
	PackageManager string            `json:"packageManager"`
}

// collectNeeds reads orbit.yaml of every selected repository. package.json is read only for the
// repositories that declare node or bun.
func collectNeeds(fsys engine.FS, root string, selected []manifest.RepoTarget) (needs, error) {
	n := needs{tools: map[string]bool{}}
	for _, target := range selected {
		dir := filepath.Join(root, target.Path)
		rs, ok, err := spec.LoadRepoSpec(dir)
		if err != nil {
			return needs{}, err
		}
		if !ok {
			continue
		}
		for _, tc := range rs.Toolchains {
			n.tools[tc] = true
		}
		if rs.Services.Postgres != nil {
			n.postgres = true
		}
		if rs.Services.Mail {
			n.mail = true
		}
		if !slices.Contains(rs.Toolchains, "node") && !slices.Contains(rs.Toolchains, "bun") {
			continue
		}
		pkg, err := readPackageJSON(fsys, dir)
		if err != nil {
			return needs{}, err
		}
		if slices.Contains(rs.Toolchains, "node") {
			if major := nodeMajor(pkg.Engines["node"]); major > n.nodeMajor {
				n.nodeMajor = major
			}
		}
		if slices.Contains(rs.Toolchains, "bun") {
			pin, err := bunPin(pkg.PackageManager)
			if err != nil {
				return needs{}, clierr.Wrap(err, clierr.Usage(filepath.Join(dir, "package.json")+" packageManager is not a bun version"))
			}
			if pin != "" && (n.bunPin == "" || compareVersions(mustParts(pin), mustParts(n.bunPin)) > 0) {
				n.bunPin = pin
			}
		}
	}
	return n, nil
}

// readPackageJSON returns the package.json in dir, or zero fields when there is none.
func readPackageJSON(fsys engine.FS, dir string) (packageJSON, error) {
	path := filepath.Join(dir, "package.json")
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return packageJSON{}, nil
	}
	if err != nil {
		return packageJSON{}, clierr.Wrap(err, clierr.Failed("cannot read "+path))
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return packageJSON{}, clierr.Wrap(err, clierr.Usage(path+" is malformed"))
	}
	return pkg, nil
}

// nodeMajor returns the first integer of an engines.node range, or 0 when there is none.
func nodeMajor(rng string) int {
	m := nodeMajorRe.FindString(rng)
	if m == "" {
		return 0
	}
	n, err := strconv.Atoi(m)
	if err != nil {
		return 0
	}
	return n
}

// bunPin returns the version of a packageManager field naming bun, without any +hash suffix. Other
// package managers and an empty field give "". A bun version that is not dotted integers is an error.
func bunPin(packageManager string) (string, error) {
	version, ok := strings.CutPrefix(packageManager, "bun@")
	if !ok {
		return "", nil
	}
	version, _, _ = strings.Cut(version, "+")
	if _, ok := versionParts(version); !ok {
		return "", fmt.Errorf("bun version %q is not dotted integers", version)
	}
	return version, nil
}

// Plan prepends the user tool directories to this process's PATH, plans the missing packages and
// the Bun pin, checks the versions of the declared tools that are installed, then plans the rc block.
func (p part) Plan(ctx context.Context, s *engine.State) (engine.Plan, error) {
	selected, err := s.Selected()
	if err != nil {
		return engine.Plan{}, err
	}
	n, err := collectNeeds(s.FS, s.Root, selected)
	if err != nil {
		return engine.Plan{}, err
	}
	home, err := p.home()
	if err != nil {
		return engine.Plan{}, clierr.Wrap(err, clierr.Failed("cannot find the home directory"))
	}
	prefix, hasBrew := brewPrefix(s)
	if err := p.prependPath(home, prefix, hasBrew); err != nil {
		return engine.Plan{}, err
	}

	var plan engine.Plan
	if err := p.planTools(ctx, s, n, &plan); err != nil {
		return engine.Plan{}, err
	}
	if err := p.planShellRC(s, home, &plan); err != nil {
		return engine.Plan{}, err
	}
	return plan, nil
}

// planTools plans the installs of missing packages, the Bun pin, and the version warnings for the
// declared tools that are already installed.
func (p part) planTools(ctx context.Context, s *engine.State, n needs, plan *engine.Plan) error {
	var missing []string
	if names := n.packageNames(s.Machine.Services); len(names) > 0 {
		mgr, err := pkgmgr.ForLookPath(s, p.lookPath)
		if err != nil {
			return err
		}
		if missing, err = mgr.Missing(ctx, names); err != nil {
			return err
		}
		// A pinned Bun is installed by its own action, never by the unpinned script package.
		var installs []string
		for _, name := range missing {
			if name != "bun" || n.bunPin == "" {
				installs = append(installs, name)
			}
		}
		for _, a := range mgr.Install(installs) {
			a.Part = partName
			plan.Actions = append(plan.Actions, a)
		}
	}

	if n.tools["bun"] && n.bunPin != "" {
		if action := p.planBunPin(ctx, s, n.bunPin, slices.Contains(missing, "bun"), plan); action != nil {
			plan.Actions = append(plan.Actions, *action)
		}
	}
	if n.tools["go"] && !slices.Contains(missing, "go") {
		checkGo(ctx, s, plan)
	}
	if n.tools["node"] && n.nodeMajor > 0 && !slices.Contains(missing, "node") {
		checkNode(ctx, s, n.nodeMajor, plan)
	}
	return nil
}

// planBunPin returns the action that installs the pinned Bun when it is missing or its major or minor
// differs from the pin. A patch difference is accepted. An unreadable installed version is a warning.
func (p part) planBunPin(ctx context.Context, s *engine.State, pin string, missing bool, plan *engine.Plan) *engine.Action {
	if missing {
		return bunPinAction(s, pin)
	}
	have, err := installedVersion(ctx, s, "bun", "--version")
	if err != nil {
		warn(plan, "bun", fmt.Sprintf("cannot read bun version: %v", err))
		return nil
	}
	got, ok := versionParts(have)
	if !ok {
		warn(plan, "bun", fmt.Sprintf("cannot read bun version %q", have))
		return nil
	}
	want := mustParts(pin)
	if versionAt(got, 0) == versionAt(want, 0) && versionAt(got, 1) == versionAt(want, 1) {
		return nil
	}
	return bunPinAction(s, pin)
}

// bunPinAction installs exactly the pinned Bun release with the official installer, without sudo.
func bunPinAction(s *engine.State, version string) *engine.Action {
	tty := s.Tty
	return &engine.Action{
		Part:    partName,
		Subject: "bun",
		Summary: "Install bun " + version,
		Do: func(ctx context.Context) error {
			return tty.RunInteractive(ctx, "", nil, "bash", "-c", "curl -fsSL https://bun.sh/install | bash -s bun-v"+version)
		},
	}
}

// checkGo warns when the installed Go is older than minGo.
func checkGo(ctx context.Context, s *engine.State, plan *engine.Plan) {
	v, err := installedVersion(ctx, s, "go", "env", "GOVERSION")
	if err != nil {
		warn(plan, "go", fmt.Sprintf("cannot read go version: %v", err))
		return
	}
	num := strings.TrimPrefix(v, "go")
	got, ok := versionParts(num)
	if !ok {
		warn(plan, "go", fmt.Sprintf("cannot read go version %q", v))
		return
	}
	if compareVersions(got, minGo) < 0 {
		warn(plan, "go", fmt.Sprintf("go %s is older than 1.21; upgrade it", num))
	}
}

// checkNode warns when the installed Node major is below the required major.
func checkNode(ctx context.Context, s *engine.State, required int, plan *engine.Plan) {
	v, err := installedVersion(ctx, s, "node", "--version")
	if err != nil {
		warn(plan, "node", fmt.Sprintf("cannot read node version: %v", err))
		return
	}
	num := strings.TrimPrefix(v, "v")
	got, ok := versionParts(num)
	if !ok {
		warn(plan, "node", fmt.Sprintf("cannot read node version %q", v))
		return
	}
	if versionAt(got, 0) < required {
		warn(plan, "node", fmt.Sprintf("node %s found; %d required", num, required))
	}
}

// installedVersion runs a version query for an installed tool and returns its trimmed stdout.
func installedVersion(ctx context.Context, s *engine.State, name string, args ...string) (string, error) {
	res, err := s.Run.Run(ctx, "", name, args...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func warn(plan *engine.Plan, subject, message string) {
	plan.Findings = append(plan.Findings, engine.Finding{
		Part:     partName,
		Subject:  subject,
		Message:  message,
		Severity: engine.SeverityWarn,
	})
}

// brewPrefix returns the Homebrew prefix on darwin when a brew binary is there. The Apple Silicon
// prefix is tried before the Intel one, matching the shell block.
func brewPrefix(s *engine.State) (string, bool) {
	if s.Platform != "darwin" {
		return "", false
	}
	for _, prefix := range []string{"/opt/homebrew", "/usr/local"} {
		if _, err := s.FS.Stat(prefix + "/bin/brew"); err == nil {
			return prefix, true
		}
	}
	return "", false
}

// prependPath puts the user tool directories, and on darwin the keg-only Homebrew formulae, first on
// PATH, skipping any already there. A directory need not exist yet.
func (p part) prependPath(home, prefix string, hasBrew bool) error {
	dirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, "go", "bin"),
	}
	if hasBrew {
		dirs = append(dirs, prefix+"/opt/node@24/bin", prefix+"/opt/postgresql@18/bin")
	}
	current := p.getenv("PATH")
	onPath := filepath.SplitList(current)
	var add []string
	for _, dir := range dirs {
		if !slices.Contains(onPath, dir) {
			add = append(add, dir)
		}
	}
	if len(add) == 0 {
		return nil
	}
	if current != "" {
		add = append(add, current)
	}
	if err := p.setenv("PATH", strings.Join(add, string(os.PathListSeparator))); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot set PATH"))
	}
	return nil
}

// versionParts parses v as dotted integers, such as 1.22.2 or 24.1.0. ok is false for anything else.
func versionParts(v string) ([]int, bool) {
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// mustParts is versionParts for a value already validated by bunPin or read from a pin.
func mustParts(v string) []int {
	parts, _ := versionParts(v)
	return parts
}

// versionAt returns component i of v, or 0 when v is shorter.
func versionAt(v []int, i int) int {
	if i < len(v) {
		return v[i]
	}
	return 0
}

// compareVersions orders a and b component by component, treating missing components as 0.
func compareVersions(a, b []int) int {
	for i := range max(len(a), len(b)) {
		if d := versionAt(a, i) - versionAt(b, i); d != 0 {
			return d
		}
	}
	return 0
}
