package application

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
)

// DefaultPackage is the design-system package `o audit deps` reports when no
// package argument is given.
const DefaultPackage = "@manovaspace/ui"

// PinsPath is the workspace-relative versions.env the audit reads.
const PinsPath = "manovaspace/ci/ci/versions.env"

// ExceptionsPath is the workspace-relative exception file the audit reads.
// Absent means no exceptions.
const ExceptionsPath = "handbook/agents/workspace/version-exceptions.yaml"

// Result is one audit run: the report plus a human summary detail. The report
// embeds so the JSON data keeps findings and unverified at its top level.
type Result struct {
	domain.Report
	Detail string `json:"detail,omitempty"`
}

// Service is the audit use-case layer.
type Service struct {
	Root         string
	ManifestPath string
	Workspace    Workspace
	Files        Files
	VCS          VCS
	Registry     Registry
	Advisories   Advisories
	GitHub       GitHub
	Commands     Commands
	// Now is the clock the merge lookback uses; nil means time.Now.
	Now func() time.Time
}

// repoScan is the shared repository view every audit command starts from.
type repoScan struct {
	repos  []Repo
	report domain.Report
}

// All runs every local check, plus the remote checks when remote is true.
func (s *Service) All(ctx context.Context, remote bool) Result {
	results := []Result{
		s.Versions(ctx),
		s.Deps(ctx, DefaultPackage, remote),
		s.Usage(ctx, "", false),
		s.Agents(ctx),
		s.Tests(ctx, remote),
	}
	if remote {
		results = append(results, s.Upstream(ctx, true), s.Merges(ctx, DefaultMergedDays, true))
	}

	report := domain.NewReport()
	var details []string
	for _, result := range results {
		report.Merge(result.Report)
		if result.Detail != "" {
			details = append(details, result.Detail)
		}
	}
	return Result{Report: report, Detail: strings.Join(details, "; ")}
}

// Versions compares each repository against the versions.env pins.
func (s *Service) Versions(ctx context.Context) Result {
	scan := s.scan()
	report := scan.report

	pins, err := s.loadPins()
	if err != nil {
		report.AddUnverified(err.Error())
		return Result{Report: report}
	}
	for _, repo := range scan.repos {
		s.addVersionFindings(&report, repo, pins)
	}
	report.Findings = domain.ApplyExceptions(report.Findings, s.loadExceptions())

	detail := fmt.Sprintf("%d repositories checked against %s", len(scan.repos), PinsPath)
	if !hasPin(pins) {
		detail = fmt.Sprintf("%d repositories checked; %s declares no pins", len(scan.repos), PinsPath)
	}
	return Result{Report: report, Detail: detail}
}

