package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadValidatedRuntime(cmd.Context())
			if err != nil {
				return err
			}
			if err := printHostedCredentialStatus(rt.cfg); err != nil {
				return err
			}
			fmt.Println("configuration is valid")
			return nil
		},
	}
}
