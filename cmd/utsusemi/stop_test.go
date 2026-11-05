package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/drainctl"
	"github.com/novr/utsusemi/internal/instancelock"
)

func holdAgentLock(t *testing.T, stateDir string) {
	t.Helper()
	l, err := instancelock.Acquire(filepath.Join(stateDir, "utsusemi.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Release() })
}

func testControlCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd
}

func TestStopRejectsNegativeTimeout(t *testing.T) {
	cmd := newStopCmd()
	cmd.SetArgs([]string{"--timeout=-1s"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--timeout") {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitStopUnlimitedFailsWithoutAck(t *testing.T) {
	stateDir := t.TempDir()
	holdAgentLock(t, stateDir)
	prev := poolControlAckTimeout
	poolControlAckTimeout = 300 * time.Millisecond
	t.Cleanup(func() { poolControlAckTimeout = prev })

	err := waitDrainStatus(testControlCmd(), stateDir, "req-1", "agent", os.Getpid(), time.Time{},
		poolControlOpts{name: "stop", stop: true})
	if err == nil || !strings.Contains(err.Error(), "did not acknowledge") {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitStopErrorSaysPoolHeld(t *testing.T) {
	stateDir := t.TempDir()
	holdAgentLock(t, stateDir)
	if err := drainctl.WriteStatus(stateDir, drainctl.Status{
		RequestID: "req-1",
		AgentID:   "agent",
		State:     drainctl.StateError,
		Active:    1,
		Stop:      true,
		Error:     "force reclaim timed out with runners still active",
	}); err != nil {
		t.Fatal(err)
	}

	err := waitDrainStatus(testControlCmd(), stateDir, "req-1", "agent", os.Getpid(), time.Time{},
		poolControlOpts{name: "stop", stop: true})
	if err == nil || !strings.Contains(err.Error(), "pool held empty; active=1") {
		t.Fatalf("err=%v", err)
	}
}
