package main

import (
	"fmt"
	"sort"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	portsapp "github.com/manovaspace/orbit-cli/pkg/ports/application"
	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
	portscan "github.com/manovaspace/orbit-cli/pkg/ports/infrastructure/scan"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/spf13/cobra"
)

// portCheckSource lists and reads workspace files; command tests may replace it.
var portCheckSource portsapp.Source = portscan.New()

func newPortCheckCmd() *cobra.Command {
	var (
		jsonFlag   bool
		strictFlag bool
		rootFlag   string
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check repositories against the dev port registry (ports.yaml)",
		Long: "Scan compose files, package.json scripts, .env.example files, Go sources, Caddy files and TS/JS sources for ports 10000-17999 and report " +
			"numbers the registry does not list (UNREGISTERED), retired numbers (RETIRED), binds in a repository the entry does not list (WRONG_REPO), " +
			"binds of reserved slots (RESERVED) and compose publishes on all interfaces (OPEN_BIND, a warning unless --strict). " +
			"Exit codes: 0 no findings, 1 at least one finding, 2 the registry is missing or a file could not be read.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			reg, registryPath, err := loadPortRegistry(rootFlag)
			if err == nil && reg == nil {
				err = clierr.Unverified(fmt.Sprintf("no port registry at %s; nothing to check against", registryPath))
			}
			if err != nil {
				return emitPortFailure(out, jsonFlag, err)
			}

			root := rootFlag
			if root == "" {
				root = findWorkspaceRoot("")
			}
			result, err := portsapp.Checker{Source: portCheckSource}.Run(root, registryPath, reg, strictFlag)
			if err != nil {
				return emitPortFailure(out, jsonFlag, clierr.Wrap(err, clierr.Unverified("port check could not read the workspace")))
			}

			var failure error
			if result.Failing > 0 {
				failure = clierr.Failed(fmt.Sprintf("port check found %d problem(s)", result.Failing))
			}
			return render.EmitFindings(out, jsonFlag, result, failure, func(p *render.Printer) error {
				renderPortCheck(p, result)
				return nil
			})
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	cmd.Flags().BoolVar(&strictFlag, "strict", false, "Treat OPEN_BIND warnings as failures")
	cmd.Flags().StringVar(&rootFlag, "root", "", "workspace root holding handbook/agents/workspace/ports.yaml (default: the workspace containing the current directory)")
	return cmd
}

// renderPortCheck prints the findings grouped by repository, then the counts
// per kind.
func renderPortCheck(p *render.Printer, result portsapp.CheckResult) {
	p.Title("Port check")
	p.KV("Root", result.Root)
	p.KV("Registry", result.Registry)
	p.KV("Files", fmt.Sprintf("%d scanned, %d port occurrences", result.Files, result.Occurrences))

	groups := map[string][]portsdomain.Finding{}
	var repos []string
	for _, f := range result.Findings {
		repo := f.Repo
		if repo == "" {
			repo = "(no repository)"
		}
		if _, ok := groups[repo]; !ok {
			repos = append(repos, repo)
		}
		groups[repo] = append(groups[repo], f)
	}
	sort.Strings(repos)

	for _, repo := range repos {
		p.Section(repo)
		for _, f := range groups[repo] {
			status := render.StatusFail
			if f.Severity == portsdomain.SeverityWarning {
				status = render.StatusWarn
			}
			label := string(f.Kind)
			if f.Name != "" {
				label = fmt.Sprintf("%s %s", f.Kind, f.Name)
			}
			p.Line(status, fmt.Sprintf("%s:%d  %d  %s  %s", f.File, f.Line, f.Port, label, f.Message))
		}
	}

	parts := make([]render.SummaryPart, 0, len(portsdomain.AllKinds)+1)
	for _, kind := range portsdomain.AllKinds {
		status := render.StatusFail
		if kind == portsdomain.KindOpenBind {
			status = render.StatusWarn
		}
		parts = append(parts, render.SummaryPart{Status: status, Count: result.Counts[string(kind)], Label: string(kind)})
	}
	p.Summary(parts...)
	if result.Failing == 0 {
		p.Hint("no failing findings")
	}
}
