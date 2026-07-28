package main

import (
	"fmt"

	"github.com/novr/utsusemi/internal/listing"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list [vms|runners]",
		Short: "List managed VMs and/or GitHub runners",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := listing.ScopeAll
			if len(args) > 0 {
				scope = args[0]
				valid := false
				for _, choice := range listing.TargetChoices() {
					if scope == choice {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("unknown list target %q (expected vms or runners)", args[0])
				}
			}

			rt, err := loadValidatedRuntime(cmd.Context())
			if err != nil {
				return fmt.Errorf("%w\nRun `utsusemi validate` to check configuration and credentials", err)
			}

			report, err := listing.Collect(cmd.Context(), rt.listingInput(scope))
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(report)
			}
			fmt.Print(listing.FormatText(report))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	return cmd
}
