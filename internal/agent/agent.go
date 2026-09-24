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
	"github.com/novr/utsusemi/internal/runnercache"
	"github.com/novr/utsusemi/internal/target"
)

type Options struct {
	Config      *config.Config
	Target      target.Target
	Provider    provider.VMProvider
	Registrar   registrar.RunnerRegistrar
	Logger      *slog.Logger
	LogFilePath string
}

type Agent struct {
	cfg         *config.Config
	tgt         target.Target
	provider    provider.VMProvider
	pool        *pool.Pool
	logger      *slog.Logger
	logFilePath string
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
		var err error
		logger, err = logging.New(logging.Options{})
		if err != nil {
			return nil, err
		}
	}
	return &Agent{
		cfg:         opts.Config,
		tgt:         opts.Target,
		provider:    opts.Provider,
		pool:        pool.New(opts.Config, opts.Target, opts.Provider, opts.Registrar, logger),
		logger:      logger,
		logFilePath: opts.LogFilePath,
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

	if err := runnercache.PrepareDir(a.cfg.StateDir); err != nil {
		a.logger.Warn("prepare runner cache dir failed", "error", err)
	} else {
		arch := a.provider.Capabilities().RunnerArch
		if arch == "" {
			arch = "osx-arm64"
		}
		if err := runnercache.Ensure(ctx, a.cfg.StateDir, a.cfg.RunnerVersion, arch, runnercache.Options{Logger: a.logger}); err != nil {
			a.logger.Warn("ensure runner cache failed; bootstrap may download inside the VM", "error", err)
		}
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		a.logger.Info("shutdown signal received, draining in-flight jobs")
		a.pool.BeginShutdown()
	}()

	startAttrs := []any{
		"target", a.tgt.String(),
		"pool_size", a.cfg.PoolSize,
		"labels", a.cfg.Labels,
		"state_dir", a.cfg.StateDir,
		"pool_check_interval", a.cfg.PoolCheckInterval.Duration().String(),
	}
	if a.logFilePath != "" {
		startAttrs = append(startAttrs, "log_file", a.logFilePath)
	}
	a.logger.Info("agent started", startAttrs...)
	return a.pool.Run(ctx)
}

func (a *Agent) PurgeAll(ctx context.Context, dryRun bool) ([]provider.VM, []int64, error) {
	return a.pool.PurgeAll(ctx, dryRun)
}
