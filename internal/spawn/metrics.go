package spawn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const lastSpawnFile = "last_spawn.json"

type LastSpawn struct {
	At            time.Time `json:"at"`
	VMName        string    `json:"vm_name,omitempty"`
	RunnerVersion string    `json:"runner_version"`
	CloneMs       int64     `json:"clone_ms"`
	BootMs        int64     `json:"boot_ms"`
	RegisterMs    int64     `json:"register_ms"`
	JobMs         int64     `json:"job_ms"`
	ColdStartMs   int64     `json:"cold_start_ms"`
	TotalMs       int64     `json:"total_ms"`
	Success       bool      `json:"success"`
}

func (m LastSpawn) coldStartMs() int64 {
	return m.CloneMs + m.BootMs + m.RegisterMs
}

// RunnerVersionSnapshot compares configured runner_version with the last successful spawn metrics.
type RunnerVersionSnapshot struct {
	Configured  string
	LastMetrics LastSpawn
	HasMetrics  bool
}

func LoadRunnerVersionSnapshot(configured, stateDir string) RunnerVersionSnapshot {
	snap := RunnerVersionSnapshot{Configured: strings.TrimSpace(configured)}
	if last, ok := LoadLastSpawn(stateDir); ok {
		snap.LastMetrics = last
		snap.HasMetrics = true
	}
	return snap
}

func (s RunnerVersionSnapshot) Mismatch() bool {
	return s.HasMetrics && s.LastMetrics.RunnerVersion != s.Configured
}

func SaveLastSpawn(stateDir string, m LastSpawn) error {
	if stateDir == "" || !m.Success {
		return nil
	}
	m.ColdStartMs = m.coldStartMs()
	m.TotalMs = m.ColdStartMs + m.JobMs
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_ = os.MkdirAll(stateDir, 0o755)
	return os.WriteFile(filepath.Join(stateDir, lastSpawnFile), data, 0o644)
}

func LoadLastSpawn(stateDir string) (LastSpawn, bool) {
	data, err := os.ReadFile(filepath.Join(stateDir, lastSpawnFile))
	if err != nil {
		return LastSpawn{}, false
	}
	var m LastSpawn
	if err := json.Unmarshal(data, &m); err != nil {
		return LastSpawn{}, false
	}
	if !m.Success {
		return LastSpawn{}, false
	}
	return m, true
}
