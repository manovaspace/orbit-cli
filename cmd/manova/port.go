package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"git.dev.manova.space/manova/orbit-cli/pkg/ports"
	"github.com/spf13/cobra"
)

var defaultSlotNames = map[int]string{
	0: "Base / Database",
	1: "Core API / Backend",
	2: "Frontend / Web",
	3: "Admin / Dashboard",
	4: "Metrics / Telemetry",
	5: "Health / Status",
	6: "Queue / Broker",
	7: "Ingress / Gateway",
	8: "Auth / Identity",
	9: "Aux / Worker",
}

func newPortCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "port",
		Short: "Manage and inspect the hybrid 50-port allocation model",
		Long:  "Inspect project port ranges (50-port blocks), deterministic service slots (0-9), and dynamically allocate ports (10-49).",
	}

	cmd.AddCommand(newPortListCmd())
	cmd.AddCommand(newPortAllocateCmd())

	return cmd
}

func newPortListCmd() *cobra.Command {
	var scanNetwork bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List base ports and deterministic slots for all registered projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, titleStyle.Render("Manova Port Manager — 50-Port Block Allocations"))

			type projEntry struct {
				name string
				id   int
			}

			var entries []projEntry
			for name, id := range ports.DefaultProjectMapping {
				entries = append(entries, projEntry{name: name, id: id})
			}
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].id < entries[j].id
			})

			for _, p := range entries {
				base := ports.BasePort(p.id)
				start, end := ports.GetProjectRange(p.id)

				// Scan ports
				scanResults, _ := ports.ScanProjectPorts(p.id)
				activeCount := 0
				for _, inUse := range scanResults {
					if inUse {
						activeCount++
					}
				}

				header := fmt.Sprintf("Project %d: %s  [%d - %d]", p.id, p.name, start, end)
				fmt.Fprintf(out, "\n%s\n", headerStyle.Render("── "+header+" ──────────────────────────────────"))
				fmt.Fprintf(out, "  Base Port: %s  |  Total Range: %s  |  Active: %s\n\n",
					codeStyle.Render(strconv.Itoa(base)),
					subtleStyle.Render(fmt.Sprintf("%d-%d (50 ports)", start, end)),
					boldStyle.Render(fmt.Sprintf("%d in use", activeCount)),
				)

				// Deterministic slots (0-9)
				fmt.Fprintln(out, boldStyle.Render("  Deterministic Slots (0-9):"))
				for slot := 0; slot < 10; slot++ {
					port := base + slot
					inUse := scanResults[port]

					var statusBadge string
					if inUse {
						statusBadge = warningStyle.Render("[IN USE]")
					} else {
						statusBadge = successStyle.Render("[FREE]")
					}

					slotName := defaultSlotNames[slot]
					fmt.Fprintf(out, "    Slot +%d: %s  %s  %s\n",
						slot,
						codeStyle.Render(strconv.Itoa(port)),
						statusBadge,
						subtleStyle.Render(slotName),
					)
				}

				// Dynamic slots (10-49)
				dynamicInUse := 0
				for slot := 10; slot < 50; slot++ {
					if scanResults[base+slot] {
						dynamicInUse++
					}
				}
				dynamicFree := 40 - dynamicInUse

				fmt.Fprintf(out, "\n  %s %s (%d free, %d in use)\n",
					boldStyle.Render("Dynamic Slots (10-49):"),
					subtleStyle.Render(fmt.Sprintf("Ports %d-%d", base+10, base+49)),
					dynamicFree,
					dynamicInUse,
				)
			}

			fmt.Fprintln(out)
			return nil
		},
	}

	cmd.Flags().BoolVar(&scanNetwork, "scan", true, "Scan active network sockets on loopback")
	return cmd
}

func newPortAllocateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allocate <project> <service>",
		Short: "Dynamically allocate the next free port in a project's 50-port range",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectName := args[0]
			serviceName := args[1]
			out := cmd.OutOrStdout()

			projectID, ok := ports.ResolveProjectID(projectName)
			if !ok {
				// Try parsing as integer
				if id, err := strconv.Atoi(projectName); err == nil && id >= 0 {
					projectID = id
					ok = true
				}
			}

			if !ok {
				var validProjects []string
				for k := range ports.DefaultProjectMapping {
					validProjects = append(validProjects, k)
				}
				sort.Strings(validProjects)
				return fmt.Errorf("unknown project %q. Valid projects: %s (or enter project ID number)", projectName, strings.Join(validProjects, ", "))
			}

			// Scan project ports to find in-use ports
			scanResults, err := ports.ScanProjectPorts(projectID)
			if err != nil {
				return fmt.Errorf("failed to scan project ports: %w", err)
			}

			var inUse []int
			for p, used := range scanResults {
				if used {
					inUse = append(inUse, p)
				}
			}

			allocatedPort, err := ports.AllocateDynamic(projectID, inUse)
			if err != nil {
				return fmt.Errorf("port allocation failed: %w", err)
			}

			slot := allocatedPort - ports.BasePort(projectID)

			fmt.Fprintln(out, titleStyle.Render("Port Allocation Successful"))
			fmt.Fprintf(out, "  %s  Allocated port %s for service %s\n",
				iconOK,
				successStyle.Render(strconv.Itoa(allocatedPort)),
				boldStyle.Render(serviceName),
			)
			fmt.Fprintf(out, "  Project:     %s (ID: %d)\n", boldStyle.Render(projectName), projectID)
			fmt.Fprintf(out, "  Slot:        +%d (Dynamic)\n", slot)
			fmt.Fprintf(out, "  Export:      %s\n",
				codeStyle.Render(fmt.Sprintf("export %s_PORT=%d", strings.ToUpper(strings.ReplaceAll(serviceName, "-", "_")), allocatedPort)),
			)

			return nil
		},
	}

	return cmd
}
