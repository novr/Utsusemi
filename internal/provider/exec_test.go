package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartDetachedOutlivesContext(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "done")
	ctx, cancel := context.WithCancel(context.Background())
	if err := startDetached(ctx, "bash", []string{"-c", "sleep 1; touch " + marker}, nil); err != nil {
		t.Fatal(err)
	}
	cancel()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("detached process was killed when its context was cancelled")
}

func TestStartDetachedRejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := startDetached(ctx, "bash", []string{"-c", "true"}, nil); err == nil {
		t.Fatal("expected failure for an already cancelled context")
	}
}

func TestStartDetachedReportsEarlyExit(t *testing.T) {
	orig := startDetachedGrace
	startDetachedGrace = 200 * time.Millisecond
	t.Cleanup(func() { startDetachedGrace = orig })

	err := startDetached(context.Background(), "bash", []string{"-c", "echo boom >&2; exit 7"}, nil)
	if err == nil {
		t.Fatal("expected early-exit error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error=%v", err)
	}
}

func TestStartRejectsMissingMountPath(t *testing.T) {
	exec := NewFakeExecutor()
	p := NewTartProvider(exec, false, []string{"/nonexistent-utsusemi-mount-xyz"})
	if err := p.Start(context.Background(), "vm-1"); err == nil {
		t.Fatal("expected missing mount error")
	}
	if len(exec.Calls) != 0 {
		t.Fatalf("tart should not start: calls=%v", exec.Calls)
	}
}
