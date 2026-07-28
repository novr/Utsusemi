package main

import (
	"fmt"

	"github.com/novr/utsusemi/internal/status"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local operational status",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadConfigRuntime(cmd.Context())
			if err != nil {
				return err
			}
			report, err := status.Collect(cmd.Context(), rt.statusInput(nil))
			if err != nil {
				return err
			}
			if jsonOut {
				report.ConfigPath = configPath
				return writeJSON(report)
			}
			fmt.Print(status.FormatText(report))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	return cmd
}
