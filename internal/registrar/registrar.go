package registrar

import (
	"context"
	"fmt"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

type JITConfig struct {
	Encoded string
	Runner  Runner
}

type Runner struct {
	ID   int64
	Name string
}

type RunnerRegistrar interface {
	CreateJIT(ctx context.Context, tgt target.Target, labels []string, name string) (JITConfig, error)
	DeleteRunner(ctx context.Context, tgt target.Target, runnerID int64) error
	ListRunners(ctx context.Context, tgt target.Target, prefix string) ([]Runner, error)
	ValidateCredential(ctx context.Context, service, account string) error
}

func NewFromConfig(cfg *config.Config, store keychain.Store) (RunnerRegistrar, error) {
	switch cfg.Registration.Mode {
	case config.ModeGitHubPAT:
		return NewGitHubPATRegistrar(store, cfg.CredentialService(), cfg.CredentialAccount()), nil
	case config.ModeHostedApp:
		return NewBrokerRegistrar(store, cfg), nil
	default:
		return nil, fmt.Errorf("unsupported registration.mode %q", cfg.Registration.Mode)
	}
}
