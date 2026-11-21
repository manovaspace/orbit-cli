package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/output"
	portsapp "github.com/manovaspace/orbit-cli/pkg/ports/application"
	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
	"github.com/manovaspace/orbit-cli/pkg/ports/infrastructure/netprobe"
	portregistry "github.com/manovaspace/orbit-cli/pkg/ports/infrastructure/registry"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

// portProbe is the seam through which the port commands probe TCP listeners.
var portProbe portsapp.Prober = netprobe.New()

func newPortService(out io.Writer) *portsapp.Service {
	return &portsapp.Service{
		Ports: portProbe,
		Out:   out,
	}
}

// loadPortRegistry reads handbook/agents/workspace/ports.yaml under root (the
// workspace root when root is empty). A missing registry returns nil with no
// error so the commands keep their pre-registry behaviour; a malformed one is
// a failure.
func loadPortRegistry(root string) (*portsdomain.Registry, string, error) {
	if root == "" {
		root = findWorkspaceRoot("")
	}
	path := filepath.Join(root, filepath.FromSlash(portregistry.RelPath))
	reg, err := portregistry.Find(root)
	if errors.Is(err, portregistry.ErrNotFound) {
		return nil, path, nil
	}
	if err != nil {
		return nil, path, clierr.Wrap(err, clierr.Failed("port registry unreadable"))
	}
	return reg, path, nil
}

func newPortCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use: "port",
		// Args rejects an unknown subcommand as a usage error (exit 3). Cobra
		// skips argument checks on a command without RunE, so RunE is needed.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonFlag {
				return emitPortFailure(cmd.OutOrStdout(), true, clierr.Usage("port needs a subcommand: list, allocate or check"))
			}
			return cmd.Help()
		},
		Short: "Inspect, allocate and check dev ports against the port registry",
		Long:  "Read the dev port registry (handbook/agents/workspace/ports.yaml, ADR-058) and enforce it. list shows registered services with a listener probe; allocate suggests a free port that no registry entry claims; check scans repositories for unregistered, retired, misplaced and open-bind ports. Without a registry, list shows the 50-port project blocks and allocate uses the loopback bind probe alone. Suggestions are not durable reservations.",
	}
	cmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Print usage errors as a JSON envelope")

	cmd.AddCommand(newPortListCmd())
	cmd.AddCommand(newPortAllocateCmd())
	cmd.AddCommand(newPortCheckCmd())

	return cmd
}

func newPortListCmd() *cobra.Command {
	var (
		scanNetwork bool
		pageFlag    int
		limitFlag   int
		jsonFlag    bool
		rootFlag    string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered services (ports.yaml) or base ports for all projects",
		Long:  "With a handbook/agents/workspace/ports.yaml registry, list every registered service by port with a live listener probe. Without one, list the 50-port project blocks. --limit and --page page projects, or services when a registry is found.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			reg, registryPath, err := loadPortRegistry(rootFlag)
			if err != nil {
				return emitPortFailure(out, jsonFlag, err)
			}
			service := newPortService(io.Discard)
			service.Registry = reg
			opts := portsapp.ListOptions{Scan: scanNetwork, Page: pageFlag, Limit: limitFlag}

			if reg != nil {
				result := service.ListServices(cmd.Context(), opts)
				return render.Emit(out, jsonFlag, result, func(p *render.Printer) error {
					renderServiceList(p, result, registryPath)
					return nil
				})
			}

			result, err := service.List(cmd.Context(), opts)
			if err != nil {
				return emitPortFailure(out, jsonFlag, err)
			}
			if jsonFlag {
				return output.Write(out, output.Success(result))
			}

			p := render.NewPrinter(out)
			renderPortList(p, result, limitFlag)
			return p.Err()
		},
	}

	cmd.Flags().BoolVar(&scanNetwork, "scan", false, "Momentarily attempt loopback binds for every block slot (no effect with a registry, which always probes)")
	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum projects (or services, with a registry) per page (0 = all)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	cmd.Flags().StringVar(&rootFlag, "root", "", "workspace root holding handbook/agents/workspace/ports.yaml (default: the workspace containing the current directory)")
	return cmd
}

// emitPortFailure writes the JSON failure envelope when requested and returns err.
func emitPortFailure(out io.Writer, jsonFlag bool, err error) error {
	return render.EmitFailure(out, jsonFlag, err)
}

