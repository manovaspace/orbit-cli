package main

import (
	"fmt"

	"github.com/manovaspace/orbit-cli/pkg/host"
	"github.com/spf13/cobra"
)

func enforceHost(cmd *cobra.Command, live func() host.Report) error {
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
