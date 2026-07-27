package main

import (
	"context"

	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the utsusemi agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			ag, err := buildAgent(cmd.Context())
			if err != nil {
				return err
			}
			return ag.Run(context.Background())
		},
	}
}
