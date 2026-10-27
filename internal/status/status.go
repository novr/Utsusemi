package status

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/credentialview"
	"github.com/novr/utsusemi/internal/drainctl"
	"github.com/novr/utsusemi/internal/hostid"
	"github.com/novr/utsusemi/internal/instancelock"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/runnercache"
	"github.com/novr/utsusemi/internal/runtimeconfig"
	"github.com/novr/utsusemi/internal/spawn"
	"github.com/novr/utsusemi/internal/target"
	"github.com/novr/utsusemi/internal/timefmt"
)

type AgentState string

const (
	AgentRunning AgentState = "running"
	AgentStale   AgentState = "stale"
	AgentStopped AgentState = "stopped"
)

type Input struct {
	Cfg      *config.Config
	Target   target.Target
	Provider provider.VMProvider
	Store    keychain.Store
}

type Report struct {
	Target               string              `json:"target"`
	ConfigPath           string              `json:"config_path,omitempty"`
	StateDir             string              `json:"state_dir"`
	Host                 HostInfo            `json:"host"`
	Runtime              *RuntimeInfo        `json:"runtime,omitempty"`
	RunnerVersion        RunnerVersionInfo   `json:"runner_version"`
	RunnerCache          RunnerCacheInfo     `json:"runner_cache"`
	Mounts               []string            `json:"mounts,omitempty"`
	Spawn                *SpawnInfo          `json:"spawn,omitempty"`
	BootNotRunningBudget *BootNotRunningInfo `json:"boot_not_running_budget,omitempty"`
	Agent                AgentInfo           `json:"agent"`
	Jobs                 []Job               `json:"jobs"`
	VMs                  VMsInfo             `json:"vms"`
	Draining             []string            `json:"draining"`
	PoolDrain            *PoolDrainInfo      `json:"pool_drain,omitempty"`
	Health               HealthInfo          `json:"health"`
	Credential           credentialview.Info `json:"credential"`
}

// PoolDrainInfo is the operator recycle requested by `utsusemi drain`.
// Distinct from Draining (lease-less running VM names).
type PoolDrainInfo struct {
	State     string `json:"state"`
	RequestID string `json:"request_id,omitempty"`
	Active    int    `json:"active"`
	Forced    bool   `json:"forced,omitempty"`
	Deadline  string `json:"deadline,omitempty"`
	Error     string `json:"error,omitempty"`
}

// RuntimeInfo is the agent-frozen config from StateDir/runtime_config.json.
type RuntimeInfo struct {
	State             string   `json:"state"` // active | stale | pending
	SavedAt           string   `json:"saved_at,omitempty"`
	AgentID           string   `json:"agent_id,omitempty"`
	Target            string   `json:"target,omitempty"`
	BaseImage         string   `json:"base_image,omitempty"`
	RunnerVersion     string   `json:"runner_version,omitempty"`
	PoolSize          int      `json:"pool_size,omitempty"`
	Labels            []string `json:"labels,omitempty"`
	ReclaimPolicy     string   `json:"reclaim_policy,omitempty"`
	MinFreeDiskGB     int      `json:"min_free_disk_gb,omitempty"`
	Softnet           bool     `json:"softnet"`
	Mounts            []string `json:"mounts,omitempty"`
	SystemProxyHTTP   string   `json:"system_proxy_http,omitempty"`
	SystemProxyHTTPS  string   `json:"system_proxy_https,omitempty"`
	SystemProxyBypass []string `json:"system_proxy_bypass,omitempty"`
	SpawnTimeout      string   `json:"spawn_timeout,omitempty"`
	JobTimeout        string   `json:"job_timeout,omitempty"`
	VMNamePrefix      string   `json:"vm_name_prefix,omitempty"`
}

type BootNotRunningInfo struct {
	Count  int    `json:"count"`
	LastAt string `json:"last_at"`
	LastVM string `json:"last_vm,omitempty"`
}

type HostInfo struct {
	ID              string   `json:"id"`
	Hostname        string   `json:"hostname"`
	LocalHostName   string   `json:"local_host_name,omitempty"`
	EffectivePrefix string   `json:"effective_prefix"`
	Warnings        []string `json:"warnings,omitempty"`
}

