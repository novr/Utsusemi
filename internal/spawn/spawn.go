package spawn

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

//go:embed bootstrap.sh
var bootstrapScript string

type Options struct {
	Config    *config.Config
	Target    target.Target
	Provider  provider.VMProvider
	Registrar registrar.RunnerRegistrar
	Logger    *slog.Logger
}

type Spawner struct {
	opts Options
}

func New(opts Options) *Spawner {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Spawner{opts: opts}
}

func (s *Spawner) Run(ctx context.Context, vmName string) error {
	cfg := s.opts.Config
	log := s.opts.Logger.With("vm", vmName)

	freeGB, err := s.opts.Provider.FreeDiskGB(ctx)
	if err != nil {
		return fmt.Errorf("disk check: %w", err)
	}
	if freeGB < float64(cfg.MinFreeDiskGB) {
		return fmt.Errorf("insufficient disk: %.1fGB free, need %dGB", freeGB, cfg.MinFreeDiskGB)
	}

	spawnCtx, cancel := context.WithTimeout(ctx, cfg.SpawnTimeout.Duration())
	defer cancel()

	if err := s.opts.Provider.Clone(spawnCtx, cfg.BaseImage, vmName); err != nil {
		return fmt.Errorf("clone: %w", err)
	}
	defer func() {
		_ = s.opts.Provider.Delete(context.Background(), vmName)
	}()

	if err := s.opts.Provider.Start(spawnCtx, vmName); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	jit, err := s.opts.Registrar.CreateJIT(spawnCtx, s.opts.Target, cfg.Labels, vmName)
	if err != nil {
		return fmt.Errorf("create jit: %w", err)
	}
	runnerID := jit.Runner.ID
	runnerRegistered := true
	defer func() {
		if runnerRegistered {
			_ = s.opts.Registrar.DeleteRunner(context.Background(), s.opts.Target, runnerID)
		}
	}()

	env := map[string]string{"RUNNER_VERSION": cfg.RunnerVersion}
	execDone := make(chan error, 1)
	go func() {
		execDone <- s.opts.Provider.ExecStdin(spawnCtx, vmName, "bash", []string{"-c", bootstrapScript}, []byte(jit.Encoded), env)
	}()

	jobCtx, jobCancel := context.WithTimeout(ctx, cfg.JobTimeout.Duration())
	defer jobCancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var execErr error
	completed := false
	for !completed {
		select {
		case <-jobCtx.Done():
			log.Warn("job timeout reached")
			_ = s.opts.Provider.Stop(context.Background(), vmName)
			return jobCtx.Err()
		case err := <-execDone:
			execErr = err
			completed = true
		case <-ticker.C:
			running, err := s.opts.Provider.IsRunning(jobCtx, vmName)
			if err != nil {
				log.Warn("vm state check failed", "error", err)
				continue
			}
			if !running {
				log.Info("vm stopped")
				completed = true
			}
		}
	}

	if execErr != nil && !strings.Contains(execErr.Error(), "context canceled") {
		return fmt.Errorf("runner exec: %w", execErr)
	}

	runnerRegistered = false
	return nil
}
