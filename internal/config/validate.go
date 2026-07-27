package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/novr/utsusemi/internal/target"
)

func Validate(cfg *Config, maxConcurrent int) (target.Target, error) {
	if cfg == nil {
		return target.Target{}, fmt.Errorf("config is nil")
	}
	applyDefaults(cfg)
	if cfg.Provider != "tart" {
		return target.Target{}, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
	if strings.TrimSpace(cfg.BaseImage) == "" {
		return target.Target{}, fmt.Errorf("base_image is required")
	}
	if strings.TrimSpace(cfg.RunnerVersion) == "" {
		return target.Target{}, fmt.Errorf("runner_version is required")
	}
	if cfg.PoolSize <= 0 {
		return target.Target{}, fmt.Errorf("pool_size must be positive")
	}
	if cfg.PoolSize > maxConcurrent {
		return target.Target{}, fmt.Errorf("pool_size %d exceeds provider max concurrent %d", cfg.PoolSize, maxConcurrent)
	}
	if !slices.Contains(cfg.Labels, "self-hosted") {
		return target.Target{}, fmt.Errorf("labels must include self-hosted")
	}
	if err := validateRegistration(cfg.Registration); err != nil {
		return target.Target{}, err
	}
	if cfg.SpawnTimeout.Duration() > cfg.JobTimeout.Duration() {
		return target.Target{}, fmt.Errorf("spawn_timeout must not exceed job_timeout")
	}
	if cfg.PoolCheckInterval.Duration() <= 0 {
		return target.Target{}, fmt.Errorf("pool_check_interval must be positive")
	}
	if cfg.ReconciliationInterval.Duration() <= 0 {
		return target.Target{}, fmt.Errorf("reconciliation_interval must be positive")
	}
	switch cfg.ReclaimPolicy {
	case ReclaimSoft, ReclaimGrace, ReclaimHard:
	default:
		return target.Target{}, fmt.Errorf("unsupported reclaim_policy %q", cfg.ReclaimPolicy)
	}
	if cfg.ReclaimGrace.Duration() <= 0 {
		return target.Target{}, fmt.Errorf("reclaim_grace must be positive")
	}
	tgt, err := target.FromConfig(cfg.Target)
	if err != nil {
		return target.Target{}, err
	}
	return tgt, tgt.Validate()
}

func validateRegistration(reg Registration) error {
	switch reg.Mode {
	case ModeGitHubPAT:
		return nil
	case ModeOwnApp, ModeHostedApp:
		if strings.TrimSpace(reg.BrokerURL) == "" {
			return fmt.Errorf("registration.broker_url is required for %s", reg.Mode)
		}
		return nil
	default:
		return fmt.Errorf("unsupported registration.mode %q", reg.Mode)
	}
}