type RunnerVersionInfo struct {
	Configured string `json:"configured"`
	Runtime    string `json:"runtime,omitempty"`
	LastSpawn  string `json:"last_spawn,omitempty"`
	Status     string `json:"status"`
}

type RunnerCacheInfo struct {
	Present bool   `json:"present"`
	Path    string `json:"path,omitempty"`
}

type SpawnInfo struct {
	At        string `json:"at"`
	Clone     string `json:"clone"`
	Boot      string `json:"boot"`
	Register  string `json:"register"`
	ColdStart string `json:"cold_start"`
	Job       string `json:"job"`
	Total     string `json:"total"`
	Success   bool   `json:"success"`
}

type AgentInfo struct {
	State     AgentState `json:"state"`
	PID       int        `json:"pid,omitempty"`
	Uptime    string     `json:"uptime,omitempty"`
	SessionID string     `json:"session_id,omitempty"`
	Warning   string     `json:"warning,omitempty"`
}

type Job struct {
	VMName   string `json:"vm_name"`
	RunnerID int64  `json:"runner_id"`
	Age      string `json:"age"`
	Stale    bool   `json:"stale,omitempty"`
	AgentID  string `json:"agent_id,omitempty"`
}

type VMsInfo struct {
	Running       int `json:"running"`
	Total         int `json:"total"`
	PoolSize      int `json:"pool_size"`
	MaxConcurrent int `json:"max_concurrent,omitempty"`
}

type HealthInfo struct {
	FreeDiskGB    float64 `json:"free_disk_gb"`
	MinFreeDiskGB int     `json:"min_free_disk_gb"`
	Status        string  `json:"status"`
}

