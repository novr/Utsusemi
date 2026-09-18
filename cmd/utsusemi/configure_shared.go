package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/app"
	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/credentialview"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/runnerrelease"
)

type configureMode string

const (
	configureModeApp   configureMode = config.ModeHostedApp
	configureModeToken configureMode = config.ModeGitHubPAT
)

type configureTargetInput struct {
	Org         string
	Repo        string
	RunnerGroup int64
}

type configureAppInput struct {
	BrokerURL     string
	OAuthClientID string
}

type configureMergeInput struct {
	Mode       configureMode
	OutputPath string
	Target     configureTargetInput
	App        configureAppInput
	Opts       runnerOptions
}

type configureMergeResult struct {
	Config                 *config.Config
	ExistingConfig         bool
	OrgAuthChanged         bool
	BrokerAuthChanged      bool
	OAuthAuthChanged       bool
	RunnerGroupAuthChanged bool
}

func mergeConfigureConfig(cmd *cobra.Command, in configureMergeInput) (configureMergeResult, error) {
	if err := resolveRunnerVersionFlag(cmd.Context(), cmd, &in.Opts); err != nil {
		return configureMergeResult{}, err
	}

	existing, existingCfg, err := loadExistingConfig(in.OutputPath)
	if err != nil {
		return configureMergeResult{}, err
	}

	var cfg *config.Config
	if existing {
		cfg = existingCfg
		if err := ensureModeCompatible(cfg, string(in.Mode)); err != nil {
			return configureMergeResult{}, err
		}
	} else {
		cfg = &config.Config{
			Registration: config.Registration{Mode: string(in.Mode)},
		}
	}

	orgChanged := cmd.Flags().Changed("org")
	brokerChanged := cmd.Flags().Changed("broker")
	oauthChanged := cmd.Flags().Changed("oauth-client-id")
	repoChanged := cmd.Flags().Changed("repo")
	runnerGroupChanged := cmd.Flags().Changed("runner-group-id")

	prevOrg := cfg.Target.Org
	prevRunnerGroup := cfg.Target.RunnerGroupID
	prevBroker := cfg.Registration.BrokerURL
	prevOAuth := cfg.Registration.OAuthClientID

	if existing {
		if orgChanged {
			cfg.Target.Org = in.Target.Org
		}
		if repoChanged {
			cfg.Target.Repo = in.Target.Repo
		}
		if runnerGroupChanged {
			cfg.Target.RunnerGroupID = in.Target.RunnerGroup
		}
	} else {
		cfg.Target = config.TargetYAML(in.Target.Org, in.Target.Repo, in.Target.RunnerGroup)
	}

	orgAuthChanged := orgChanged && cfg.Target.Org != prevOrg
	runnerGroupAuthChanged := runnerGroupChanged && cfg.Target.RunnerGroupID != prevRunnerGroup
	brokerAuthChanged := false
	if in.Mode == configureModeApp {
		if existing && brokerChanged {
			cfg.Registration.BrokerURL = in.App.BrokerURL
		} else if !existing {
			cfg.Registration.BrokerURL = in.App.BrokerURL
		}
		brokerAuthChanged = brokerChanged && cfg.Registration.BrokerURL != prevBroker
		if existing && oauthChanged {
			cfg.Registration.OAuthClientID = in.App.OAuthClientID
		} else if !existing {
			cfg.Registration.OAuthClientID = in.App.OAuthClientID
		}
	} else {
		cfg.Registration.Mode = config.ModeGitHubPAT
	}

	if err := applyRunnerOptions(cmd, in.Opts, cfg, !existing); err != nil {
		return configureMergeResult{}, err
	}

	if !existing {
		cfg.Registration.Mode = string(in.Mode)
		if in.Mode == configureModeApp {
			cfg.Registration.BrokerURL = in.App.BrokerURL
			cfg.Registration.OAuthClientID = in.App.OAuthClientID
		}
	}

	oauthAuthChanged := oauthChanged && cfg.Registration.OAuthClientID != prevOAuth

	if err := validateConfigureTarget(cfg, existing, in.Mode); err != nil {
		return configureMergeResult{}, err
	}

	config.ApplyDefaults(cfg)
	if _, err := app.ValidateConfig(cfg); err != nil {
		return configureMergeResult{}, err
	}

	return configureMergeResult{
		Config:                 cfg,
		ExistingConfig:         existing,
		OrgAuthChanged:         orgAuthChanged,
		BrokerAuthChanged:      brokerAuthChanged,
		OAuthAuthChanged:       oauthAuthChanged,
		RunnerGroupAuthChanged: runnerGroupAuthChanged,
	}, nil
}