func newPortAllocateCmd() *cobra.Command {
	var (
		jsonFlag bool
		rootFlag string
	)

	cmd := &cobra.Command{
		Use:     "allocate <project> <service>",
		Aliases: []string{"alloc"},
		Short:   "Suggest a free dynamic port that no registry entry claims; not reserved",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			reg, _, err := loadPortRegistry(rootFlag)
			if err != nil {
				return emitPortFailure(out, jsonFlag, err)
			}
			service := newPortService(io.Discard)
			service.Registry = reg
			result, err := service.Allocate(cmd.Context(), portsapp.AllocateInput{
				Project: args[0],
				Service: args[1],
			})
			if err != nil {
				return emitPortFailure(out, jsonFlag, err)
			}
			if jsonFlag {
				return output.Write(out, output.Success(result))
			}

			p := render.NewPrinter(out)
			renderPortAllocate(p, result)
			return p.Err()
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	cmd.Flags().StringVar(&rootFlag, "root", "", "workspace root holding handbook/agents/workspace/ports.yaml (default: the workspace containing the current directory)")
	return cmd
}

// renderPortList writes the project summary table and per-project slot groups.
func renderPortList(p *render.Printer, result portsapp.ListResult, limit int) {
	p.Title("Port allocations")
	p.KV("Projects", fmt.Sprintf("%d", result.TotalProjects))
	if result.Scanned {
		p.KV("Scan", "on")
	} else {
		p.KV("Scan", "off")
	}
	if limit > 0 {
		p.KV("Showing", portShowing(result))
	}

	if len(result.Projects) == 0 {
		p.Empty(render.Plural(0, "project", "projects"))
	} else {
		tbl := table.New(
			table.Column{Title: "PROJECT", HeaderStyle: render.Header, CellStyle: render.Bold, MinWidth: 20},
			table.Column{Title: "BASE", HeaderStyle: render.Header, MinWidth: 8},
			table.Column{Title: "RANGE", HeaderStyle: render.Header, MinWidth: 14},
			table.Column{Title: "STATUS", HeaderStyle: render.Header, MinWidth: 16},
		)
		for _, project := range result.Projects {
			var status string
			switch {
			case !project.Scanned:
				status = render.StatusUnverified.Icon() + " unverified"
			case project.Active == 0:
				status = render.StatusOK.Icon() + " bindable"
			default:
				status = fmt.Sprintf("%s %d in use", render.StatusWarn.Icon(), project.Active)
			}
			tbl.AddRow(project.Name, fmt.Sprintf("%d", project.Base), fmt.Sprintf("%d-%d", project.Start, project.End), status)
		}
		_ = p.Table(tbl)

		for _, project := range result.Projects {
			p.Section(fmt.Sprintf("Project %d: %s", project.ID, project.Name))

			baseStatus := render.StatusUnverified
			active := "unverified"
			if project.Scanned {
				if project.Active == 0 {
					baseStatus = render.StatusOK
				} else {
					baseStatus = render.StatusWarn
				}
				active = fmt.Sprintf("%d unavailable at probe", project.Active)
			}
			p.Line(baseStatus, fmt.Sprintf("base %d · range %d-%d (50 ports) · %s", project.Base, project.Start, project.End, active))

			for _, slot := range project.Slots {
				status := render.StatusUnverified
				suffix := ""
				if project.Scanned {
					if slot.Available {
						status = render.StatusOK
					} else {
						status = render.StatusWarn
						suffix = "  in use"
					}
				}
				p.Line(status, fmt.Sprintf("+%d  %d  %s%s", slot.Index, slot.Port, slot.Name, suffix))
			}

			dynamicStart := project.Base + portsdomain.DynamicSlotStart
			dynamicEnd := project.Base + portsdomain.DynamicSlotEnd
			if project.Scanned {
				status := render.StatusOK
				if project.DynamicUsed > 0 {
					status = render.StatusWarn
				}
				p.Line(status, fmt.Sprintf("dynamic %d-%d  %d bindable, %d in use", dynamicStart, dynamicEnd, project.DynamicFree, project.DynamicUsed))
			} else {
				p.Line(render.StatusUnverified, fmt.Sprintf("dynamic %d-%d  unverified", dynamicStart, dynamicEnd))
			}
		}
	}

	bindable, unavailable, unverified := 0, 0, 0
	for _, project := range result.Projects {
		for _, slot := range project.Slots {
			switch {
			case !project.Scanned:
				unverified++
			case slot.Available:
				bindable++
			default:
				unavailable++
			}
		}
	}
	p.Summary(
		render.SummaryPart{Status: render.StatusOK, Count: bindable, Label: "bindable"},
		render.SummaryPart{Status: render.StatusWarn, Count: unavailable, Label: "unavailable"},
		render.SummaryPart{Status: render.StatusUnverified, Count: unverified, Label: "unverified"},
	)
}

// portShowing reproduces the service's paging line from the result fields.
func portShowing(result portsapp.ListResult) string {
	switch {
	case result.TotalProjects == 0:
		return "0 of 0 projects"
	case len(result.Projects) == 0:
		return fmt.Sprintf("page %d of %d (no projects on this page, total: %d)", result.Page, result.TotalPages, result.TotalProjects)
	default:
		return fmt.Sprintf("showing %d-%d of %d (page %d/%d)", result.Start+1, result.End, result.TotalProjects, result.Page, result.TotalPages)
	}
}

// renderPortAllocate writes the suggestion the service used to print.
func renderPortAllocate(p *render.Printer, result portsapp.AllocateResult) {
	p.Title("Port allocation")
	p.KVs(
		[2]string{"Project", fmt.Sprintf("%s (id %d)", result.Project, result.ProjectID)},
		[2]string{"Service", result.Service},
		[2]string{"Port", fmt.Sprintf("%d", result.Port)},
		[2]string{"Slot", fmt.Sprintf("+%d (dynamic)", result.Slot)},
		[2]string{"Export", result.EnvVariable},
		[2]string{"Reserved", "no"},
	)
	p.Line(render.StatusInfo, "not reserved; the port may be taken at bind time")
	p.Hint("add it to handbook/agents/workspace/ports.yaml before the service binds it")

	reserved := 0
	if result.Reserved {
		reserved = 1
	}
	p.Summary(
		render.SummaryPart{Status: render.StatusInfo, Count: 1, Label: "suggested"},
		render.SummaryPart{Status: render.StatusSkipped, Count: reserved, Label: "reserved"},
	)
}

// renderServiceList writes the registered services table and a summary of how
// many listen right now.
func renderServiceList(p *render.Printer, result portsapp.ServiceListResult, registryPath string) {
	p.Title("Port registry")
	p.KV("Registry", registryPath)
	p.KV("Services", fmt.Sprintf("%d", result.TotalServices))
	if result.Limit > 0 {
		p.KV("Showing", fmt.Sprintf("showing %d-%d of %d (page %d/%d)", result.Start+1, result.End, result.TotalServices, result.Page, result.TotalPages))
	}

	if len(result.Services) == 0 {
		p.Empty(render.Plural(0, "service", "services"))
		p.Blank()
		return
	}

	tbl := table.New(
		table.Column{Title: "PORT", HeaderStyle: render.Header, MinWidth: 6},
		table.Column{Title: "PROJECT", HeaderStyle: render.Header, MinWidth: 14},
		table.Column{Title: "SERVICE", HeaderStyle: render.Header, CellStyle: render.Bold, MinWidth: 24},
		table.Column{Title: "RUNS", HeaderStyle: render.Header, MinWidth: 6},
		table.Column{Title: "LISTENING", HeaderStyle: render.Header, MinWidth: 12},
		table.Column{Title: "REPOS", HeaderStyle: render.Header, MinWidth: 20, Flexible: true},
	)
	listening := 0
	for _, svc := range result.Services {
		state := render.StatusOK.Icon() + " free"
		if svc.Listening {
			state = render.StatusWarn.Icon() + " listening"
			listening++
		}
		repos := ""
		for i, r := range svc.Repos {
			if i > 0 {
				repos += ", "
			}
			repos += r
		}
		tbl.AddRow(fmt.Sprintf("%d", svc.Port), svc.Project, svc.Service, svc.Runs, state, repos)
	}
	_ = p.Table(tbl)

	p.Summary(
		render.SummaryPart{Status: render.StatusInfo, Count: len(result.Services), Label: "registered"},
		render.SummaryPart{Status: render.StatusWarn, Count: listening, Label: "listening"},
	)
}