// Deps reports resolved versions, shadowed kit exports and forbidden
// dependency specs. It reads the npm registry only when remote is true.
func (s *Service) Deps(ctx context.Context, pkg string, remote bool) Result {
	if strings.TrimSpace(pkg) == "" {
		pkg = DefaultPackage
	}
	scan := s.scan()
	report := scan.report

	var allResolved []string
	for _, repo := range scan.repos {
		declared, pkgErr := s.packageJSON(repo)
		if pkgErr != nil {
			report.AddUnverified(repo.Path + ": package.json is not valid JSON")
		}
		for _, decl := range declared.Declarations {
			if !domain.ForbiddenSpec(decl.Spec) {
				continue
			}
			report.Add(domain.Finding{
				ID:       "deps.forbidden",
				Severity: domain.SeverityError,
				Subject:  decl.Name,
				Actual:   decl.Spec,
				Expected: "a pinned version",
				Fix:      fmt.Sprintf("replace %q for %s in %s", decl.Spec, decl.Name, repoFile(repo, "package.json")),
			})
		}

		lock, lockErr := s.Files.ReadFile(filepath.Join(repo.Dir, "bun.lock"))
		hasLock := lockErr == nil
		resolved := []domain.Resolved{}
		if hasLock {
			resolved = domain.ParseBunLock(string(lock), pkg)
		}
		versions := make([]string, 0, len(resolved))
		localKit := false
		for _, entry := range resolved {
			// A workspace: resolution means this repo owns the package; its own
			// source exports are definitions, not shadows.
			if strings.HasPrefix(entry.Version, "workspace:") {
				localKit = true
				continue
			}
			versions = append(versions, entry.Version)
		}
		versions = domain.Versions(versions)

		_, declaredKit := declared.Spec(pkg)
		if !localKit && (declaredKit || s.importsPackage(repo, pkg)) {
			findings, unverified := s.shadowFindings(repo, pkg)
			report.AddAll(findings)
			report.AddUnverified(unverified)
		}
		if !hasLock {
			if len(declared.Declarations) > 0 {
				report.AddUnverified(repo.Path + ": no bun.lock (resolved versions unverified)")
			}
			continue
		}
		if len(versions) == 0 {
			continue
		}
		allResolved = append(allResolved, versions...)
		if len(versions) > 1 {
			report.Add(domain.Finding{
				ID:       "deps.multiple",
				Severity: domain.SeverityWarn,
				Subject:  pkg,
				Actual:   strings.Join(versions, ", "),
				Expected: "a single resolved version",
				Fix:      fmt.Sprintf("use one %s version in %s", pkg, repoFile(repo, "bun.lock")),
			})
		}
	}

	allResolved = domain.Versions(allResolved)
	if len(allResolved) > 1 {
		report.Add(domain.Finding{
			ID:       "deps.multiple",
			Severity: domain.SeverityWarn,
			Subject:  pkg,
			Actual:   strings.Join(allResolved, ", "),
			Expected: "a single resolved version",
			Fix:      fmt.Sprintf("use one %s version across the workspace", pkg),
		})
	}

	if remote {
		s.addRemoteFinding(ctx, &report, pkg, allResolved)
	}
	report.Findings = domain.ApplyExceptions(report.Findings, s.loadExceptions())

	detail := fmt.Sprintf("%d resolved %s versions", len(allResolved), pkg)
	if len(allResolved) > 0 {
		detail = fmt.Sprintf("%s: %s", pkg, strings.Join(allResolved, ", "))
	}
	return Result{Report: report, Detail: detail}
}

// Usage verifies that named kit imports exist in the installed package and,
// with unused, lists kit exports nothing imports.
func (s *Service) Usage(ctx context.Context, symbol string, unused bool) Result {
	scan := s.scan()
	report := scan.report

	cache := map[string][]string{}
	used := map[string]map[string]bool{}
	unverified := map[string]bool{}
	namespacePkgs := map[string]bool{}
	walked := 0
	for _, repo := range scan.repos {
		if !s.Files.Exists(repo.Dir) {
			continue
		}
		walked++
		used[repo.Path] = map[string]bool{}
		err := s.Files.WalkFiles(repo.Dir, func(path string) error {
			data, err := s.Files.ReadFile(path)
			if err != nil {
				return nil
			}
			named, namespaces := domain.ParseImports(string(data))
			for _, imported := range named {
				if !strings.HasPrefix(imported.Path, "@manovaspace/") {
					continue
				}
				exports, ok := s.exports(cache, repo, imported.Path)
				if !ok {
					key := repo.Path + "\x00" + imported.Path
					if !unverified[key] {
						unverified[key] = true
						report.AddUnverified(fileRef(repo, path) + ": " + imported.Path + " is not installed (exports unverified)")
					}
					continue
				}
				used[repo.Path][imported.Symbol] = true
				if symbol != "" && imported.Symbol != symbol {
					continue
				}
				if !contains(exports, imported.Symbol) {
					report.Add(domain.Finding{
						ID:       "usage.unknown",
						Severity: domain.SeverityWarn,
						Subject:  imported.Symbol,
						Actual:   imported.Symbol,
						Expected: "exported by " + imported.Path,
						Fix:      fmt.Sprintf("import %s from %s or remove the import in %s", imported.Symbol, imported.Path, fileRef(repo, path)),
					})
				}
			}
			for _, namespace := range namespaces {
				if !strings.HasPrefix(namespace.Path, "@manovaspace/") {
					continue
				}
				namespacePkgs[repo.Path+"\x00"+namespace.Path] = true
				key := repo.Path + "\x00namespace\x00" + namespace.Path
				if !unverified[key] {
					unverified[key] = true
					report.AddUnverified(fileRef(repo, path) + ": namespace import of " + namespace.Path + " (symbols unverified)")
				}
			}
			return nil
		})
		if err != nil {
			report.AddUnverified(repo.Path + ": cannot walk sources")
		}
	}

	if unused {
		for repoPath, symbols := range used {
			repo, ok := repoByPath(scan.repos, repoPath)
			if !ok {
				continue
			}
			for pkg, exports := range cacheForRepo(cache, repoPath) {
				if namespacePkgs[repoPath+"\x00"+pkg] {
					continue
				}
				for _, name := range exports {
					if symbol != "" && name != symbol {
						continue
					}
					if symbols[name] {
						continue
					}
					report.Add(domain.Finding{
						ID:       "usage.unused",
						Severity: domain.SeverityInfo,
						Subject:  name,
						Actual:   name,
						Expected: "imported somewhere in " + repo.Path,
						Fix:      fmt.Sprintf("remove %s from %s or document why it is exported", name, DefaultPackage),
					})
				}
			}
		}
	}
	report.Findings = domain.ApplyExceptions(report.Findings, s.loadExceptions())

	detail := fmt.Sprintf("%d repositories scanned for kit usage", walked)
	if symbol != "" {
		detail = fmt.Sprintf("symbol %s; %s", symbol, detail)
	}
	return Result{Report: report, Detail: detail}
}

