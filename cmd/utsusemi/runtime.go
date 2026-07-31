package main

import (
	"context"
	"log/slog"

	"github.com/novr/utsusemi/internal/agent"
	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/listing"
	"github.com/novr/utsusemi/internal/logging"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/status"
	"github.com/novr/utsusemi/internal/target"
)

type runtime struct {
	cfg       *config.Config
	tgt       target.Target
	provider  provider.VMProvider
	registrar registrar.RunnerRegistrar
	logger    *slog.Logger
}

func loadConfigRuntime(ctx context.Context) (*runtime, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	vmProvider := provider.NewTartProvider(provider.RealExecutor{}, cfg.Softnet)
	if err := vmProvider.Available(); err != nil {
		return nil, err
	}
	tgt, err := config.Validate(cfg, vmProvider.Capabilities().MaxConcurrent)
	if err != nil {
		return nil, err
	}

	log := logging.New()
	store := keychain.New()
	reg, err := registrar.NewFromConfig(cfg, store, log)
	if err != nil {
		return nil, err
	}

	return &runtime{
		cfg:       cfg,
		tgt:       tgt,
		provider:  vmProvider,
		registrar: reg,
		logger:    log,
	}, nil
}

func loadValidatedRuntime(ctx context.Context) (*runtime, error) {
	rt, err := loadConfigRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if err := rt.registrar.ValidateCredential(ctx, rt.cfg.CredentialService(), rt.cfg.CredentialAccount()); err != nil {
		return nil, err
	}
	return rt, nil
}

func (rt *runtime) statusInput(store keychain.Store) status.Input {
	return status.Input{
		Cfg:      rt.cfg,
		Target:   rt.tgt,
		Provider: rt.provider,
		Store:    store,
	}
}

func (rt *runtime) listingInput(scope string) listing.Input {
	return listing.Input{
		Cfg:       rt.cfg,
		Target:    rt.tgt,
		Provider:  rt.provider,
		Registrar: rt.registrar,
		Scope:     scope,
	}
}

func buildAgentFromRuntime(rt *runtime) (*agent.Agent, error) {
	return agent.New(agent.Options{
		Config:    rt.cfg,
		Target:    rt.tgt,
		Provider:  rt.provider,
		Registrar: rt.registrar,
		Logger:    rt.logger,
	})
}

func buildAgent(ctx context.Context) (*agent.Agent, error) {
	rt, err := loadValidatedRuntime(ctx)
	if err != nil {
		return nil, err
	}
	return buildAgentFromRuntime(rt)
}

func saveCredential(cfg *config.Config, secret string) error {
	store := keychain.New()
	return store.Set(cfg.CredentialService(), cfg.CredentialAccount(), secret)
}

func providerMaxConcurrent(cfg *config.Config) int {
	softnet := false
	if cfg != nil {
		softnet = cfg.Softnet
	}
	return provider.NewTartProvider(provider.RealExecutor{}, softnet).Capabilities().MaxConcurrent
}
