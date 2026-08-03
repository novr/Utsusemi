package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/credentialview"
	"github.com/novr/utsusemi/internal/hostid"
	"github.com/novr/utsusemi/internal/instancelock"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/spawn"
	"github.com/novr/utsusemi/internal/target"
)

type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Check struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message"`
}

type Report struct {
	Checks []Check `json:"checks"`
}

type Input struct {
	Cfg       *config.Config
	Target    target.Target
	Provider  provider.VMProvider
	Registrar registrar.RunnerRegistrar
	Store     keychain.Store
}

func Collect(ctx context.Context, in Input) Report {
	var checks []Check
	add := func(name string, status Status, msg string) {
		checks = append(checks, Check{Name: name, Status: status, Message: msg})
	}

	if in.Cfg == nil {
		add("config", StatusFail, "config is nil")
		return Report{Checks: checks}
	}
	if in.Provider == nil {
		add("provider", StatusFail, "provider is nil")
		return Report{Checks: checks}
	}
	if in.Registrar == nil {
		add("credential", StatusFail, "registrar is nil")
		return Report{Checks: checks}
	}

	add("config", StatusOK, "loaded")

	if err := in.Provider.Available(); err != nil {
		add("provider", StatusFail, err.Error())
	} else {
		caps := in.Provider.Capabilities()
		add("provider", StatusOK, fmt.Sprintf("available (max_concurrent=%d, runner_arch=%s)", caps.MaxConcurrent, caps.RunnerArch))
	}

	freeGB, err := in.Provider.FreeDiskGB(ctx)
	if err != nil {
		add("disk", StatusFail, err.Error())
	} else if freeGB < float64(in.Cfg.MinFreeDiskGB) {
		add("disk", StatusWarn, fmt.Sprintf("%.1f GB free (need %d GB)", freeGB, in.Cfg.MinFreeDiskGB))
	} else {
		add("disk", StatusOK, fmt.Sprintf("%.1f GB free", freeGB))
	}

	store := in.Store
	if store == nil {
		store = keychain.New()
	}
	if err := in.Registrar.ValidateCredential(ctx, in.Cfg.CredentialService(), in.Cfg.CredentialAccount()); err != nil {
		add("credential", StatusFail, err.Error())
	} else {
		cred, err := credentialview.Load(in.Cfg, store)
		if err != nil {
			add("credential", StatusWarn, err.Error())
		} else {
			add("credential", StatusOK, credentialview.StatusLine(cred))
		}
	}

	lockPath := filepath.Join(in.Cfg.StateDir, "utsusemi.lock")
	if instancelock.Held(lockPath) {
		add("agent_lock", StatusOK, "utsusemi is running")
	} else {
		add("agent_lock", StatusOK, "no running agent")
	}

	host := hostid.Collect(in.Cfg.StateDir, in.Cfg.VMNamePrefix)
	add("host_id", StatusOK, fmt.Sprintf("%s (prefix %s)", host.ID, host.EffectivePrefix))
	for _, w := range host.Warnings {
		add("hostname", StatusWarn, w)
	}
	if len(host.Warnings) == 0 {
		add("hostname", StatusOK, fmt.Sprintf("%s (LocalHostName: %s)", host.Hostname, orDash(host.LocalHostName)))
	}

	checkRunnerVersion(in.Cfg, add)
	checkMounts(in.Cfg, add)
	checkMultiHost(ctx, in, host, add)

	return Report{Checks: checks}
}

func checkMounts(cfg *config.Config, add func(string, Status, string)) {
	if len(cfg.Mounts) == 0 {
		return
	}
	dirs, err := provider.ResolveMountDirs(cfg.Mounts)
	if err != nil {
		add("mounts", StatusFail, err.Error())
		return
	}
	var missing []string
	for _, d := range dirs {
		path := provider.HostPathFromDir(d)
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		add("mounts", StatusWarn, fmt.Sprintf("%d configured; missing on host: %s", len(dirs), strings.Join(missing, ", ")))
		return
	}
	add("mounts", StatusOK, fmt.Sprintf("%d configured", len(dirs)))
}

func checkRunnerVersion(cfg *config.Config, add func(string, Status, string)) {
	snap := spawn.LoadRunnerVersionSnapshot(cfg.RunnerVersion, cfg.StateDir)
	if snap.Configured == "" {
		add("runner_version", StatusFail, "runner_version is empty")
		return
	}
	msg := "configured " + snap.Configured
	if snap.HasMetrics {
		msg += fmt.Sprintf("; last successful spawn used %s", snap.LastMetrics.RunnerVersion)
		if snap.Mismatch() {
			add("runner_version", StatusWarn, msg+"; rebuild the base image and restart the agent after changing runner_version")
			return
		}
		msg += fmt.Sprintf(" (cold_start %dms)", snap.LastMetrics.ColdStartMs)
	} else {
		msg += "; no successful spawn metrics yet"
	}
	add("runner_version", StatusOK, msg)
}

func checkMultiHost(ctx context.Context, in Input, host hostid.Info, add func(string, Status, string)) {
	if len(host.Warnings) > 0 {
		add("multi_host", StatusWarn, "fix hostname warnings before running multiple hosts against the same org")
		return
	}

	runners, err := in.Registrar.ListRunners(ctx, in.Target, in.Cfg.VMNamePrefix)
	if err != nil {
		add("multi_host", StatusWarn, "could not list runners: "+err.Error())
		return
	}

	ids := map[string]int{}
	for _, r := range runners {
		id, ok := hostid.ParseFromRunnerName(r.Name, in.Cfg.VMNamePrefix)
		if !ok {
			continue
		}
		ids[id]++
	}
	switch len(ids) {
	case 0:
		add("multi_host", StatusOK, "no managed runners under prefix")
	case 1:
		add("multi_host", StatusOK, fmt.Sprintf("%d runner(s) under this host prefix", len(runners)))
	default:
		add("multi_host", StatusOK, fmt.Sprintf("%d host identifier(s) and %d runner(s) under prefix", len(ids), len(runners)))
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func FailedCount(r Report) int {
	n := 0
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			n++
		}
	}
	return n
}