// scan resolves the repository list, records missing clones and stale checkouts.
func (s *Service) scan() repoScan {
	report := domain.NewReport()
	if s.Workspace == nil || s.Files == nil {
		report.AddUnverified("audit: workspace wiring missing")
		return repoScan{report: report}
	}
	repos, err := s.Workspace.Repos(s.Root, s.ManifestPath)
	if err != nil {
		report.AddUnverified("workspace manifest: " + err.Error())
		return repoScan{report: report}
	}
	var present []Repo
	for _, repo := range repos {
		repo.Path = strings.ReplaceAll(repo.Path, "\\", "/")
		if domain.SkipPath(repo.Path) {
			continue
		}
		if !s.Files.Exists(repo.Dir) {
			report.AddUnverified(repo.Path + ": repository not cloned")
			continue
		}
		present = append(present, repo)
	}
	report.AddAll(s.cloneFindings(present))
	return repoScan{repos: present, report: report}
}

// cloneFindings warns when local HEAD is not the fetched origin default branch:
// the files on disk are stale evidence.
func (s *Service) cloneFindings(repos []Repo) []domain.Finding {
	if s.VCS == nil {
		return nil
	}
	var findings []domain.Finding
	for _, repo := range repos {
		head, origin, err := s.VCS.Head(repo.Dir, repo.DefaultBranch)
		if err != nil || head == "" || origin == "" || head == origin {
			continue
		}
		findings = append(findings, domain.Finding{
			ID:       "clone.behind",
			Severity: domain.SeverityWarn,
			Subject:  repo.Path,
			Actual:   head,
			Expected: origin,
			Fix:      "o sync",
		})
	}
	return findings
}

