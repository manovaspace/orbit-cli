package main

import (
	"fmt"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	localservices "github.com/manovaspace/orbit-cli/pkg/setup/parts/services"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

// serviceRow is one service in `o services --json`.
type serviceRow struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Port  int    `json:"port"`
	Owner string `json:"owner"`
	Error string `json:"error,omitempty"`
	Note  string `json:"note,omitempty"`
}

// servicesResult is the data of `o services --json`.
type servicesResult struct {
	Services []serviceRow `json:"services"`
}

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Start the local services this machine needs",
		Long:  "Start the native Postgres cluster and Mailpit that the selected repositories declare, creating the cluster and missing databases first. Services are left running; stop them with 'o down'. With services: external in machine.yaml, nothing is checked or started and the command exits 0.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			p := render.NewPrinter(cmd.OutOrStdout())
			s, err := newSetupState(ctx, true)
			if err != nil {
				return err
			}
			if s.Machine.Services == "external" {
				p.Line(render.StatusInfo, "services are external; nothing to start")
				return p.Err()
			}

			e := engine.Engine{Parts: []engine.Part{localservices.New()}}
			res, applyErr := e.Apply(ctx, s)
			for _, pr := range res.Parts {
				for _, action := range pr.Actions {
					p.Line(render.StatusOK, action)
				}
				for _, f := range pr.Findings {
					p.Line(findingStatus(f), f.Message)
				}
				if pr.Error != "" {
					p.Line(render.StatusFail, pr.Error)
				}
				if len(pr.Actions) == 0 && pr.Status == engine.StatusOK {
					p.Line(render.StatusOK, "services already running")
				}
			}
			if err := p.Err(); err != nil {
				return err
			}
			return applyErr
		},
	}
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the local services orbit started",
		Long:  "Stop Mailpit and the orbit-owned Postgres cluster. A server that orbit did not start, or an external one, is left alone.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			p := render.NewPrinter(cmd.OutOrStdout())
			s, err := newSetupState(ctx, true)
			if err != nil {
				return err
			}
			if s.Machine.Services == "external" {
				p.Line(render.StatusInfo, "services are external; nothing to stop")
				return p.Err()
			}

			stopped, err := localservices.Down(ctx, s)
			if err != nil {
				return err
			}
			if len(stopped) == 0 {
				p.Line(render.StatusInfo, "no orbit-owned services running")
			}
			for _, name := range stopped {
				p.Line(render.StatusOK, "stopped "+name)
			}
			return p.Err()
		},
	}
}

func newServicesCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "services",
		Short: "Show the state of the local services",
		Long:  "Show whether Postgres and Mailpit run, on which port, and whether orbit or another process owns the listener. Reads only; nothing is started or stopped.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := newSetupState(ctx, true)
			if err != nil {
				return clierr.Wrap(err, clierr.Failed("cannot load the machine file"))
			}
			rows, err := localservices.Services(ctx, s)
			if err != nil {
				return err
			}

			result := servicesResult{Services: make([]serviceRow, 0, len(rows))}
			for _, r := range rows {
				row := serviceRow{Name: r.Name, State: "stopped", Port: r.Status.Port, Owner: r.Status.Owner}
				switch {
				case r.Err != nil:
					row.State = "unknown"
					row.Error = r.Err.Error()
				case r.Status.Running:
					row.State = "running"
				}
				if r.Note != nil {
					row.Note = r.Note.Error()
				}
				result.Services = append(result.Services, row)
			}

			return render.Emit(cmd.OutOrStdout(), jsonFlag, result, func(p *render.Printer) error {
				tbl := table.New(
					table.Column{Title: "NAME", HeaderStyle: render.Header, CellStyle: render.Bold, MinWidth: 10},
					table.Column{Title: "STATE", HeaderStyle: render.Header, MinWidth: 8},
					table.Column{Title: "PORT", HeaderStyle: render.Header, MinWidth: 6},
					table.Column{Title: "OWNER", HeaderStyle: render.Header, MinWidth: 8, Flexible: true},
				)
				for _, row := range result.Services {
					owner := row.Owner
					if owner == "" {
						owner = "-"
					}
					tbl.AddRow(row.Name, row.State, fmt.Sprintf("%d", row.Port), owner)
				}
				if err := p.Table(tbl); err != nil {
					return err
				}
				for _, row := range result.Services {
					if row.Error != "" {
						p.Line(render.StatusFail, row.Name+": "+row.Error)
					}
					if row.Note != "" {
						p.Line(render.StatusWarn, row.Name+": "+row.Note)
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

// findingStatus maps a finding severity to a printer status.
func findingStatus(f engine.Finding) render.Status {
	if f.Severity == engine.SeverityError {
		return render.StatusFail
	}
	return render.StatusWarn
}
