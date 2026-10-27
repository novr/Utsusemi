package pull

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/instancelock"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/runtimeconfig"
)

func TestImageHoldsImageLockWhilePulling(t *testing.T) {
	state := t.TempDir()
	ref := "ghcr.io/example/image:1"
	err := Image(context.Background(), ImageOptions{
		Ref:      ref,
		StateDir: state,
		Sync: func(context.Context, string) error {
			proc, err := instancelock.Acquire(filepath.Join(state, "utsusemi.lock"))
			if err != nil {
				t.Fatalf("process lock: %v", err)
			}
			_ = proc.Release()
			path, err := instancelock.ImageLockPath(state, ref)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := instancelock.Acquire(path); !errors.Is(err, instancelock.ErrBusy) {
				t.Fatalf("image lock: %v", err)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestImageRefusesInUseOrUnknownWhileRunning(t *testing.T) {
	state := t.TempDir()
	holdLock(t, state)
	publishLiveImage(t, state, "ghcr.io/example/image:1")

	var pulls int
	err := Image(context.Background(), ImageOptions{
		Ref:      "ghcr.io/example/image:1",
		StateDir: state,
		Sync: func(context.Context, string) error {
			pulls++
			return nil
		},
	})
	if !errors.Is(err, instancelock.ErrBusy) {
		t.Fatalf("err=%v", err)
	}
	if pulls != 0 {
		t.Fatalf("pulls=%d", pulls)
	}

	stateUnknown := t.TempDir()
	holdLock(t, stateUnknown)
	err = Image(context.Background(), ImageOptions{
		Ref:      "ghcr.io/example/image:1",
		StateDir: stateUnknown,
		Sync: func(context.Context, string) error {
			pulls++
			return nil
		},
	})
	if pulls != 0 {
		t.Fatalf("pulls=%d", pulls)
	}
}

func TestImageRefusesEmptyLiveRef(t *testing.T) {
	state := t.TempDir()
	holdLock(t, state)
	publishLiveImage(t, state, "  ")

	var pulls int
	err := Image(context.Background(), ImageOptions{
		Ref:      "ghcr.io/example/image:1",
		StateDir: state,
		Sync: func(context.Context, string) error {
			pulls++
			return nil
		},
	})
	if !errors.Is(err, instancelock.ErrBusy) {
		t.Fatalf("err=%v", err)
	}
	if pulls != 0 {
		t.Fatalf("pulls=%d", pulls)
	}
}

func TestImagePullsDifferentLiveRef(t *testing.T) {
	state := t.TempDir()
	holdLock(t, state)
	publishLiveImage(t, state, "ghcr.io/example/old:1")

	var pulled string
	var stderr bytes.Buffer
	err := Image(context.Background(), ImageOptions{
		Ref:             "  ghcr.io/example/new:1  ",
		StateDir:        state,
		ConfigBaseImage: "ghcr.io/example/old:1",
		Stderr:          &stderr,
		Sync: func(_ context.Context, ref string) error {
			pulled = ref
			path, err := instancelock.ImageLockPath(state, ref)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := instancelock.Acquire(path); !errors.Is(err, instancelock.ErrBusy) {
				t.Fatalf("image lock: %v", err)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pulled != "ghcr.io/example/new:1" {
		t.Fatalf("pulled %q", pulled)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("next start still uses base_image ghcr.io/example/old:1")) {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestImageLockSetupErrorDoesNotPull(t *testing.T) {
	parent := t.TempDir()
	state := filepath.Join(parent, "not-a-directory")
	if err := os.WriteFile(state, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var pulls int
	err := Image(context.Background(), ImageOptions{
		Ref:      "ghcr.io/example/image:1",
		StateDir: state,
		Sync: func(context.Context, string) error {
			pulls++
			return nil
		},
	})
	if err == nil || errors.Is(err, instancelock.ErrBusy) {
		t.Fatalf("err=%v", err)
	}
	if pulls != 0 {
		t.Fatalf("pulls=%d", pulls)
	}
}

func holdLock(t *testing.T, stateDir string) {
	t.Helper()
	lock, err := instancelock.Acquire(filepath.Join(stateDir, "utsusemi.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
}

func publishLiveImage(t *testing.T, stateDir, baseImage string) {
	t.Helper()
	reg := lease.NewRegistry(stateDir)
	session, err := reg.BeginAgentSession()
	if err != nil {
		t.Fatal(err)
	}
	snap := runtimeconfig.FromConfig(&config.Config{BaseImage: baseImage}, session.ID)
	if err := runtimeconfig.Save(stateDir, snap); err != nil {
		t.Fatal(err)
	}
}
