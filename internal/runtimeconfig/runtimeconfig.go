package runtimeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/target"
)

const fileName = "runtime_config.json"

// Snapshot is the agent-frozen config written to StateDir for `utsusemi status`.
// Only non-secret operational knobs; do not add credential material.
type Snapshot struct {
	SavedAt           time.Time `json:"saved_at"`
	AgentID           string    `json:"agent_id,omitempty"`
	Target            string    `json:"target"`
	BaseImage         string    `json:"base_image"`
	RunnerVersion     string    `json:"runner_version"`
	PoolSize          int       `json:"pool_size"`
	Labels            []string  `json:"labels,omitempty"`
	ReclaimPolicy     string    `json:"reclaim_policy,omitempty"`
	MinFreeDiskGB     int       `json:"min_free_disk_gb"`
	Softnet           bool      `json:"softnet"`
	Mounts            []string  `json:"mounts,omitempty"`
	SystemProxyHTTP   string    `json:"system_proxy_http,omitempty"`
	SystemProxyHTTPS  string    `json:"system_proxy_https,omitempty"`
	SystemProxyBypass []string  `json:"system_proxy_bypass,omitempty"`
	SpawnTimeout      string    `json:"spawn_timeout"`
	JobTimeout        string    `json:"job_timeout"`
	VMNamePrefix      string    `json:"vm_name_prefix"`
	StateDir          string    `json:"state_dir"`
	PoolCheck         string    `json:"pool_check_interval,omitempty"`
	Reconciliation    string    `json:"reconciliation_interval,omitempty"`
	ReclaimGrace      string    `json:"reclaim_grace,omitempty"`
}

func FromConfig(cfg *config.Config, agentID string) Snapshot {
	if cfg == nil {
		return Snapshot{SavedAt: time.Now().UTC(), AgentID: agentID}
	}
	tgt := ""
	if parsed, err := target.FromConfig(cfg.Target); err == nil {
		tgt = parsed.DisplayString()
	}
	labels := cfg.Labels
	if labels != nil {
		labels = append([]string(nil), labels...)
	}
	mounts := cfg.Mounts
	if mounts != nil {
		mounts = append([]string(nil), mounts...)
	}
	var proxyHTTP, proxyHTTPS string
	var proxyBypass []string
	if cfg.SystemProxy != nil {
		proxyHTTP = cfg.SystemProxy.HTTP
		proxyHTTPS = cfg.SystemProxy.HTTPS
		if cfg.SystemProxy.Bypass != nil {
			proxyBypass = append([]string(nil), cfg.SystemProxy.Bypass...)
		}
	}
	return Snapshot{
		SavedAt:           time.Now().UTC(),
		AgentID:           agentID,
		Target:            tgt,
		BaseImage:         cfg.BaseImage,
		RunnerVersion:     cfg.RunnerVersion,
		PoolSize:          cfg.PoolSize,
		Labels:            labels,
		ReclaimPolicy:     cfg.ReclaimPolicy,
		MinFreeDiskGB:     cfg.MinFreeDiskGB,
		Softnet:           cfg.Softnet,
		Mounts:            mounts,
		SystemProxyHTTP:   proxyHTTP,
		SystemProxyHTTPS:  proxyHTTPS,
		SystemProxyBypass: proxyBypass,
		SpawnTimeout:      cfg.SpawnTimeout.Duration().String(),
		JobTimeout:        cfg.JobTimeout.Duration().String(),
		VMNamePrefix:      cfg.VMNamePrefix,
		StateDir:          cfg.StateDir,
		PoolCheck:         cfg.PoolCheckInterval.Duration().String(),
		Reconciliation:    cfg.ReconciliationInterval.Duration().String(),
		ReclaimGrace:      cfg.ReclaimGrace.Duration().String(),
	}
}

func Save(stateDir string, snap Snapshot) error {
	if stateDir == "" {
		return nil
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	final := filepath.Join(stateDir, fileName)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

func Load(stateDir string) (Snapshot, bool) {
	if stateDir == "" {
		return Snapshot{}, false
	}
	data, err := os.ReadFile(filepath.Join(stateDir, fileName))
	if err != nil {
		return Snapshot{}, false
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, false
	}
	return snap, true
}
