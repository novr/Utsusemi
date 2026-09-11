package app

import (
	"context"
	"log/slog"

	"github.com/novr/utsusemi/internal/agent"
	"github.com/novr/utsusemi/internal/brokerhttp"
	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/doctor"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/listing"
	"github.com/novr/utsusemi/internal/logging"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/status"
	"github.com/novr/utsusemi/internal/target"
)

type Runtime struct {
	Config     *config.Config
	Target     target.Target
	Provider   provider.VMProvider
	Registrar  registrar.RunnerRegistrar
	Logger     *slog.Logger
	LogFilePath string
}

type LoadOptions struct {
	ConfigPath      string
	Exec            provider.CommandExecutor
	Logger          *slog.Logger
	LogFile         string
	ResolveAgentLog bool
}

func Load(ctx context.Context, opts LoadOptions) (*Runtime, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	exec := opts.Exec
	if exec == nil {
		exec = provider.RealExecutor{}
	}

	vmProvider, err := newProvider(cfg, exec)
	if err != nil {
		return nil, err
	}
	tgt, err := config.Validate(cfg, vmProvider)
	if err != nil {
		return nil, err
	}

	log := opts.Logger
	logFilePath := ""
	if log == nil {
		logPath := ""
		if opts.ResolveAgentLog {
			logPath, err = logging.ResolveLogFile(opts.LogFile, cfg.StateDir)
			if err != nil {
				return nil, err
			}
			logFilePath = logPath
		}
		log, err = logging.New(logging.Options{LogFile: logPath})
		if err != nil {
			return nil, err
		}
	}
	store := keychain.New()
	reg, err := registrar.NewFromConfig(cfg, store, log)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		Config:      cfg,
		Target:      tgt,
		Provider:    vmProvider,
		Registrar:   reg,
		Logger:      log,
		LogFilePath: logFilePath,
	}, nil
}

func LoadValidated(ctx context.Context, opts LoadOptions) (*Runtime, error) {
	if opts.ResolveAgentLog {
		savedLogFile := opts.LogFile
		opts.ResolveAgentLog = false
		opts.LogFile = ""
		rt, err := Load(ctx, opts)
		if err != nil {
			return nil, err
		}
		if err := rt.ValidateCredential(ctx); err != nil {
			return nil, err
		}
		logPath, err := logging.ResolveLogFile(savedLogFile, rt.Config.StateDir)
		if err != nil {
			return nil, err
		}
		logger, err := logging.New(logging.Options{LogFile: logPath})
		if err != nil {
			return nil, err
		}
		store := keychain.New()
		reg, err := registrar.NewFromConfig(rt.Config, store, logger)
		if err != nil {
			return nil, err
		}
		rt.Logger = logger
		rt.Registrar = reg
		rt.LogFilePath = logPath
		return rt, nil
	}

	rt, err := Load(ctx, opts)
	if err != nil {
		return nil, err
	}
	if rt.Config.Registration.Mode == config.ModeHostedApp {
		if err := brokerhttp.CheckReachable(ctx, rt.Config.Registration.BrokerURL); err != nil {
			return nil, err
		}
	}
	if err := rt.ValidateCredential(ctx); err != nil {
		return nil, err
	}
	return rt, nil
}

func (r *Runtime) ValidateCredential(ctx context.Context) error {
	return r.Registrar.ValidateCredential(ctx, r.Config.CredentialService(), r.Config.CredentialAccount())
}

func (r *Runtime) StatusInput(store keychain.Store) status.Input {
	return status.Input{
		Cfg:      r.Config,
		Target:   r.Target,
		Provider: r.Provider,
		Store:    store,
	}
}

func (r *Runtime) ListingInput(scope string) listing.Input {
	return listing.Input{
		Cfg:       r.Config,
		Target:    r.Target,
		Provider:  r.Provider,
		Registrar: r.Registrar,
		Scope:     scope,
	}
}

func (r *Runtime) DoctorInput(store keychain.Store) doctor.Input {
	return doctor.Input{
		Cfg:       r.Config,
		Target:    r.Target,
		Provider:  r.Provider,
		Registrar: r.Registrar,
		Store:     store,
	}
}

func (r *Runtime) Agent() (*agent.Agent, error) {
	return agent.New(agent.Options{
		Config:      r.Config,
		Target:      r.Target,
		Provider:    r.Provider,
		Registrar:   r.Registrar,
		Logger:      r.Logger,
		LogFilePath: r.LogFilePath,
	})
}
