package application

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/audit/domain"
)

const (
	// RemoteRequired is the named reason a remote sensor reports without
	// --remote.
	RemoteRequired = "remote checks need --remote"
	// DefaultMergedDays is the lookback for `o audit merges`.
	DefaultMergedDays = 30

	agentCheckAll        = "handbook/agents/scripts/check-all.mjs"
	agentSetupMCP        = "handbook/agents/setup-mcp.py"
	agentOpencode        = "handbook/agents/opencode/manova-implementer.md"
	agentRTKRule         = "handbook/agents/rules/rtk.mdc"
	agentInstallArtifact = "handbook/agents/install-artifacts.json"
	handbookEval         = "handbook/packages/handbook-mcp/eval.mjs"
)

// editorCLIs are the editor runtimes the workspace generates configuration
// for.
var editorCLIs = []string{"claude", "opencode", "cursor", "codex", "gemini"}

// Upstream reports OSV advisories on resolved lockfile versions and toolchain
// pins that lag their upstream release. Without remote it makes no request.
func (s *Service) Upstream(ctx context.Context, remote bool) (result Result) {
	report := domain.NewReport()
	defer func() {
		s.applyAccepted(&report, "upstream")
		result.Report = report
	}()
	if !remote {
		report.AddUnverified(RemoteRequired)
		return Result{Report: report, Detail: "upstream checks skipped"}
	}

	if s.Files == nil {
		report.AddUnverified("upstream: files wiring missing")
		return Result{Report: report}
	}
	pins, err := s.loadPins()
	if err != nil {
		report.AddUnverified(err.Error())
	}
	s.addAdvisoryFindings(ctx, &report)

	checked := 0
	for _, tool := range []struct {
		name string
		pin  string
	}{
		{"go", pins.Go},
		{"bun", pins.Bun},
		{"node", pins.Node},
		{"next", pins.Next},
		{"react", pins.React},
	} {
		if tool.pin == "" {
			continue
		}
		latest, err := s.latestUpstream(ctx, tool.name)
		if err != nil {
			report.AddUnverified(tool.name + ": upstream lookup failed: " + err.Error())
			continue
		}
		checked++
		latest = normalizeUpstreamTag(latest)
		if latest == "" || domain.CompareVersions(latest, tool.pin) <= 0 {
			continue
		}
		report.Add(domain.Finding{
			ID:       "upstream.lag",
			Severity: domain.SeverityInfo,
			Subject:  tool.name,
			Actual:   tool.pin,
			Expected: latest,
			Fix:      fmt.Sprintf("bump %s to %s in %s", pinKey(tool.name), latest, PinsPath),
		})
	}

	return Result{Report: report, Detail: fmt.Sprintf("%d toolchain pins compared with upstream", checked)}
}

