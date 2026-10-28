package actioncache

import (
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed hook.js
var hookFS embed.FS

// StageBin copies the current executable and hook.js into BinDir for guest mount.
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
	destBin := filepath.Join(dir, BinaryName)
	if err := copyFile(self, destBin, 0o755); err != nil {
		return fmt.Errorf("stage binary: %w", err)
	}
	hook, err := hookFS.ReadFile(HookName)
	if err != nil {
		return fmt.Errorf("embed hook: %w", err)
	}
	destHook := filepath.Join(dir, HookName)
	if err := os.WriteFile(destHook, hook, 0o644); err != nil {
		return fmt.Errorf("stage hook: %w", err)
	}
	clearQuarantine(destBin)
	clearQuarantine(destHook)
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

func clearQuarantine(path string) {
	_ = exec.Command("xattr", "-d", "com.apple.quarantine", path).Run()
}

// HookSource returns the embedded hook for tests.
func HookSource() ([]byte, error) {
	return hookFS.ReadFile(HookName)
}
