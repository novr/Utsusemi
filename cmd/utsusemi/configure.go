package main

import (
	"github.com/spf13/cobra"
)

func newConfigureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure credentials and config file",
	}
	cmd.AddCommand(newConfigureAppCmd())
	cmd.AddCommand(newConfigureTokenCmd())
	cmd.AddCommand(newConfigureAlertsCmd())
	cmd.AddCommand(newConfigureEditCmd())
	cmd.AddCommand(newConfigurePathCmd())
	cmd.AddCommand(newConfigureShowCmd())
	return cmd
}
