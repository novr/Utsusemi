package actioncache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DataMountName = "utsusemi-action-cache"
	BinMountName  = "utsusemi-action-cache-bin"

	GuestDataDir = "/Volumes/My Shared Files/" + DataMountName
	GuestBinDir  = "/Volumes/My Shared Files/" + BinMountName

	dataSubdir = "action-cache"
	binSubdir  = "action-cache-bin"

	// DefaultAddr is the guest-local listen address. Each VM has its own
	// localhost, so a fixed port is safe across concurrent pool slots.
	DefaultAddr = "127.0.0.1:19527"

	BinaryName = "utsusemi"
	HookName   = "hook.js"
)

// Dir is the host directory for finalized cache archives.
func Dir(stateDir string) string {
	return filepath.Join(stateDir, dataSubdir)
}

// BinDir holds the staged host binary and hook copied into the guest.
func BinDir(stateDir string) string {
	return filepath.Join(stateDir, binSubdir)
}

// PrepareDir creates the data directory for Tart --dir.
func PrepareDir(stateDir string) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir is empty")
	}
	return os.MkdirAll(Dir(stateDir), 0o755)
}

// DataMountEntry returns a Tart --dir value for the writable cache store.
func DataMountEntry(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	dir, err := filepath.Abs(Dir(stateDir))
	if err != nil {
		dir = Dir(stateDir)
	}
	return fmt.Sprintf("%s:%s", DataMountName, dir)
}

// BinMountEntry returns a Tart --dir value for the staged binary (read-only).
func BinMountEntry(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	dir, err := filepath.Abs(BinDir(stateDir))
	if err != nil {
		dir = BinDir(stateDir)
	}
	return fmt.Sprintf("%s:%s:ro", BinMountName, dir)
}

// PrepareBinDir creates the staged-binary directory for Tart --dir.
func PrepareBinDir(stateDir string) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir is empty")
	}
	return os.MkdirAll(BinDir(stateDir), 0o755)
}

// WithMounts prepends action-cache mounts when enabled.
func WithMounts(stateDir string, enabled bool, mounts []string) []string {
	if !enabled || strings.TrimSpace(stateDir) == "" {
		return append([]string(nil), mounts...)
	}
	_ = PrepareDir(stateDir)
	_ = PrepareBinDir(stateDir)
	out := make([]string, 0, len(mounts)+2)
	if entry := DataMountEntry(stateDir); entry != "" {
		out = append(out, entry)
	}
	if entry := BinMountEntry(stateDir); entry != "" {
		out = append(out, entry)
	}
	out = append(out, mounts...)
	return out
}
