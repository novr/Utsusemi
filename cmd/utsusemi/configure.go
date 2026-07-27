package main

import (
	"github.com/spf13/cobra"
)

func newConfigureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Write config and store credentials",
	}
	cmd.AddCommand(newConfigureAppCmd())
	cmd.AddCommand(newConfigureTokenCmd())
	return cmd
}