func Collect(ctx context.Context, in Input) (Report, error) {
	now := time.Now().UTC()
	stateDir := in.Cfg.StateDir
	reg := lease.NewRegistry(stateDir)
	lockPath := filepath.Join(stateDir, "utsusemi.lock")

	session, err := reg.LoadAgentSession()
	if err != nil {
		return Report{}, err
	}

	agent := classifyAgent(session, instancelock.Held(lockPath))

	leases, err := reg.ListLeases()
	if err != nil {
		return Report{}, err
	}
	jobs := collectJobs(agent.State, session, leases, now)

	runtimeInfo := loadRuntimeInfo(stateDir, agent, session)
	activeRuntime := runtimeInfo != nil && runtimeInfo.State == "active"

	poolSize := in.Cfg.PoolSize
	minFreeDisk := in.Cfg.MinFreeDiskGB
	runnerVersionForCache := in.Cfg.RunnerVersion
	if activeRuntime {
		if runtimeInfo.PoolSize > 0 {
			poolSize = runtimeInfo.PoolSize
		}
		if runtimeInfo.MinFreeDiskGB > 0 {
			minFreeDisk = runtimeInfo.MinFreeDiskGB
		}
		if runtimeInfo.RunnerVersion != "" {
			runnerVersionForCache = runtimeInfo.RunnerVersion
		}
	}

	vms, err := in.Provider.ListManaged(ctx, in.Cfg.VMNamePrefix)
	if err != nil {
		return Report{}, err
	}
	vmInfo, draining := summarizeVMs(vms, poolSize, jobs, agent.State, leases)
	vmInfo.MaxConcurrent = in.Provider.Capabilities().MaxConcurrent

	freeGB, err := in.Provider.FreeDiskGB(ctx)
	if err != nil {
		return Report{}, err
	}
	health := HealthInfo{
		FreeDiskGB:    freeGB,
		MinFreeDiskGB: minFreeDisk,
		Status:        "ok",
	}
	if freeGB < float64(minFreeDisk) {
		health.Status = "low disk"
	}

	store := in.Store
	if store == nil {
		store = keychain.New()
	}
	cred, err := credentialview.Load(in.Cfg, store)
	if err != nil {
		return Report{}, err
	}

	host := hostid.Collect(stateDir, in.Cfg.VMNamePrefix)
	versionSnap := spawn.LoadRunnerVersionSnapshot(in.Cfg.RunnerVersion, stateDir)
	runnerInfo := RunnerVersionInfo{
		Configured: versionSnap.Configured,
		Status:     "ok",
	}
	// Only an active freeze is "what the running agent uses".
	if activeRuntime && runtimeInfo.RunnerVersion != "" {
		runnerInfo.Runtime = runtimeInfo.RunnerVersion
	}
	mounts, err := provider.ResolveMountDirs(runnercache.WithRunnerCacheMount(stateDir, in.Cfg.Mounts))
	if err != nil {
		return Report{}, err
	}
	var spawnInfo *SpawnInfo
	if versionSnap.HasMetrics {
		last := versionSnap.LastMetrics
		runnerInfo.LastSpawn = last.RunnerVersion
		effectiveRunner := versionSnap.Configured
		if runnerInfo.Runtime != "" {
			effectiveRunner = runnerInfo.Runtime
		}
		if last.RunnerVersion != effectiveRunner {
			runnerInfo.Status = "config changed; rebuild base image and restart agent"
		}
		spawnInfo = &SpawnInfo{
			At:        last.At.UTC().Format(time.RFC3339),
			Clone:     formatMs(last.CloneMs),
			Boot:      formatMs(last.BootMs),
			Register:  formatMs(last.RegisterMs),
			ColdStart: formatMs(last.ColdStartMs),
			Job:       formatMs(last.JobMs),
			Total:     formatMs(last.TotalMs),
			Success:   last.Success,
		}
	}
	// YAML drift vs a live freeze must surface even when last_spawn matches runtime.
	if activeRuntime && runnerInfo.Configured != "" && runnerInfo.Runtime != "" && runnerInfo.Runtime != runnerInfo.Configured {
		runnerInfo.Status = "config.yaml differs from running agent; restart agent to apply"
	}

	var bootNotRunning *BootNotRunningInfo
	if stats, ok := spawn.LoadBootNotRunning(stateDir); ok {
		bootNotRunning = &BootNotRunningInfo{
			Count:  stats.Count,
			LastAt: stats.LastAt.UTC().Format(time.RFC3339),
			LastVM: stats.LastVM,
		}
	}

	arch := in.Provider.Capabilities().RunnerArch
	if arch == "" {
		arch = "osx-arm64"
	}
	cachePath, cacheOK := runnercache.Cached(stateDir, arch, runnerVersionForCache)
	runnerCache := RunnerCacheInfo{Present: cacheOK, Path: cachePath}

	return Report{
		Target:               in.Target.DisplayString(),
		StateDir:             stateDir,
		Host:                 HostInfo{ID: host.ID, Hostname: host.Hostname, LocalHostName: host.LocalHostName, EffectivePrefix: host.EffectivePrefix, Warnings: host.Warnings},
		Runtime:              runtimeInfo,
		RunnerVersion:        runnerInfo,
		RunnerCache:          runnerCache,
		Mounts:               mounts,
		Spawn:                spawnInfo,
		BootNotRunningBudget: bootNotRunning,
		Agent:                agent,
		Jobs:                 emptyJobs(jobs),
		VMs:                  vmInfo,
		Draining:             emptyDraining(draining),
		PoolDrain:            loadPoolDrain(stateDir),
		Health:               health,
		Credential:           cred,
	}, nil
}

func loadPoolDrain(stateDir string) *PoolDrainInfo {
	st, ok, err := drainctl.LoadStatus(stateDir)
	if err != nil || !ok {
		return nil
	}
	switch {
	case st.State == drainctl.StateRunning:
		// in progress
	case drainctl.IsTerminal(st) && !st.UpdatedAt.IsZero() && time.Since(st.UpdatedAt) < 2*time.Minute:
		// recent result for operators who just ran drain
	default:
		return nil
	}
	info := &PoolDrainInfo{
		State:     st.State,
		RequestID: st.RequestID,
		Active:    st.Active,
		Forced:    st.Forced,
		Error:     st.Error,
	}
	if !st.Deadline.IsZero() {
		info.Deadline = st.Deadline.UTC().Format(time.RFC3339)
	}
	return info
}

