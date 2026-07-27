package spawn

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/lease"
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
	Leases    *lease.Registry
	Logger    *slog.Logger
}

type Spawner struct {
	opts    Options
	session *lease.AgentSession
}

func New(opts Options) *Spawner {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Spawner{opts: opts}
}

func (s *Spawner) SetSession(session *lease.AgentSession) {
	s.session = session
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
	if err := waitForRunning(spawnCtx, s.opts.Provider, vmName); err != nil {
		return fmt.Errorf("wait for vm: %w", err)
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

	if s.opts.Leases != nil && s.session != nil {
		if err := s.opts.Leases.WriteLease(s.session, lease.Lease{
			VMName:   vmName,
			RunnerID: runnerID,
		}); err != nil {
			log.Warn("write lease failed", "error", err)
		}
		defer func() {
			_ = s.opts.Leases.RemoveLease(vmName)
		}()
	}

	jobCtx, jobCancel := context.WithTimeout(ctx, cfg.JobTimeout.Duration())
	defer jobCancel()

	env := map[string]string{"RUNNER_VERSION": cfg.RunnerVersion}
	execDone := make(chan error, 1)
	go func() {
		execDone <- s.opts.Provider.ExecStdin(jobCtx, vmName, "bash", []string{"-c", bootstrapScript}, []byte(jit.Encoded), env)
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-jobCtx.Done():
			log.Warn("job timeout reached")
			_ = s.opts.Provider.Stop(context.Background(), vmName)
			return jobCtx.Err()
		case err := <-execDone:
			if err != nil {
				return fmt.Errorf("runner exec: %w", err)
			}
			runnerRegistered = false
			return nil
		case <-ticker.C:
			running, err := s.opts.Provider.IsRunning(jobCtx, vmName)
			if err != nil {
				log.Warn("vm state check failed", "error", err)
				continue
			}
			if !running {
				select {
				case err := <-execDone:
					if err != nil && !errors.Is(err, context.Canceled) {
						return fmt.Errorf("runner exec: %w", err)
					}
					runnerRegistered = false
					return nil
				case <-time.After(30 * time.Second):
					return fmt.Errorf("vm stopped before runner exec finished")
				case <-jobCtx.Done():
					return jobCtx.Err()
				}
			}
		}
	}
}

func waitForRunning(ctx context.Context, vmProvider provider.VMProvider, name string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		running, err := vmProvider.IsRunning(ctx, name)
		if err != nil {
			return err
		}
		if running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