// Agents checks the handbook gates, the generated editor configuration and
// the agent runtimes.
func (s *Service) Agents(ctx context.Context) (result Result) {
	report := domain.NewReport()
	defer func() {
		s.applyAccepted(&report, "agents")
		result.Report = report
	}()
	if s.Files == nil || s.Commands == nil {
		report.AddUnverified("agents: wiring missing")
		return Result{Report: report}
	}

	ran := 0
	if s.Files.Exists(filepath.Join(s.Root, filepath.FromSlash(agentCheckAll))) {
		res, err := s.Commands.Run(ctx, s.Root, "bun", agentCheckAll, "--json")
		s.addCheckAllFindings(&report, res, err)
		ran++
	} else {
		report.AddUnverified("agents: " + agentCheckAll + " not found (handbook checks unverified)")
	}

	if s.Files.Exists(filepath.Join(s.Root, filepath.FromSlash(agentSetupMCP))) {
		res, err := s.Commands.Run(ctx, s.Root, "python3", agentSetupMCP, "--check")
		switch {
		case err == nil && res.ExitCode == 0:
		case res.ExitCode == 1:
			report.Add(domain.Finding{
				ID:       "agents.mcp-drift",
				Severity: domain.SeverityWarn,
				Subject:  "MCP configuration",
				Actual:   commandDetail(res),
				Expected: "no drift from handbook/agents/mcp.servers.json",
				Fix:      "python3 handbook/agents/setup-mcp.py",
			})
		case res.ExitCode == 0:
			report.AddUnverified("agents: python3 not installed (MCP configuration unverified)")
		default:
			report.AddUnverified("agents: setup-mcp.py --check failed: " + commandDetail(res))
		}
		ran++
	} else {
		report.AddUnverified("agents: " + agentSetupMCP + " not found (MCP configuration unverified)")
	}

	if !s.rtkHookPresent() {
		report.Add(domain.Finding{
			ID:       "agents.rtk-hook",
			Severity: domain.SeverityWarn,
			Subject:  "RTK",
			Actual:   "hook not detected",
			Expected: "RTK installed for agent shell output",
			Fix:      "install the RTK hook from handbook/agents",
		})
	}

	if !s.Files.Exists(filepath.Join(s.Root, filepath.FromSlash(agentOpencode))) {
		report.Add(domain.Finding{
			ID:       "agents.opencode-agent",
			Severity: domain.SeverityWarn,
			Subject:  "manova-implementer",
			Actual:   "missing",
			Expected: agentOpencode,
			Fix:      "restore the opencode manova-implementer agent",
		})
	}

	for _, editor := range editorCLIs {
		path, err := s.Commands.LookPath(editor)
		if err != nil {
			report.AddUnverified("agents: " + editor + " CLI not installed (runtime status unverified)")
			continue
		}
		report.Add(domain.Finding{
			ID:       "agents.editor",
			Severity: domain.SeverityInfo,
			Subject:  editor,
			Actual:   path,
			Expected: "installed",
		})
	}

	if s.Files.Exists(filepath.Join(s.Root, filepath.FromSlash(handbookEval))) {
		res, err := s.Commands.RunIsolated(ctx, s.Root, "bun", handbookEval)
		recall, mrr, ok := domain.ParseEvalSummary(res.Stdout)
		if ok {
			report.Add(domain.Finding{
				ID:       "agents.mcp-eval",
				Severity: domain.SeverityInfo,
				Subject:  "handbook-mcp",
				Actual:   fmt.Sprintf("recall@5=%s mrr=%s", recall, mrr),
				Expected: "recall@5 >= 0.80 and mrr >= 0.60",
			})
		}
		switch {
		case err != nil && res.ExitCode == 0:
			report.AddUnverified("agents: bun not installed (handbook-mcp recall unverified)")
		case err != nil || res.ExitCode != 0:
			report.Add(domain.Finding{
				ID:       "agents.mcp-eval",
				Severity: domain.SeverityError,
				Subject:  "handbook-mcp",
				Actual:   commandDetail(res),
				Expected: "recall@5 >= 0.80 and mrr >= 0.60",
				Fix:      "improve handbook retrieval until eval.mjs passes",
			})
		case !ok:
			report.AddUnverified("agents: eval.mjs printed no recall numbers")
		}
		ran++
	} else {
		report.AddUnverified("agents: " + handbookEval + " not found (handbook-mcp recall unverified)")
	}

	s.addRetiredNameFindings(&report)

	return Result{Report: report, Detail: fmt.Sprintf("%d agent checks ran", ran)}
}

// Merges flags merged pull requests whose checks failed or were absent and
// that carry no override comment.
func (s *Service) Merges(ctx context.Context, days int, remote bool) (result Result) {
	report := domain.NewReport()
	defer func() {
		s.applyAccepted(&report, "merges")
		result.Report = report
	}()
	if !remote {
		report.AddUnverified(RemoteRequired)
		return Result{Report: report}
	}
	if days <= 0 {
		days = DefaultMergedDays
	}
	repos, unverified := s.workspaceRepos()
	addUnverifiedAll(&report, unverified)
	if s.GitHub == nil {
		report.AddUnverified("merges: GitHub wiring missing")
		return Result{Report: report}
	}

	since := s.now().AddDate(0, 0, -days)
	checked := 0
	for _, repo := range repos {
		slug, ok := domain.GitHubSlug(repo.RemoteURL)
		if !ok {
			report.AddUnverified(repo.Path + ": no GitHub remote (merged pull requests unverified)")
			continue
		}
		prs, err := s.GitHub.MergedPullRequests(ctx, slug, since)
		if err != nil {
			report.AddUnverified("merges: " + slug + ": " + err.Error())
			continue
		}
		checked++
		for _, pr := range prs {
			if pr.HasOverride() {
				continue
			}
			failing := pr.FailingChecks()
			if len(failing) == 0 && !pr.NoChecks() {
				continue
			}
			reason := "no checks reported"
			if len(failing) > 0 {
				reason = "checks failed: " + strings.Join(failing, ", ")
			}
			report.Add(domain.Finding{
				ID:       "merges.bypass",
				Severity: domain.SeverityError,
				Subject:  fmt.Sprintf("%s#%d", repo.Path, pr.Number),
				Actual:   reason,
				Expected: "checks passed or an override: comment recorded",
				Fix:      fmt.Sprintf("require checks or post `override: <reason>` before merging %s", pr.URL),
			})
		}
	}

	return Result{Report: report, Detail: fmt.Sprintf("%d repositories checked for merges in the last %d days", checked, days)}
}

