package main

import (
	"io"

	"github.com/manovaspace/orbit-cli/pkg/output"
	portsapp "github.com/manovaspace/orbit-cli/pkg/ports/application"
	"github.com/manovaspace/orbit-cli/pkg/ports/infrastructure/netprobe"
	"github.com/spf13/cobra"
)

// portProbe is the seam through which the port commands probe TCP listeners.
var portProbe portsapp.Prober = netprobe.New()

func newPortService(out io.Writer) *portsapp.Service {
	return &portsapp.Service{
		Ports: portProbe,
		Out:   out,
		UI: portsapp.UI{
			Title:   func(text string) string { return titleStyle.Render(text) },
			Header:  func(text string) string { return headerStyle.Render(text) },
			Bold:    func(text string) string { return boldStyle.Render(text) },
			OK:      iconOK,
			Success: func(text string) string { return successStyle.Render(text) },
			Warning: func(text string) string { return warningStyle.Render(text) },
			Subtle:  func(text string) string { return subtleStyle.Render(text) },
			Code:    func(text string) string { return codeStyle.Render(text) },
			Info:    func(text string) string { return infoStyle.Render(text) },
		},
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
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			result, err := newPortService(uiOut).List(cmd.Context(), portsapp.ListOptions{
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
			return nil
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
			uiOut := io.Writer(out)
			if jsonFlag {
				uiOut = io.Discard
			}

			result, err := newPortService(uiOut).Allocate(cmd.Context(), portsapp.AllocateInput{
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
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}
