package main

import (
	"fmt"

	"github.com/novr/utsusemi/internal/doctor"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run local preflight checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadConfigRuntime(cmd.Context())
			if err != nil {
				return err
			}
			report := doctor.Collect(cmd.Context(), rt.DoctorInput(nil))
			if jsonOut {
				return writeJSON(report)
			}
			fmt.Print(doctor.FormatText(report))
			if doctor.FailedCount(report) > 0 {
				return fmt.Errorf("%d check(s) failed", doctor.FailedCount(report))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	return cmd
}
