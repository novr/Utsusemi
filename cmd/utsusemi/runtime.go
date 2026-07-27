package main

import (
	"context"

	"github.com/novr/utsusemi/internal/agent"
	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/logging"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

type runtime struct {
	cfg       *config.Config
	tgt       target.Target
	provider  provider.VMProvider
	registrar registrar.RunnerRegistrar
}

func loadValidatedRuntime(ctx context.Context) (*runtime, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	vmProvider := provider.NewTartProvider(provider.RealExecutor{})
	tgt, err := config.Validate(cfg, vmProvider.Capabilities().MaxConcurrent)
	if err != nil {
		return nil, err
	}

	store := keychain.New()
	reg, err := registrar.NewFromConfig(cfg, store)
	if err != nil {
		return nil, err
	}
	if err := reg.ValidateCredential(ctx, cfg.CredentialService(), cfg.CredentialAccount()); err != nil {
		return nil, err
	}

	return &runtime{
		cfg:       cfg,
		tgt:       tgt,
		provider:  vmProvider,
		registrar: reg,
	}, nil
}

func buildAgent(ctx context.Context) (*agent.Agent, error) {
	rt, err := loadValidatedRuntime(ctx)
	if err != nil {
		return nil, err
	}
	log := logging.New()
	return agent.New(agent.Options{
		Config:    rt.cfg,
		Target:    rt.tgt,
		Provider:  rt.provider,
		Registrar: rt.registrar,
		Logger:    log,
	})
}

func mustCredentialService(cfg *config.Config) string {
	if cfg.Registration.CredentialKeychainService == "" {
		return config.DefaultCredentialService
	}
	return cfg.Registration.CredentialKeychainService
}

func saveCredential(cfg *config.Config, secret string) error {
	store := keychain.New()
	return store.Set(mustCredentialService(cfg), cfg.CredentialAccount(), secret)
}
