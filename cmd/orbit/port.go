package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/output"
	portsapp "github.com/manovaspace/orbit-cli/pkg/ports/application"
	portsdomain "github.com/manovaspace/orbit-cli/pkg/ports/domain"
	"github.com/manovaspace/orbit-cli/pkg/ports/infrastructure/netprobe"
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

func newPortCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "port",
		Short: "Manage and inspect the hybrid 50-port allocation model",
		Long:  "Inspect project port ranges (50-port blocks), deterministic service slots (0-9), and suggest ports (10-49) after a momentary IPv4 loopback bind probe. Suggestions are not durable reservations.",
	}

	cmd.AddCommand(newPortListCmd())
	cmd.AddCommand(newPortAllocateCmd())

	return cmd
}

func newPortListCmd() *cobra.Command {
	var (
		scanNetwork bool
		pageFlag    int
		limitFlag   int
		jsonFlag    bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List base ports and deterministic slots for all registered projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			result, err := newPortService(io.Discard).List(cmd.Context(), portsapp.ListOptions{
				Scan:  scanNetwork,
				Page:  pageFlag,
				Limit: limitFlag,
			})
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
			}
			if jsonFlag {
				return output.Write(out, output.Success(result))
			}

			p := render.NewPrinter(out)
			renderPortList(p, result, limitFlag)
			return p.Err()
		},
	}

	cmd.Flags().BoolVar(&scanNetwork, "scan", false, "Momentarily attempt IPv4 loopback binds; no reservation is retained")
	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum projects per page (0 = all)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func newPortAllocateCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "allocate <project> <service>",
		Short: "Suggest a bindable dynamic port; no reservation is retained",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			result, err := newPortService(io.Discard).Allocate(cmd.Context(), portsapp.AllocateInput{
				Project: args[0],
				Service: args[1],
			})
			if err != nil {
				if jsonFlag {
					_ = output.Write(out, output.Failure(err))
				}
				return err
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

	reserved := 0
	if result.Reserved {
		reserved = 1
	}
	p.Summary(
		render.SummaryPart{Status: render.StatusInfo, Count: 1, Label: "suggested"},
		render.SummaryPart{Status: render.StatusSkipped, Count: reserved, Label: "reserved"},
	)
}
