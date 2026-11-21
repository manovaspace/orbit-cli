package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/spf13/cobra"
)

// serverOnlyPaths are command paths (below the root) that need a Linux server
// host. On macOS they are refused, including their subcommands.
var serverOnlyPaths = []string{
	"dev up",
	"dev down",
	"dev tier2",
	"dev caddy",
	"dev logs",
	"dev portal",
	"onboard",
	"staff",
	"invite",
	"admin",
	"migrate",
	"ci runner",
}

// commandSegments returns the names from the first subcommand below the root
// down to cmd, e.g. ["dev", "up"] for `orbit dev up`.
func commandSegments(cmd *cobra.Command) []string {
	var names []string
	for c := cmd; c != nil && c.Parent() != nil; c = c.Parent() {
		names = append([]string{c.Name()}, names...)
	}
	return names
}

// serverOnlyPath reports the server-only path cmd falls under, if any.
func serverOnlyPath(cmd *cobra.Command) (string, bool) {
	segs := commandSegments(cmd)
	for _, p := range serverOnlyPaths {
		want := strings.Fields(p)
		if len(segs) >= len(want) && strings.Join(segs[:len(want)], " ") == p {
			return p, true
		}
	}
	return "", false
}

func enforceHost(cmd *cobra.Command, live func() host.Report) error {
	if os.Getenv("ORBIT_TESTBED") == "1" || os.Getenv("ORBIT_SKIP_HOSTGATE") == "1" {
		return nil
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "version", "doctor", "uninstall":
			return nil
		}
	}
	report := live()
	if !report.OK {
		fmt.Fprint(cmd.ErrOrStderr(), host.Format(report))
		return fmt.Errorf("unsupported host")
	}
	if report.Platform == "darwin" {
		if _, ok := serverOnlyPath(cmd); ok {
			return clierr.Usage(fmt.Sprintf("orbit %s: server-only; not available on macOS", strings.Join(commandSegments(cmd), " ")))
		}
	}
	return nil
}
