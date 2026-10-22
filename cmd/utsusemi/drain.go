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

			if force || timeout <= 0 {
				force = true
			}

			requestID, err := drainctl.NewRequestID()
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			deadline := now
			if !force {
				deadline = now.Add(timeout)
			}
			req := drainctl.Request{
				RequestID:   requestID,
				AgentID:     session.ID,
				RequestedAt: now,
				Deadline:    deadline,
				Force:       force,
			}
			if err := drainctl.WriteRequest(stateDir, req); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "drain requested id=%s force=%v timeout=%s\n",
				requestID, force, timeoutLabel(timeout, force))

			// Cover agent force-grace after the drain deadline, plus a small skew buffer.
			cliDeadline := deadline.Add(drainctl.ForceGrace + 30*time.Second)
			return waitDrainStatus(cmd, stateDir, requestID, session.ID, session.PID, cliDeadline)
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "wait for claimed jobs before force-reclaim (0 means --force)")
	cmd.Flags().BoolVar(&force, "force", false, "reclaim all in-flight runners immediately")
	return cmd
}

func timeoutLabel(d time.Duration, force bool) string {
	if force {
		return "force"
	}
	return d.String()
}

func waitDrainStatus(cmd *cobra.Command, stateDir, requestID, agentID string, agentPID int, cliDeadline time.Time) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	lockPath := filepath.Join(stateDir, "utsusemi.lock")

	for {
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-ticker.C:
			if !lease.PidAlive(agentPID) || !instancelock.Held(lockPath) {
				return fmt.Errorf("%w: agent stopped during drain", drainctl.ErrAgentStopped)
			}
			st, ok, err := drainctl.LoadStatus(stateDir)
			if err != nil {
				return err
			}
			if !ok {
				if time.Now().After(cliDeadline) {
					return fmt.Errorf("drain: timed out waiting for agent status")
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
				if time.Now().After(cliDeadline) {
					return fmt.Errorf("drain: timed out waiting for agent status")
				}
				continue
			}
			if st.AgentID != "" && st.AgentID != agentID {
				return fmt.Errorf("drain: status agent mismatch")
			}
			if !drainctl.IsTerminal(st) {
				if time.Now().After(cliDeadline) {
					return fmt.Errorf("drain: timed out (still active=%d)", st.Active)
				}
				continue
			}
			if st.State == drainctl.StateError {
				if st.Error != "" {
					return fmt.Errorf("drain: %s", st.Error)
				}
				return fmt.Errorf("drain: failed")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "drain complete forced=%v\n", st.Forced)
			return nil
		}
	}
}
