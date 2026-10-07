package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/ports"
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
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List base ports and deterministic slots for all registered projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, titleStyle.Render("Orbit Port Manager — 50-Port Block Allocations"))

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

			totalProjects := len(entries)
			displayEntries := entries
			var startIdx, endIdx, totalPages int

			if limitFlag > 0 {
				page := pageFlag
				if page < 1 {
					page = 1
				}
				totalPages = (totalProjects + limitFlag - 1) / limitFlag
				startIdx = (page - 1) * limitFlag
				if startIdx < totalProjects {
					endIdx = startIdx + limitFlag
					if endIdx > totalProjects {
						endIdx = totalProjects
					}
					displayEntries = entries[startIdx:endIdx]
				} else {
					displayEntries = nil
					startIdx = totalProjects
					endIdx = totalProjects
				}
			}

			for _, p := range displayEntries {
				base := ports.BasePort(p.id)
				start, end := ports.GetProjectRange(p.id)

				// Scan ports
				var scanResults map[int]bool
				if scanNetwork {
					var err error
					scanResults, err = ports.ScanProjectPorts(p.id)
					if err != nil {
						return err
					}
				}
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
					boldStyle.Render(func() string {
						if !scanNetwork {
							return "UNVERIFIED"
						}
						return fmt.Sprintf("%d unavailable at probe time", activeCount)
					}()),
				)

				// Deterministic slots (0-9)
				fmt.Fprintln(out, boldStyle.Render("  Deterministic Slots (0-9):"))
				for slot := 0; slot < 10; slot++ {
					port := base + slot
					inUse := scanResults[port]

					var statusBadge string
					if !scanNetwork {
						statusBadge = subtleStyle.Render("[UNVERIFIED]")
					} else if inUse {
						statusBadge = warningStyle.Render("[UNAVAILABLE AT PROBE]")
					} else {
						statusBadge = successStyle.Render("[BINDABLE AT PROBE]")
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

				if scanNetwork {
					fmt.Fprintf(out, "\n  %s %s (%d bindable, %d unavailable at probe time)\n",
						boldStyle.Render("Dynamic Slots (10-49):"),
						subtleStyle.Render(fmt.Sprintf("Ports %d-%d", base+10, base+49)),
						dynamicFree,
						dynamicInUse,
					)
				} else {
					fmt.Fprintf(out, "\n  Dynamic Slots (10-49): Ports %d-%d [UNVERIFIED]\n", base+10, base+49)
				}
			}

			if limitFlag > 0 {
				var info string
				if totalProjects == 0 {
					info = "Showing 0 of 0 projects"
				} else if len(displayEntries) == 0 {
					info = fmt.Sprintf("Page %d of %d (no projects on this page, total: %d)", pageFlag, totalPages, totalProjects)
				} else {
					info = fmt.Sprintf("Showing %d-%d of %d projects (Page %d/%d)", startIdx+1, endIdx, totalProjects, pageFlag, totalPages)
				}
				fmt.Fprintf(out, "\n  %s\n", subtleStyle.Render(info))
			}

			fmt.Fprintln(out)
			return nil
		},
	}

	cmd.Flags().BoolVar(&scanNetwork, "scan", false, "Momentarily attempt IPv4 loopback binds; no reservation is retained")
	cmd.Flags().IntVar(&pageFlag, "page", 1, "page number to display")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "maximum projects per page (0 = all)")
	return cmd
}

func newPortAllocateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allocate <project> <service>",
		Short: "Suggest a bindable dynamic port; no reservation is retained",
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

			fmt.Fprintln(out, titleStyle.Render("Port Assignment Suggestion (not reserved)"))
			fmt.Fprintf(out, "  %s  Suggested port %s for service %s\n",
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
