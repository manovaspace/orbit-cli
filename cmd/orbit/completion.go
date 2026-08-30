package main

import (
	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "completion",
		Short: "Generate zsh autocompletion",
	}
	zsh := &cobra.Command{
		Use:   "zsh",
		Short: "Generate the autocompletion script for zsh",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
		},
	}
	c.AddCommand(zsh)
	return c
}
