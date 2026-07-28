package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/agent"
	"github.com/novr/utsusemi/internal/instancelock"
	"github.com/novr/utsusemi/internal/logging"
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

			lock, err := instancelock.Acquire(filepath.Join(rt.cfg.StateDir, "utsusemi.lock"))
			if err != nil {
				return fmt.Errorf("stop utsusemi before clean: %w", err)
			}
			defer lock.Release()

			ag, err := agent.New(agent.Options{
				Config:    rt.cfg,
				Target:    rt.tgt,
				Provider:  rt.provider,
				Registrar: rt.registrar,
				Logger:    logging.New(),
			})
			if err != nil {
				return err
			}

			vms, runnerIDs, err := ag.ReclaimAll(cmd.Context(), dryRun)
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Printf("would delete %d vm(s) and %d runner(s)\n", len(vms), len(runnerIDs))
				for _, vm := range vms {
					fmt.Printf("  vm %s running=%v\n", vm.Name, vm.Running)
				}
				for _, id := range runnerIDs {
					fmt.Printf("  runner %d\n", id)
				}
				return nil
			}

			fmt.Printf("deleted %d vm(s) and %d runner(s)\n", len(vms), len(runnerIDs))
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print resources without deleting")
	return cmd
}
