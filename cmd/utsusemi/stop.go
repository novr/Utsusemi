package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/drainctl"
)

func newStopCmd() *cobra.Command {
	var (
		timeout time.Duration
		force   bool
	)

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Drain the pool and hold it empty (agent stays up)",
		Long: `Ask the running agent to stop accepting new jobs and finish in-flight
runners, then keep active=0 without refilling.

The agent process keeps running. Restart it (for example brew services restart
utsusemi) to accept jobs again. Idle runners are torn down immediately; claimed
jobs are waited on by default. Use --timeout or --force to reclaim claimed jobs
early. SIGTERM or brew services stop before completion still cancels claimed jobs.

Ctrl-C only stops this CLI wait; the agent continues holding the pool empty.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPoolControl(cmd, poolControlOpts{
				name:    "stop",
				stop:    true,
				force:   force,
				timeout: timeout,
				complete: func(cmd *cobra.Command, st drainctl.Status) {
					fmt.Fprintf(cmd.OutOrStdout(), "stop complete forced=%v active=%d (pool held empty; restart agent to refill)\n",
						st.Forced, st.Active)
				},
			})
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", 0, "wait this long for claimed jobs before force-reclaim (default: wait without force)")
	cmd.Flags().BoolVar(&force, "force", false, "reclaim all in-flight runners immediately")
	return cmd
}
