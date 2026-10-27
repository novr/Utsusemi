package instancelock

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLockErrBusyOnlyForContention(t *testing.T) {
	if !errors.Is(lockErr(syscall.EAGAIN), ErrBusy) || !errors.Is(lockErr(syscall.EWOULDBLOCK), ErrBusy) {
		t.Fatal("contention should be busy")
	}
	if errors.Is(lockErr(syscall.EINVAL), ErrBusy) {
		t.Fatal("EINVAL should not be busy")
	}
}

func TestAcquireWaitBlocksUntilRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	held, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		lock, err := AcquireWait(context.Background(), path)
		if err != nil {
			t.Errorf("wait: %v", err)
		}
		_ = lock.Release()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("acquired while lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("still waiting after release")
	}
}

func TestImageLockPathTrimsRef(t *testing.T) {
	a, err := ImageLockPath("/tmp/state", " ghcr.io/example/image:1 ")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ImageLockPath("/tmp/state", "ghcr.io/example/image:1")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("%s != %s", a, b)
	}
}
