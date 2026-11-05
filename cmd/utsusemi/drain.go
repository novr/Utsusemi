package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/drainctl"
	"github.com/novr/utsusemi/internal/instancelock"
	"github.com/novr/utsusemi/internal/lease"
)

func newDrainCmd() *cobra.Command {
	var (
		timeout time.Duration
		force   bool
	)

	cmd := &cobra.Command{
		Use:   "drain",
		Short: "Drain the pool then refill (agent stays up)",
		Long: `Ask the running agent to stop accepting new jobs, finish or reclaim
in-flight runners, then refill to pool_size.

Unlike clean, the agent must be running and is not stopped. Idle (unclaimed)
runners are torn down immediately; claimed jobs are waited on until --timeout
(or --force). A race remains between GitHub assignment and runner log markers.

clean is the last-resort wipe after stopping the agent.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if force || timeout <= 0 {
				force = true
			}
			return runPoolControl(cmd, poolControlOpts{
				name:     "drain",
				stop:     false,
				force:    force,
				timeout:  timeout,
				complete: func(cmd *cobra.Command, st drainctl.Status) {
					fmt.Fprintf(cmd.OutOrStdout(), "drain complete forced=%v\n", st.Forced)
				},
			})
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "wait for claimed jobs before force-reclaim (0 means --force)")
	cmd.Flags().BoolVar(&force, "force", false, "reclaim all in-flight runners immediately")
	return cmd
}

type poolControlOpts struct {
	name     string
	stop     bool
	force    bool
	timeout  time.Duration
	complete func(*cobra.Command, drainctl.Status)
}

func runPoolControl(cmd *cobra.Command, opts poolControlOpts) error {
	rt, err := loadConfigRuntime(cmd.Context())
	if err != nil {
		return err
	}
	stateDir := rt.Config.StateDir

	session, err := lease.NewRegistry(stateDir).LoadAgentSession()
	if err != nil {
		return err
	}
	if session == nil || !lease.PidAlive(session.PID) || !instancelock.Held(filepath.Join(stateDir, "utsusemi.lock")) {
		return drainctl.ErrAgentStopped
	}

	if st, ok, err := drainctl.LoadStatus(stateDir); err != nil {
		return err
	} else if ok && st.State == drainctl.StateRunning && st.AgentID == session.ID {
		return drainctl.ErrBusy
	}

	requestID, err := drainctl.NewRequestID()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var deadline time.Time
	switch {
	case opts.force:
		deadline = now
	case opts.timeout > 0:
		deadline = now.Add(opts.timeout)
	default:
		// Zero deadline: agent waits without Force (stop default).
	}
	req := drainctl.Request{
		RequestID:   requestID,
		AgentID:     session.ID,
		RequestedAt: now,
		Deadline:    deadline,
		Force:       opts.force,
		Stop:        opts.stop,
	}
	if err := drainctl.WriteRequest(stateDir, req); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s requested id=%s force=%v timeout=%s\n",
		opts.name, requestID, opts.force, timeoutLabel(opts.timeout, opts.force))

	var cliDeadline time.Time
	if opts.force || opts.timeout > 0 {
		// Cover agent force-grace after the drain deadline, plus a small skew buffer.
		cliDeadline = deadline.Add(drainctl.ForceGrace + 30*time.Second)
	}
	return waitDrainStatus(cmd, stateDir, requestID, session.ID, session.PID, cliDeadline, opts)
}

func timeoutLabel(d time.Duration, force bool) string {
	if force {
		return "force"
	}
	if d <= 0 {
		return "none"
	}
	return d.String()
}

func waitDrainStatus(cmd *cobra.Command, stateDir, requestID, agentID string, agentPID int, cliDeadline time.Time, opts poolControlOpts) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	lockPath := filepath.Join(stateDir, "utsusemi.lock")
	limited := !cliDeadline.IsZero()

	for {
		select {
		case <-cmd.Context().Done():
			if opts.stop {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: interrupted; the agent keeps stopping and will hold the pool empty\n", opts.name)
			}
			return cmd.Context().Err()
		case <-ticker.C:
			if !lease.PidAlive(agentPID) || !instancelock.Held(lockPath) {
				return fmt.Errorf("%w: agent stopped during %s", drainctl.ErrAgentStopped, opts.name)
			}
			st, ok, err := drainctl.LoadStatus(stateDir)
			if err != nil {
				return err
			}
			if !ok {
				if limited && time.Now().After(cliDeadline) {
					return fmt.Errorf("%s: timed out waiting for agent status", opts.name)
				}
				continue
			}
			if st.RejectedRequestID == requestID {
				if st.RejectedError != "" {
					return fmt.Errorf("%s", st.RejectedError)
				}
				return drainctl.ErrBusy
			}
			if !drainctl.MatchesRequest(st, requestID) {
				if limited && time.Now().After(cliDeadline) {
					return fmt.Errorf("%s: timed out waiting for agent status", opts.name)
				}
				continue
			}
			if st.AgentID != "" && st.AgentID != agentID {
				return fmt.Errorf("%s: status agent mismatch", opts.name)
			}
			if !drainctl.IsTerminal(st) {
				if limited && time.Now().After(cliDeadline) {
					return fmt.Errorf("%s: timed out (still active=%d)", opts.name, st.Active)
				}
				continue
			}
			if st.State == drainctl.StateError {
				if st.Error != "" {
					return fmt.Errorf("%s: %s", opts.name, st.Error)
				}
				return fmt.Errorf("%s: failed", opts.name)
			}
			if opts.complete != nil {
				opts.complete(cmd, st)
			}
			return nil
		}
	}
}
