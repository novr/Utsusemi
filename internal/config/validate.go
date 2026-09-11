package config

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/target"
)

func validateRegistration(reg Registration) error {
	switch reg.Mode {
	case ModeGitHubPAT:
		return nil
	case ModeHostedApp:
		if strings.TrimSpace(reg.BrokerURL) == "" {
			return fmt.Errorf("registration.broker_url is required for %s", reg.Mode)
		}
		return ValidateBrokerURL(reg.BrokerURL)
	default:
		return fmt.Errorf("unsupported registration.mode %q", reg.Mode)
	}
}

func Validate(cfg *Config, p provider.VMProvider) (target.Target, error) {
	if cfg == nil {
		return target.Target{}, fmt.Errorf("config is nil")
	}
	if p == nil {
		return target.Target{}, fmt.Errorf("provider is nil")
	}
	applyDefaults(cfg)
	if strings.TrimSpace(cfg.Provider) == "" {
		cfg.Provider = "tart"
	}
	maxConcurrent := p.Capabilities().MaxConcurrent
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
		return target.Target{}, fmt.Errorf("pool_size %d exceeds %s provider max concurrent %d", cfg.PoolSize, cfg.Provider, maxConcurrent)
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
	if err := tgt.Validate(); err != nil {
		return target.Target{}, err
	}
	if cfg.Registration.Mode == ModeHostedApp && tgt.Type != target.TypeOrg {
		return target.Target{}, fmt.Errorf("hosted_app requires an organization target")
	}
	return tgt, nil
}

// ValidateBrokerURL checks hosted app broker URLs (https, or loopback http).
func ValidateBrokerURL(brokerURL string) error {
	u, err := url.Parse(strings.TrimSpace(brokerURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("registration.broker_url must use https")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "127.0.0.1" {
			return nil
		}
		if strings.EqualFold(host, "localhost") {
			return fmt.Errorf("registration.broker_url must use http://127.0.0.1 for a local broker (not localhost)")
		}
		return fmt.Errorf("registration.broker_url must use https")
	default:
		return fmt.Errorf("registration.broker_url must use https")
	}
}