func loadRuntimeInfo(stateDir string, agent AgentInfo, session *lease.AgentSession) *RuntimeInfo {
	snap, ok := runtimeconfig.Load(stateDir)
	if !ok {
		return nil
	}
	info := &RuntimeInfo{
		State:             "stale",
		SavedAt:           snap.SavedAt.UTC().Format(time.RFC3339),
		AgentID:           snap.AgentID,
		Target:            snap.Target,
		BaseImage:         snap.BaseImage,
		RunnerVersion:     snap.RunnerVersion,
		PoolSize:          snap.PoolSize,
		Labels:            snap.Labels,
		ReclaimPolicy:     snap.ReclaimPolicy,
		MinFreeDiskGB:     snap.MinFreeDiskGB,
		Softnet:           snap.Softnet,
		Mounts:            snap.Mounts,
		SystemProxyHTTP:   snap.SystemProxyHTTP,
		SystemProxyHTTPS:  snap.SystemProxyHTTPS,
		SystemProxyBypass: snap.SystemProxyBypass,
		SpawnTimeout:      snap.SpawnTimeout,
		JobTimeout:        snap.JobTimeout,
		VMNamePrefix:      snap.VMNamePrefix,
	}
	if agent.State != AgentRunning || session == nil || snap.AgentID == "" {
		return info
	}
	if snap.AgentID == session.ID {
		info.State = "active"
		return info
	}
	// agent.json and runtime_config.json briefly disagree (either write order).
	info.State = "pending"
	return info
}

func formatMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return timefmt.Age(time.Duration(ms) * time.Millisecond)
}

func classifyAgent(session *lease.AgentSession, lockHeld bool) AgentInfo {
	if session == nil {
		return AgentInfo{State: AgentStopped}
	}
	pidAlive := lease.PidAlive(session.PID)
	info := AgentInfo{
		PID:       session.PID,
		SessionID: session.ID,
	}
	if !pidAlive {
		info.State = AgentStale
		if lockHeld {
			info.Warning = "lock held but agent pid is not alive"
		}
		return info
	}
	if lockHeld {
		info.State = AgentRunning
		info.Uptime = timefmt.Age(time.Since(session.StartedAt))
		return info
	}
	info.State = AgentStopped
	info.Warning = "agent.json present but utsusemi is not running"
	return info
}

func collectJobs(state AgentState, session *lease.AgentSession, leases []lease.Lease, now time.Time) []Job {
	jobs := make([]Job, 0, len(leases))
	for i := range leases {
		l := leases[i]
		stale := false
		agentID := ""
		switch state {
		case AgentRunning:
			if session == nil || lease.IsStale(&l, session) {
				continue
			}
		default:
			stale = session == nil || lease.IsStale(&l, session)
			if stale && l.AgentID != "" {
				agentID = l.AgentID
			}
		}
		jobs = append(jobs, Job{
			VMName:   l.VMName,
			RunnerID: l.RunnerID,
			Age:      timefmt.Age(now.Sub(l.StartedAt)),
			Stale:    stale,
			AgentID:  agentID,
		})
	}
	return jobs
}

func summarizeVMs(vms []provider.VM, poolSize int, jobs []Job, agentState AgentState, leases []lease.Lease) (VMsInfo, []string) {
	info := VMsInfo{PoolSize: poolSize}
	leased := make(map[string]struct{}, len(leases))
	for _, l := range leases {
		leased[l.VMName] = struct{}{}
	}
	active := make(map[string]struct{}, len(jobs))
	if agentState == AgentRunning {
		for _, job := range jobs {
			active[job.VMName] = struct{}{}
		}
	}
	var draining []string
	for _, vm := range vms {
		info.Total++
		if !vm.Running {
			continue
		}
		info.Running++
		if agentState != AgentRunning {
			continue
		}
		if _, ok := active[vm.Name]; ok {
			continue
		}
		if _, ok := leased[vm.Name]; ok {
			continue
		}
		draining = append(draining, vm.Name)
	}
	return info, draining
}

func emptyJobs(jobs []Job) []Job {
	if jobs == nil {
		return []Job{}
	}
	return jobs
}

func emptyDraining(draining []string) []string {
	if draining == nil {
		return []string{}
	}
	return draining
}
