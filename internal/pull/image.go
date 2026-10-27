package pull

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/novr/utsusemi/internal/instancelock"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/runtimeconfig"
)

// ImageOptions syncs one base image ref through the VM provider.
// Config is not modified. Sync is Provider.SyncImage.
type ImageOptions struct {
	Ref             string
	StateDir        string
	ConfigBaseImage string
	Sync            func(ctx context.Context, ref string) error
	Stderr          io.Writer
}

// Image syncs ref. The process lock is used only to see whether the agent is
// running, then released. SyncImage holds a per-ref image lock shared with
// startup SyncImage and Clone of that same name.
func Image(ctx context.Context, opts ImageOptions) error {
	ref := strings.TrimSpace(opts.Ref)
	if ref == "" {
		return fmt.Errorf("image is required")
	}
	if opts.Sync == nil {
		return fmt.Errorf("image sync is required")
	}
	if strings.TrimSpace(opts.StateDir) == "" {
		return fmt.Errorf("state dir is required")
	}

	proc, err := instancelock.Acquire(filepath.Join(opts.StateDir, "utsusemi.lock"))
	if err != nil {
		if !isBusy(err) {
			return err
		}
		live, ok := confirmedBaseImage(opts.StateDir)
		if !ok || live == ref {
			return fmt.Errorf("stop utsusemi before pull image %s: %w", ref, instancelock.ErrBusy)
		}
		return pullUnderImageLock(ctx, opts, ref)
	}

	// Keep the process lock until this ref's image lock is held, so a start
	// cannot begin SyncImage/Clone of ref and then overlap this download.
	img, err := instancelock.AcquireImage(ctx, opts.StateDir, ref)
	_ = proc.Release()
	if err != nil {
		return err
	}
	defer img.Release()
	return pullImage(ctx, opts, ref)
}

func pullUnderImageLock(ctx context.Context, opts ImageOptions, ref string) error {
	img, err := instancelock.AcquireImage(ctx, opts.StateDir, ref)
	if err != nil {
		return err
	}
	defer img.Release()
	return pullImage(ctx, opts, ref)
}

func pullImage(ctx context.Context, opts ImageOptions, ref string) error {
	configRef := strings.TrimSpace(opts.ConfigBaseImage)
	if configRef != "" && configRef != ref {
		fmt.Fprintf(stderrOf(opts.Stderr), "next start still uses base_image %s\n", configRef)
	}
	if err := opts.Sync(ctx, ref); err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	return nil
}

func confirmedBaseImage(stateDir string) (string, bool) {
	session, err := lease.NewRegistry(stateDir).LoadAgentSession()
	if err != nil || session == nil || !lease.PidAlive(session.PID) {
		return "", false
	}
	snap, ok := runtimeconfig.Load(stateDir)
	if !ok || snap.AgentID == "" || snap.AgentID != session.ID {
		return "", false
	}
	live := strings.TrimSpace(snap.BaseImage)
	if live == "" {
		return "", false
	}
	return live, true
}

func isBusy(err error) bool {
	return errors.Is(err, instancelock.ErrBusy)
}