func (s *Service) addVersionFindings(report *domain.Report, repo Repo, pins domain.Pins) {
	if data, err := s.Files.ReadFile(filepath.Join(repo.Dir, "go.mod")); err == nil {
		module := domain.ParseGoMod(string(data))
		actual := ""
		if module.Directive != "" && domain.CompareVersions(module.Directive, pins.Go) != 0 {
			actual = module.Directive
		}
		if actual == "" && module.Toolchain != "" && domain.CompareVersions(module.Toolchain, pins.Go) != 0 {
			actual = module.Toolchain
		}
		if actual != "" && pins.Go != "" {
			report.Add(domain.Finding{
				ID:       "versions.go",
				Severity: domain.SeverityError,
				Subject:  "go",
				Actual:   actual,
				Expected: pins.Go,
				Fix:      fmt.Sprintf("set go %s in %s", pins.Go, repoFile(repo, "go.mod")),
			})
		}
	}

	pkg, err := s.packageJSON(repo)
	if err != nil {
		report.AddUnverified(repo.Path + ": package.json is not valid JSON")
	}
	if pin := domain.BunPin(pkg.PackageManager); pin != "" && pins.Bun != "" && domain.CompareVersions(pin, pins.Bun) != 0 {
		report.Add(domain.Finding{
			ID:       "versions.bun",
			Severity: domain.SeverityError,
			Subject:  "bun",
			Actual:   pin,
			Expected: pins.Bun,
			Fix:      fmt.Sprintf("set packageManager to bun@%s in %s", pins.Bun, repoFile(repo, "package.json")),
		})
	}
	for _, engine := range []struct {
		id, tool, pin string
	}{
		{"versions.bun", "bun", pins.Bun},
		{"versions.node", "node", pins.Node},
	} {
		constraint, ok := pkg.Engines[engine.tool]
		if !ok || engine.pin == "" || domain.AllowsVersion(constraint, engine.pin) {
			continue
		}
		report.Add(domain.Finding{
			ID:       engine.id,
			Severity: domain.SeverityError,
			Subject:  engine.tool,
			Actual:   constraint,
			Expected: "allows " + engine.pin,
			Fix:      fmt.Sprintf("widen engines.%s in %s to allow %s", engine.tool, repoFile(repo, "package.json"), engine.pin),
		})
	}
	for _, target := range []struct {
		id, tool, pin string
	}{
		{"versions.next", "next", pins.Next},
		{"versions.react", "react", pins.React},
	} {
		spec, ok := pkg.Spec(target.tool)
		if !ok || target.pin == "" {
			continue
		}
		if !domain.ExactPin(spec) {
			report.Add(domain.Finding{
				ID:       target.id,
				Severity: domain.SeverityError,
				Subject:  target.tool,
				Actual:   spec,
				Expected: target.pin,
				Fix:      fmt.Sprintf("pin %s to %s in %s", target.tool, target.pin, repoFile(repo, "package.json")),
			})
			continue
		}
		if domain.CompareVersions(spec, target.pin) != 0 {
			report.Add(domain.Finding{
				ID:       target.id,
				Severity: domain.SeverityError,
				Subject:  target.tool,
				Actual:   spec,
				Expected: target.pin,
				Fix:      fmt.Sprintf("pin %s to %s in %s", target.tool, target.pin, repoFile(repo, "package.json")),
			})
		}
	}

	for _, name := range []string{"Dockerfile", "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		data, err := s.Files.ReadFile(filepath.Join(repo.Dir, name))
		if err != nil {
			continue
		}
		var refs []domain.ImageRef
		if strings.HasPrefix(name, "Dockerfile") {
			refs = domain.ParseDockerfileImages(string(data))
		} else {
			refs = domain.ParseComposeImages(string(data))
		}
		for _, ref := range refs {
			tool, _ := domain.ToolForImage(ref.Repository)
			if tool == "" {
				continue
			}
			want := pinFor(pins, tool)
			tag, ok := domain.ComparableTag(ref.Tag)
			if want == "" || !ok || domain.CompareVersions(tag, want) == 0 {
				continue
			}
			report.Add(domain.Finding{
				ID:       "versions." + tool,
				Severity: domain.SeverityError,
				Subject:  tool,
				Actual:   ref.Tag,
				Expected: want,
				Fix:      fmt.Sprintf("update %s to %s:%s", repoFile(repo, name), ref.Repository, want),
			})
		}
	}
}

