package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/logging"
	"github.com/novr/utsusemi/internal/pool"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

type Options struct {
	Config    *config.Config
	Target    target.Target
	Provider  provider.VMProvider
	Registrar registrar.RunnerRegistrar
	Logger    *slog.Logger
}

type Agent struct {
	cfg      *config.Config
	tgt      target.Target
	provider provider.VMProvider
	pool     *pool.Pool
	logger   *slog.Logger
}

func New(opts Options) (*Agent, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if opts.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = logging.New()
	}
	return &Agent{
		cfg:      opts.Config,
		tgt:      opts.Target,
		provider: opts.Provider,
		pool:     pool.New(opts.Config, opts.Target, opts.Provider, opts.Registrar, logger),
		logger:   logger,
	}, nil
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.provider.SyncImage(ctx, a.cfg.BaseImage); err != nil {
		a.logger.Warn("image sync failed", "error", err)
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		a.logger.Info("shutdown signal received, draining in-flight jobs")
		a.pool.BeginShutdown()
	}()

	a.logger.Info("agent started", "target", a.tgt.String(), "pool_size", a.cfg.PoolSize)
	return a.pool.Run(ctx)
}
