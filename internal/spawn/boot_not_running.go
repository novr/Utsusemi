package spawn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/novr/utsusemi/internal/instancelock"
)

const (
	bootNotRunningFile = "boot_not_running.json"
	bootNotRunningLock = "boot_not_running.lock"
)

// BootNotRunningStats tracks how often waitUntilReady hit notRunningBudget.
type BootNotRunningStats struct {
	Count  int       `json:"count"`
	LastAt time.Time `json:"last_at"`
	LastVM string    `json:"last_vm,omitempty"`
}

// RecordBootNotRunning increments the persistent failure counter (best-effort).
func RecordBootNotRunning(stateDir, vmName string) error {
	if stateDir == "" {
		return nil
	}
	lock, err := instancelock.AcquireBlocking(filepath.Join(stateDir, bootNotRunningLock))
	if err != nil {
		return err
	}
	defer lock.Release()

	stats, _ := loadBootNotRunningUnlocked(stateDir)
	stats.Count++
	stats.LastAt = time.Now().UTC()
	stats.LastVM = vmName
	data, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(stateDir, "boot_not_running-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(stateDir, bootNotRunningFile))
}

// LoadBootNotRunning reads the counter file. ok is false when missing or invalid.
func LoadBootNotRunning(stateDir string) (BootNotRunningStats, bool) {
	return loadBootNotRunningUnlocked(stateDir)
}

func loadBootNotRunningUnlocked(stateDir string) (BootNotRunningStats, bool) {
	if stateDir == "" {
		return BootNotRunningStats{}, false
	}
	data, err := os.ReadFile(filepath.Join(stateDir, bootNotRunningFile))
	if err != nil {
		return BootNotRunningStats{}, false
	}
	var stats BootNotRunningStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return BootNotRunningStats{}, false
	}
	if stats.Count <= 0 {
		return BootNotRunningStats{}, false
	}
	return stats, true
}
