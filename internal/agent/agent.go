package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/instancelock"
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
	lock, err := instancelock.Acquire(filepath.Join(a.cfg.StateDir, "utsusemi.lock"))
	if err != nil {
		return err
	}
	defer lock.Release()

	a.logger.Info("syncing base image", "image", a.cfg.BaseImage, "note", "first download can take several minutes")
	if err := a.provider.SyncImage(ctx, a.cfg.BaseImage); err != nil {
		return fmt.Errorf("sync base image: %w", err)
	}
	a.logger.Info("base image ready", "image", a.cfg.BaseImage)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		a.logger.Info("shutdown signal received, draining in-flight jobs")
		a.pool.BeginShutdown()
	}()

	a.logger.Info("agent started",
		"target", a.tgt.String(),
		"pool_size", a.cfg.PoolSize,
		"labels", a.cfg.Labels,
		"state_dir", a.cfg.StateDir,
		"pool_check_interval", a.cfg.PoolCheckInterval.Duration().String(),
	)
	return a.pool.Run(ctx)
}

func (a *Agent) ReclaimAll(ctx context.Context, dryRun bool) ([]provider.VM, []int64, error) {
	return a.pool.ReclaimAll(ctx, dryRun)
}