func loadExistingConfig(path string) (bool, *config.Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil, nil
		}
		return false, nil, err
	}
	if info.IsDir() {
		return false, nil, fmt.Errorf("config path %s is a directory", path)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return false, nil, err
	}
	return true, cfg, nil
}

func ensureModeCompatible(cfg *config.Config, mode string) error {
	if cfg.Registration.Mode == "" || cfg.Registration.Mode == mode {
		return nil
	}
	switch mode {
	case config.ModeGitHubPAT:
		return fmt.Errorf("registration.mode is %s; use configure app or configure edit before switching to github_pat", cfg.Registration.Mode)
	case config.ModeHostedApp:
		return fmt.Errorf("registration.mode is %s; use configure token or configure edit before switching to hosted_app", cfg.Registration.Mode)
	default:
		return fmt.Errorf("unsupported registration.mode %q", cfg.Registration.Mode)
	}
}

func validateConfigureTarget(cfg *config.Config, existing bool, mode configureMode) error {
	if existing {
		if cfg.Target.Org == "" && cfg.Target.Repo == "" {
			return fmt.Errorf("config has no target; pass --org or --repo")
		}
		return nil
	}
	switch mode {
	case configureModeToken:
		if cfg.Target.Org == "" && cfg.Target.Repo == "" {
			return fmt.Errorf("either --org or --repo is required")
		}
	case configureModeApp:
		if cfg.Target.Org == "" {
			return fmt.Errorf("--org is required")
		}
	}
	return nil
}

func applyRunnerOptions(cmd *cobra.Command, opts runnerOptions, cfg *config.Config, isNew bool) error {
	if isNew {
		opts.apply(cfg)
		return nil
	}
	if cmd.Flags().Changed("labels") {
		cfg.Labels = splitLabels(opts.labels)
	}
	if cmd.Flags().Changed("base-image") {
		cfg.BaseImage = opts.baseImage
	}
	if cmd.Flags().Changed("runner-version") {
		cfg.RunnerVersion = opts.runnerVer
	}
	if cmd.Flags().Changed("pool-size") {
		cfg.PoolSize = opts.poolSize
	}
	if cfg.Provider == "" {
		cfg.Provider = "tart"
	}
	return nil
}

func resolveRunnerVersionFlag(ctx context.Context, cmd *cobra.Command, opts *runnerOptions) error {
	if !cmd.Flags().Changed("runner-version") {
		return nil
	}
	if strings.TrimSpace(opts.runnerVer) != "latest" {
		return nil
	}
	version, err := runnerrelease.Latest(ctx, &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		return fmt.Errorf("resolve --runner-version latest: %w", err)
	}
	opts.runnerVer = version
	return nil
}

func needsAppDeviceFlow(cfg *config.Config, merge configureMergeResult) bool {
	if merge.OrgAuthChanged || merge.BrokerAuthChanged || merge.OAuthAuthChanged {
		return true
	}
	info, err := credentialview.Load(cfg, credentialStoreOrDefault())
	if err != nil {
		return true
	}
	return !info.Present
}

type configureSuccess struct {
	CredentialUpdated bool
	CredentialMissing bool
	GitHubUser        string
	WroteConfig       bool
}

var configureAppConfigFlagNames = []string{
	"broker", "oauth-client-id", "org", "runner-group-id",
	"labels", "base-image", "runner-version", "pool-size",
}

func configureAppConfigFlagsChanged(cmd *cobra.Command) bool {
	for _, name := range configureAppConfigFlagNames {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func configureOnlyCredentialRefresh(cmd *cobra.Command, refresh bool) bool {
	return refresh && !configureAppConfigFlagsChanged(cmd)
}

func ensureConfigureAppRefreshCompatible(path string) error {
	existing, cfg, err := loadExistingConfig(path)
	if err != nil {
		return err
	}
	if !existing || cfg.Registration.Mode == "" || cfg.Registration.Mode == config.ModeHostedApp {
		return nil
	}
	return fmt.Errorf(
		"configure app --refresh requires registration.mode %s (current: %s); renew credentials with configure token",
		config.ModeHostedApp, cfg.Registration.Mode,
	)
}

func credentialStoreOrDefault() keychain.Store {
	if credentialStore != nil {
		return credentialStore
	}
	return keychain.New()
}

var credentialStore keychain.Store

func configureOutputPath(cmd *cobra.Command, outputFlag string) string {
	if cmd.Flags().Changed("output") {
		return outputFlag
	}
	return configPath
}