// importsPackage reports whether any source file references the module path.
func (s *Service) importsPackage(repo Repo, pkg string) bool {
	quoted := "\"" + pkg + "\""
	found := false
	_ = s.Files.WalkFiles(repo.Dir, func(path string) error {
		data, err := s.Files.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), quoted) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// shadowFindings reports local exports that reuse a kit export name.
func (s *Service) shadowFindings(repo Repo, pkg string) ([]domain.Finding, string) {
	exportsFile, ok := s.Files.InstalledExports(repo.Dir, pkg)
	if !ok {
		return nil, repo.Path + ": " + pkg + " is not installed (shadowed exports unverified)"
	}
	data, err := s.Files.ReadFile(exportsFile)
	if err != nil {
		return nil, repo.Path + ": cannot read installed " + pkg + " exports"
	}
	kit := domain.ParseKitExports(string(data))
	if len(kit) == 0 {
		return nil, repo.Path + ": installed " + pkg + " declares no exports"
	}
	seen := map[string]bool{}
	var findings []domain.Finding
	_ = s.Files.WalkFiles(repo.Dir, func(path string) error {
		source, err := s.Files.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, name := range domain.ParseLocalExports(string(source)) {
			if seen[name] || !contains(kit, name) {
				continue
			}
			seen[name] = true
			findings = append(findings, domain.Finding{
				ID:       "deps.shadow",
				Severity: domain.SeverityWarn,
				Subject:  name,
				Actual:   name,
				Expected: "imported from " + pkg,
				Fix:      fmt.Sprintf("import %s from %s instead of declaring it in %s", name, pkg, fileRef(repo, path)),
			})
		}
		return nil
	})
	sort.Slice(findings, func(i, j int) bool { return findings[i].Subject < findings[j].Subject })
	return findings, ""
}

func (s *Service) addRemoteFinding(ctx context.Context, report *domain.Report, pkg string, resolved []string) {
	if s.Registry == nil {
		report.AddUnverified(pkg + ": remote registry wiring missing")
		return
	}
	latest, err := s.Registry.Latest(ctx, pkg)
	if err != nil {
		report.AddUnverified(pkg + ": registry lookup failed: " + err.Error())
		return
	}
	if latest == "" || contains(resolved, latest) {
		return
	}
	report.Add(domain.Finding{
		ID:       "deps.outdated",
		Severity: domain.SeverityWarn,
		Subject:  pkg,
		Actual:   strings.Join(resolved, ", "),
		Expected: latest,
		Fix:      fmt.Sprintf("bun add %s@%s in the repositories that resolve it", pkg, latest),
	})
}

func (s *Service) loadPins() (domain.Pins, error) {
	data, err := s.Files.ReadFile(filepath.Join(s.Root, filepath.FromSlash(PinsPath)))
	if err != nil {
		return domain.Pins{}, fmt.Errorf("%s: %w", PinsPath, err)
	}
	return domain.ParseVersionsEnv(string(data)), nil
}

func (s *Service) loadExceptions() []domain.Exception {
	if s.Files == nil {
		return nil
	}
	data, err := s.Files.ReadFile(filepath.Join(s.Root, filepath.FromSlash(ExceptionsPath)))
	if err != nil {
		return nil
	}
	exceptions, err := domain.ParseExceptions(data)
	if err != nil {
		return nil
	}
	return exceptions
}

func (s *Service) packageJSON(repo Repo) (domain.PackageJSON, error) {
	data, err := s.Files.ReadFile(filepath.Join(repo.Dir, "package.json"))
	if err != nil {
		return domain.PackageJSON{}, nil
	}
	return domain.ParsePackageJSON(data)
}

func (s *Service) exports(cache map[string][]string, repo Repo, pkg string) ([]string, bool) {
	key := repo.Path + "\x00" + pkg
	if exports, ok := cache[key]; ok {
		return exports, len(exports) > 0
	}
	file, ok := s.Files.InstalledExports(repo.Dir, pkg)
	if !ok {
		cache[key] = nil
		return nil, false
	}
	data, err := s.Files.ReadFile(file)
	if err != nil {
		cache[key] = nil
		return nil, false
	}
	exports := domain.ParseKitExports(string(data))
	cache[key] = exports
	return exports, len(exports) > 0
}

// cacheForRepo returns the installed package exports of one repository, keyed
// by module path.
func cacheForRepo(cache map[string][]string, repoPath string) map[string][]string {
	out := map[string][]string{}
	for key, exports := range cache {
		pkg, ok := strings.CutPrefix(key, repoPath+"\x00")
		if ok && pkg != "" {
			out[pkg] = exports
		}
	}
	return out
}

func repoByPath(repos []Repo, path string) (Repo, bool) {
	for _, repo := range repos {
		if repo.Path == path {
			return repo, true
		}
	}
	return Repo{}, false
}

func pinFor(pins domain.Pins, tool string) string {
	switch tool {
	case "go":
		return pins.Go
	case "bun":
		return pins.Bun
	case "node":
		return pins.Node
	case "alpine":
		return pins.Alpine
	}
	return ""
}

func hasPin(pins domain.Pins) bool {
	return pins.Go != "" || pins.Bun != "" || pins.Node != "" || pins.Next != "" || pins.React != ""
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// repoFile renders a repository-relative file for a fix command.
func repoFile(repo Repo, name string) string {
	return strings.TrimSuffix(repo.Path, "/") + "/" + strings.TrimPrefix(name, "/")
}

// fileRef renders a source file relative to the workspace repository.
func fileRef(repo Repo, file string) string {
	relative := strings.TrimPrefix(filepath.ToSlash(file), filepath.ToSlash(repo.Dir)+"/")
	return repoFile(repo, relative)
}
