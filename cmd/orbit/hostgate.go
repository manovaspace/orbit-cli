package main

import (
	"fmt"
	"os"

	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/spf13/cobra"
)

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
	if report.OK {
		return nil
	}
	fmt.Fprint(cmd.ErrOrStderr(), host.Format(report))
	return fmt.Errorf("unsupported host")
}
