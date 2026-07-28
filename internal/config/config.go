package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/novr/utsusemi/internal/target"
)

const (
	ModeGitHubPAT = "github_pat"
	ModeHostedApp = "hosted_app"

	ReclaimSoft = "soft"
	ReclaimGrace = "grace"
	ReclaimHard = "hard"

	DefaultPoolCheckInterval      = 30 * time.Second
	DefaultReconciliationInterval = 5 * time.Minute
	DefaultSpawnTimeout           = 10 * time.Minute
	DefaultJobTimeout             = 6 * time.Hour
	DefaultMinFreeDiskGB          = 50
	DefaultVMNamePrefix           = "utsusemi-"
	DefaultReclaimPolicy          = ReclaimSoft
	DefaultReclaimGrace           = 15 * time.Minute
	DefaultCredentialService      = "utsusemi-credential"
	DefaultHostedAppBrokerURL     = "https://utsusemi-broker.novrd.workers.dev"
	DefaultCredentialAccount      = "utsusemi"
)

type Duration time.Duration

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a scalar")
	}
	parsed, err := time.ParseDuration(value.Value)
	if err == nil {
		*d = Duration(parsed)
		return nil
	}
	// Older writers emitted raw nanoseconds without a unit.
	var ns int64
	if _, scanErr := fmt.Sscan(value.Value, &ns); scanErr == nil {
		*d = Duration(ns)
		return nil
	}
	return err
}

type Registration struct {
	Mode                    string `yaml:"mode"`
	BrokerURL               string `yaml:"broker_url,omitempty"`
	CredentialKeychainService string `yaml:"credential_keychain_service,omitempty"`
}

type Config struct {
	Target                 target.ConfigYAML `yaml:"target"`
	Labels                 []string          `yaml:"labels"`
	Registration           Registration      `yaml:"registration"`
	Provider               string            `yaml:"provider"`
	BaseImage              string            `yaml:"base_image"`
	RunnerVersion          string            `yaml:"runner_version"`
	PoolSize               int               `yaml:"pool_size"`
	PoolCheckInterval      Duration          `yaml:"pool_check_interval"`
	ReconciliationInterval Duration          `yaml:"reconciliation_interval"`
	SpawnTimeout           Duration          `yaml:"spawn_timeout"`
	JobTimeout             Duration          `yaml:"job_timeout"`
	MinFreeDiskGB          int               `yaml:"min_free_disk_gb"`
	VMNamePrefix           string            `yaml:"vm_name_prefix"`
	StateDir               string            `yaml:"state_dir,omitempty"`
	Softnet                bool              `yaml:"softnet,omitempty"`
	ReclaimPolicy          string            `yaml:"reclaim_policy,omitempty"`
	ReclaimGrace           Duration          `yaml:"reclaim_grace,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(cfg)
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.PoolCheckInterval == 0 {
		cfg.PoolCheckInterval = Duration(DefaultPoolCheckInterval)
	}
	if cfg.ReconciliationInterval == 0 {
		cfg.ReconciliationInterval = Duration(DefaultReconciliationInterval)
	}
	if cfg.SpawnTimeout == 0 {
		cfg.SpawnTimeout = Duration(DefaultSpawnTimeout)
	}
	if cfg.JobTimeout == 0 {
		cfg.JobTimeout = Duration(DefaultJobTimeout)
	}
	if cfg.MinFreeDiskGB == 0 {
		cfg.MinFreeDiskGB = DefaultMinFreeDiskGB
	}
	if cfg.VMNamePrefix == "" {
		cfg.VMNamePrefix = DefaultVMNamePrefix
	}
	if cfg.StateDir == "" {
		cfg.StateDir = defaultStateDir()
	}
	if cfg.ReclaimPolicy == "" {
		cfg.ReclaimPolicy = DefaultReclaimPolicy
	}
	if cfg.ReclaimGrace == 0 {
		cfg.ReclaimGrace = Duration(DefaultReclaimGrace)
	}
	if cfg.Registration.CredentialKeychainService == "" {
		cfg.Registration.CredentialKeychainService = DefaultCredentialService
	}
}

func (c *Config) CredentialService() string {
	return c.Registration.CredentialKeychainService
}

func (c *Config) CredentialAccount() string {
	return DefaultCredentialAccount
}

func ApplyDefaults(cfg *Config) {
	applyDefaults(cfg)
}

func defaultStateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "utsusemi")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "utsusemi")
	}
	return filepath.Join(home, ".local", "state", "utsusemi")
}

func TargetYAML(org, repo string, runnerGroup int64) target.ConfigYAML {
	return target.ConfigYAML{
		Org:           org,
		Repo:          repo,
		RunnerGroupID: runnerGroup,
	}
}
