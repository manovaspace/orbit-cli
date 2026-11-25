package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newInitCmd keeps `o init` working as a hidden alias of `o setup`. It takes the same flags and
// prints a notice on stderr before it runs setup.
func newInitCmd() *cobra.Command {
	cmd := newSetupCmd()
	cmd.Use = "init"
	cmd.Hidden = true
	setupRun := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		fmt.Fprintln(c.ErrOrStderr(), "o init is now o setup")
		return setupRun(c, args)
	}
	return cmd
}
