package instancelock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrBusy is returned when another process holds the exclusive lock.
var ErrBusy = errors.New("another utsusemi instance is running")

type Lock struct {
	file *os.File
}

func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, lockErr(err)
	}
	return &Lock{file: file}, nil
}

func AcquireBlocking(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, lockErr(err)
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	return l.file.Close()
}

// Held reports whether another process holds an exclusive lock on path.
func Held(path string) bool {
	lock, err := Acquire(path)
	if err != nil {
		return true
	}
	_ = lock.Release()
	return false
}

// ImageLockPath is the per-ref lock SyncImage and Clone share.
func ImageLockPath(stateDir, ref string) (string, error) {
	stateDir = strings.TrimSpace(stateDir)
	ref = strings.TrimSpace(ref)
	if stateDir == "" || ref == "" {
		return "", errors.New("state dir and image are required")
	}
	sum := sha256.Sum256([]byte(ref))
	return filepath.Join(stateDir, "image-locks", hex.EncodeToString(sum[:])+".lock"), nil
}

// AcquireImage waits for the per-ref image lock.
func AcquireImage(ctx context.Context, stateDir, ref string) (*Lock, error) {
	path, err := ImageLockPath(stateDir, ref)
	if err != nil {
		return nil, err
	}
	return AcquireWait(ctx, path)
}

// AcquireWait blocks until the lock is held or ctx is done.
// A lock acquired after cancellation is released.
func AcquireWait(ctx context.Context, path string) (*Lock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		lock *Lock
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		lock, err := AcquireBlocking(path)
		ch <- result{lock, err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			res := <-ch
			if res.lock != nil {
				_ = res.lock.Release()
			}
		}()
		return nil, ctx.Err()
	case res := <-ch:
		return res.lock, res.err
	}
}

func lockErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrBusy
	}
	return err
}
