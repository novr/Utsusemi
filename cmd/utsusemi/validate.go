package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate config.yaml (next agent start)",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadValidatedRuntime(cmd.Context())
			if err != nil {
				return err
			}
			if err := printHostedCredentialStatus(rt.Config); err != nil {
				return err
			}
			fmt.Println("configuration is valid (config.yaml; takes effect on next agent start)")
			return nil
		},
	}
}
