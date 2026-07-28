package provider

import (
	"context"
	"os"
	"path/filepath"
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
