package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/instancelock"
)

func newCleanCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Delete all managed VMs and runners for this config",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadValidatedRuntime(cmd.Context())
			if err != nil {
				return err
			}

			lock, err := instancelock.Acquire(filepath.Join(rt.Config.StateDir, "utsusemi.lock"))
			if err != nil {
				return fmt.Errorf("stop utsusemi before clean: %w", err)
			}
			defer lock.Release()

			ag, err := buildAgentFromRuntime(rt)
			if err != nil {
				return err
			}

			vms, runnerIDs, err := ag.PurgeAll(cmd.Context(), dryRun)

			if dryRun {
				if err != nil {
					return err
				}
				fmt.Printf("would delete %d vm(s) and %d runner(s)\n", len(vms), len(runnerIDs))
				for _, vm := range vms {
					fmt.Printf("  vm %s running=%v\n", vm.Name, vm.Running)
				}
				for _, id := range runnerIDs {
					fmt.Printf("  runner %d\n", id)
				}
				return nil
			}

			// Always print what was actually deleted before returning any error,
			// so the operator knows the partial state.
			fmt.Printf("deleted %d vm(s) and %d runner(s)\n", len(vms), len(runnerIDs))
			if err != nil {
				if remaining, remainingRunners, listErr := ag.PurgeAll(cmd.Context(), true); listErr == nil &&
					(len(remaining) > 0 || len(remainingRunners) > 0) {
					fmt.Printf("remaining: %d vm(s), %d runner(s)\n", len(remaining), len(remainingRunners))
					for _, vm := range remaining {
						fmt.Printf("  vm %s (running=%v)\n", vm.Name, vm.Running)
					}
					for _, id := range remainingRunners {
						fmt.Printf("  runner id=%d\n", id)
					}
				}
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print resources without deleting")
	return cmd
}
