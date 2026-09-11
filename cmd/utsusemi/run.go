package main

import (
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var logFile string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the utsusemi agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ag, err := buildAgent(cmd.Context(), cmd.Flags().Changed("log"), logFile)
			if err != nil {
				return err
			}
			return ag.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&logFile, "log", "", "append structured logs to file (default: {state_dir}/agent.log; use --log=/path for a custom file)")
	cmd.Flags().Lookup("log").NoOptDefVal = "-"
	return cmd
}