// Tests reports repositories with no test files, excluding the static-only
// sites, and reads Actions billing on remote runs.
func (s *Service) Tests(ctx context.Context, remote bool) (result Result) {
	report := domain.NewReport()
	defer func() {
		s.applyAccepted(&report, "tests")
		result.Report = report
	}()
	if s.Files == nil {
		report.AddUnverified("tests: files wiring missing")
		return Result{Report: report}
	}
	repos, unverified := s.workspaceRepos()
	addUnverifiedAll(&report, unverified)

	scanned := 0
	for _, repo := range repos {
		if domain.StaticOnlyRepo(repo.Path) {
			continue
		}
		if !s.Files.Exists(repo.Dir) {
			report.AddUnverified(repo.Path + ": repository not cloned (tests unverified)")
			continue
		}
		scanned++
		found := false
		err := s.Files.WalkAll(repo.Dir, func(path string) error {
			if domain.IsTestFile(path) {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil {
			report.AddUnverified(repo.Path + ": cannot walk files (tests unverified)")
			continue
		}
		if !found {
			report.Add(domain.Finding{
				ID:       "tests.missing",
				Severity: domain.SeverityWarn,
				Subject:  repo.Path,
				Expected: "at least one test file",
				Fix:      "add a test for " + repo.Path,
			})
		}
	}

	if remote {
		s.addActionsUsage(ctx, &report, repos)
	}

	return Result{Report: report, Detail: fmt.Sprintf("%d repositories scanned for tests", scanned)}
}

// addCheckAllFindings maps the check-all JSON report onto audit findings.
func (s *Service) addCheckAllFindings(report *domain.Report, res CommandResult, runErr error) {
	var decoded struct {
		OK       bool `json:"ok"`
		Findings []struct {
			Check   string `json:"check"`
			Code    string `json:"code"`
			File    string `json:"file"`
			Message string `json:"message"`
		} `json:"findings"`
		Unverified []struct {
			Check  string `json:"check"`
			File   string `json:"file"`
			Reason string `json:"reason"`
		} `json:"unverified"`
	}
	if strings.TrimSpace(res.Stdout) != "" {
		if err := json.Unmarshal([]byte(res.Stdout), &decoded); err == nil {
			for _, finding := range decoded.Findings {
				report.Add(domain.Finding{
					ID:       "agents.check-all",
					Severity: domain.SeverityError,
					Subject:  strings.TrimSpace(finding.Check + " " + finding.File),
					Actual:   "[" + finding.Code + "] " + finding.Message,
					Expected: "clean handbook checks",
					Fix:      "run bun " + agentCheckAll,
				})
			}
			for _, unverified := range decoded.Unverified {
				report.AddUnverified("check-all " + unverified.Check + ": " + unverified.File + " " + unverified.Reason)
			}
			if runErr != nil && res.ExitCode != 1 && len(decoded.Findings) == 0 && len(decoded.Unverified) == 0 {
				report.AddUnverified("check-all could not run: " + commandDetail(res))
			}
			return
		}
	}
	switch {
	case runErr != nil && res.ExitCode == 0:
		report.AddUnverified("agents: bun not installed (handbook checks unverified)")
	case runErr != nil || res.ExitCode != 0:
		report.Add(domain.Finding{
			ID:       "agents.check-all",
			Severity: domain.SeverityError,
			Subject:  "check-all",
			Actual:   commandDetail(res),
			Expected: "clean handbook checks",
			Fix:      "run bun " + agentCheckAll,
		})
	}
}

// addAdvisoryFindings queries OSV for every resolved lockfile version.
func (s *Service) addAdvisoryFindings(ctx context.Context, report *domain.Report) {
	if s.Files == nil {
		return
	}
	packages := s.resolvedPackages()
	if len(packages) == 0 {
		return
	}
	if s.Advisories == nil {
		report.AddUnverified("advisories: OSV wiring missing")
		return
	}
	hits, err := s.Advisories.Query(ctx, packages)
	if err != nil {
		report.AddUnverified("advisories: OSV query failed: " + err.Error())
	}
	for _, hit := range hits {
		var ids []string
		for _, advisory := range hit.Advisories {
			if advisory.ID != "" {
				ids = append(ids, advisory.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		report.Add(domain.Finding{
			ID:       "advisories.vulnerable",
			Severity: domain.SeverityError,
			Subject:  hit.Package.Name + "@" + hit.Package.Version,
			Actual:   strings.Join(ids, ", "),
			Expected: "no known advisories",
			Fix:      fmt.Sprintf("upgrade %s past %s", hit.Package.Name, ids[0]),
		})
	}
}

// resolvedPackages collects the lockfile versions of every cloned repository.
func (s *Service) resolvedPackages() []domain.PackageRef {
	repos, _ := s.workspaceRepos()
	seen := map[string]bool{}
	var out []domain.PackageRef
	add := func(name, version, ecosystem string) {
		name = strings.TrimSpace(name)
		version = strings.TrimSpace(version)
		if name == "" || version == "" || strings.HasPrefix(name, "@manovaspace/") {
			return
		}
		if strings.HasPrefix(version, "workspace:") || strings.HasPrefix(version, "file:") || strings.HasPrefix(version, "link:") {
			return
		}
		key := ecosystem + "\x00" + name + "\x00" + version
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, domain.PackageRef{Name: name, Version: version, Ecosystem: ecosystem})
	}
	for _, repo := range repos {
		if !s.Files.Exists(repo.Dir) {
			continue
		}
		if data, err := s.Files.ReadFile(filepath.Join(repo.Dir, "bun.lock")); err == nil {
			for _, resolved := range domain.ParseBunLockAll(string(data)) {
				add(resolved.Name, resolved.Version, "npm")
			}
		}
		if data, err := s.Files.ReadFile(filepath.Join(repo.Dir, "go.sum")); err == nil {
			for _, resolved := range domain.ParseGoSum(string(data)) {
				add(resolved.Name, resolved.Version, "Go")
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ecosystem != out[j].Ecosystem {
			return out[i].Ecosystem < out[j].Ecosystem
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// addActionsUsage reports GitHub Actions billing for each workspace owner.
func (s *Service) addActionsUsage(ctx context.Context, report *domain.Report, repos []Repo) {
	if s.GitHub == nil {
		report.AddUnverified("actions minutes: GitHub wiring missing")
		return
	}
	var owners []string
	seen := map[string]bool{}
	for _, repo := range repos {
		slug, ok := domain.GitHubSlug(repo.RemoteURL)
		if !ok {
			continue
		}
		owner := strings.SplitN(slug, "/", 2)[0]
		if owner == "" || seen[owner] {
			continue
		}
		seen[owner] = true
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		usage, err := s.GitHub.ActionsUsage(ctx, owner)
		if err != nil {
			report.AddUnverified("actions minutes for " + owner + ": " + err.Error())
			continue
		}
		severity := domain.SeverityInfo
		expected := "within the plan's included minutes (not reported)"
		if usage.IncludedMinutes > 0 {
			expected = fmt.Sprintf("%d included", usage.IncludedMinutes)
			if usage.TotalMinutesUsed > usage.IncludedMinutes {
				severity = domain.SeverityWarn
			}
		}
		report.Add(domain.Finding{
			ID:       "tests.actions-minutes",
			Severity: severity,
			Subject:  owner,
			Actual:   fmt.Sprintf("%d minutes used", usage.TotalMinutesUsed),
			Expected: expected,
			Fix:      "review Actions usage on the GitHub billing page",
		})
	}
}

func (s *Service) latestUpstream(ctx context.Context, tool string) (string, error) {
	switch tool {
	case "go":
		if s.GitHub == nil {
			return "", fmt.Errorf("GitHub wiring missing")
		}
		return s.GitHub.Releases(ctx, "golang", "go")
	case "node":
		if s.GitHub == nil {
			return "", fmt.Errorf("GitHub wiring missing")
		}
		return s.GitHub.Releases(ctx, "nodejs", "node")
	case "bun", "next", "react":
		if s.Registry == nil {
			return "", fmt.Errorf("registry wiring missing")
		}
		return s.Registry.Latest(ctx, tool)
	}
	return "", fmt.Errorf("unknown upstream tool %q", tool)
}

func (s *Service) rtkHookPresent() bool {
	if s.Files.Exists(filepath.Join(s.Root, filepath.FromSlash(agentRTKRule))) {
		return true
	}
	data, err := s.Files.ReadFile(filepath.Join(s.Root, filepath.FromSlash(agentInstallArtifact)))
	return err == nil && strings.Contains(string(data), "rtk")
}

// addRetiredNameFindings warns about retired names still mentioned by active
// handbook docs. Archived, planning and review folders are history, not
// guidance, and an ADR may keep naming the retired forge.
func (s *Service) addRetiredNameFindings(report *domain.Report) {
	for _, dir := range []string{"handbook/docs", "handbook/agents"} {
		root := filepath.Join(s.Root, filepath.FromSlash(dir))
		if !s.Files.Exists(root) {
			continue
		}
		err := s.Files.WalkAll(root, func(path string) error {
			relative, err := filepath.Rel(s.Root, path)
			if err != nil {
				return nil
			}
			relative = filepath.ToSlash(relative)
			if !domain.ActiveDocPath(relative) {
				return nil
			}
			data, err := s.Files.ReadFile(path)
			if err != nil {
				return nil
			}
			for _, hit := range domain.RetiredHits(relative, data) {
				report.Add(domain.Finding{
					ID:       "agents.retired-name",
					Severity: domain.SeverityWarn,
					Subject:  fmt.Sprintf("%s:%d", hit.File, hit.Line),
					Actual:   hit.Text,
					Expected: hit.Expected,
					Fix:      hit.Fix,
				})
			}
			return nil
		})
		if err != nil {
			report.AddUnverified(dir + ": cannot walk docs (retired names unverified)")
		}
	}
}

func (s *Service) workspaceRepos() ([]Repo, []string) {
	if s.Workspace == nil {
		return nil, []string{"audit: workspace wiring missing"}
	}
	repos, err := s.Workspace.Repos(s.Root, s.ManifestPath)
	if err != nil {
		return nil, []string{"workspace manifest: " + err.Error()}
	}
	var out []Repo
	for _, repo := range repos {
		repo.Path = strings.ReplaceAll(repo.Path, "\\", "/")
		if domain.SkipPath(repo.Path) {
			continue
		}
		out = append(out, repo)
	}
	return out, nil
}

func addUnverifiedAll(report *domain.Report, messages []string) {
	for _, message := range messages {
		report.AddUnverified(message)
	}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func normalizeUpstreamTag(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "go")
	tag = strings.TrimPrefix(tag, "v")
	return tag
}

func pinKey(tool string) string {
	switch tool {
	case "go":
		return "GO_VERSION"
	case "bun":
		return "BUN_VERSION"
	case "node":
		return "NODE_VERSION"
	case "next":
		return "NEXT_VERSION"
	case "react":
		return "REACT_VERSION"
	}
	return strings.ToUpper(tool) + "_VERSION"
}

func commandDetail(res CommandResult) string {
	detail := strings.TrimSpace(res.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(res.Stdout)
	}
	if detail == "" && res.ExitCode != 0 {
		detail = fmt.Sprintf("exit code %d", res.ExitCode)
	}
	return detail
}
