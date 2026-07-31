package main

import (
	"context"

	"github.com/novr/utsusemi/internal/agent"
	"github.com/novr/utsusemi/internal/app"
	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
)

func loadConfigRuntime(ctx context.Context) (*app.Runtime, error) {
	return app.Load(ctx, app.LoadOptions{ConfigPath: configPath})
}

func loadValidatedRuntime(ctx context.Context) (*app.Runtime, error) {
	return app.LoadValidated(ctx, app.LoadOptions{ConfigPath: configPath})
}

func buildAgentFromRuntime(rt *app.Runtime) (*agent.Agent, error) {
	return rt.Agent()
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
