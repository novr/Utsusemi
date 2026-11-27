package proxyfwd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	MountName  = "utsusemi-proxy-forward-bin"
	BinaryName = "utsusemi"
)

// BinDir returns the host directory mounted read-only into guests for proxy-guest.
func BinDir(stateDir string) string {
	return filepath.Join(stateDir, "proxy-forward-bin")
}

// GuestMountDir returns the VirtioFS mount path inside the Tart guest.
func GuestMountDir() string {
	return "/Volumes/My Shared Files/" + MountName
}

// WithMount appends the read-only proxy-forward binary mount when enabled.
func WithMount(stateDir string, enabled bool, mounts []string) []string {
	if !enabled || strings.TrimSpace(stateDir) == "" {
		return mounts
	}
	dir := BinDir(stateDir)
	_ = os.MkdirAll(dir, 0o755)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	spec := MountName + ":" + dir + ":ro"
	for _, m := range mounts {
		if m == spec {
			return mounts
		}
	}
	out := make([]string, 0, len(mounts)+1)
	out = append(out, mounts...)
	out = append(out, spec)
	return out
}

// StageBin copies the current executable into BinDir for guest execution.
func StageBin(stateDir string) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir is empty")
	}
	dir := BinDir(stateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return fmt.Errorf("eval executable: %w", err)
	}
	dest := filepath.Join(dir, BinaryName)
	if err := copyFile(self, dest, 0o755); err != nil {
		return fmt.Errorf("stage proxy-forward binary: %w", err)
	}
	_ = exec.Command("xattr", "-d", "com.apple.quarantine", dest).Run()
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
