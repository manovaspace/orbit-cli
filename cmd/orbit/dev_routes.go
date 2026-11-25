package main

import (
	"fmt"
	"io"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/dev/application"
	"github.com/manovaspace/orbit-cli/pkg/dev/domain"
	devosfs "github.com/manovaspace/orbit-cli/pkg/dev/infrastructure/osfs"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

// devTextFiles reads and writes the generated Caddyfile.
var devTextFiles application.TextFiles = devosfs.New()

func newDevRoutesCmd() *cobra.Command {
	var (
		writePath string
		listFlag  bool
		jsonFlag  bool
		rootFlag  string
	)

	cmd := &cobra.Command{
		Use:   "routes",
		Short: "Generate the Caddyfile that serves native dev processes at <name>.localhost",
		Long: "Route every registry service with runs: mac and a localhost name (ports.yaml) to http://<name>.localhost " +
			"through Caddy, a reverse proxy to 127.0.0.1:<port>. The default prints the Caddyfile on stdout; --write writes " +
			"it to a path, --list prints a table, and --json prints the list as a JSON envelope.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			reg, registryPath, err := loadPortRegistry(rootFlag)
			if err != nil {
				return render.EmitFailure(out, jsonFlag, err)
			}
			if reg == nil {
				return render.EmitFailure(out, jsonFlag, clierr.Unverified(fmt.Sprintf("no port registry at %s; nothing to route", registryPath)))
			}
			if writePath != "" && (listFlag || jsonFlag) {
				return render.EmitFailure(out, jsonFlag, clierr.Usage("--write cannot be combined with --list or --json"))
			}

			routes, err := application.RoutesFor(reg)
			if err != nil {
				return render.EmitFailure(out, jsonFlag, err)
			}

			switch {
			case listFlag || jsonFlag:
				return render.Emit(out, jsonFlag, routes, func(p *render.Printer) error {
					renderDevRoutes(p, routes)
					return nil
				})
			case writePath != "":
				svc := &application.RouteService{Files: devTextFiles}
				if err := svc.WriteCaddyfile(writePath, routes); err != nil {
					return err
				}
				_, err := io.WriteString(out, writePath+"\n")
				return err
			default:
				svc := &application.RouteService{Files: devTextFiles}
				_, err := io.WriteString(out, svc.Caddyfile(routes))
				return err
			}
		},
	}

	cmd.Flags().StringVar(&writePath, "write", "", "write the Caddyfile to this path (parent directories are created) and print the path")
	cmd.Flags().BoolVar(&listFlag, "list", false, "print the routes as a table (NAME, URL, PORT, SERVICE) instead of the Caddyfile")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "print the routes as a JSON envelope")
	cmd.Flags().StringVar(&rootFlag, "root", "", "workspace root holding handbook/agents/workspace/ports.yaml (default: the workspace containing the current directory)")
	return cmd
}

// renderDevRoutes writes the route table and a count.
func renderDevRoutes(p *render.Printer, routes []domain.Route) {
	p.Title("Dev routes")
	if len(routes) == 0 {
		p.Empty(render.Plural(0, "route", "routes"))
		p.Blank()
		return
	}

	tbl := table.New(
		table.Column{Title: "NAME", HeaderStyle: render.Header, CellStyle: render.Bold, MinWidth: 16},
		table.Column{Title: "URL", HeaderStyle: render.Header, MinWidth: 24},
		table.Column{Title: "PORT", HeaderStyle: render.Header, MinWidth: 6},
		table.Column{Title: "SERVICE", HeaderStyle: render.Header, MinWidth: 20, Flexible: true},
	)
	for _, r := range routes {
		tbl.AddRow(r.Name, r.URL, fmt.Sprintf("%d", r.Port), r.Service)
	}
	_ = p.Table(tbl)

	p.Summary(render.SummaryPart{Status: render.StatusInfo, Count: len(routes), Label: "routes"})
}
