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

func (s *Spawner) Run(ctx context.Context, vmName string) (Result, error) {
	cfg := s.opts.Config
	log := s.opts.Logger.With("vm", vmName)
	spawnStart := time.Now()
	metrics := LastSpawn{
		At:            spawnStart.UTC(),
		VMName:        vmName,
		RunnerVersion: cfg.RunnerVersion,
	}
	defer func() {
		if err := SaveLastSpawn(cfg.StateDir, metrics); err != nil {
			log.Warn("save spawn metrics failed", "error", err)
		}
	}()

	freeGB, err := s.opts.Provider.FreeDiskGB(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("disk check: %w", err)
	}
	if freeGB < float64(cfg.MinFreeDiskGB) {
		return Result{}, fmt.Errorf("insufficient disk: %.1fGB free, need %dGB", freeGB, cfg.MinFreeDiskGB)
	}

	spawnCtx, cancel := context.WithTimeout(ctx, cfg.SpawnTimeout.Duration())
	defer cancel()

	phase := time.Now()
	log.Info("cloning base image", "image", cfg.BaseImage)
	if err := s.opts.Provider.Clone(spawnCtx, cfg.BaseImage, vmName); err != nil {
		return Result{}, fmt.Errorf("clone: %w", err)
	}
	metrics.CloneMs = time.Since(phase).Milliseconds()
	log.Info("spawn phase complete", "phase", "clone", "duration_ms", metrics.CloneMs)
	defer func() {
		_ = s.opts.Provider.Delete(context.Background(), vmName)
	}()

	phase = time.Now()
	log.Info("starting vm")
	if err := s.opts.Provider.Start(spawnCtx, vmName); err != nil {
		return Result{}, fmt.Errorf("start: %w", err)
	}
	if err := waitForRunning(spawnCtx, s.opts.Provider, vmName); err != nil {
		return Result{}, fmt.Errorf("wait for vm: %w", err)
	}
	metrics.BootMs = time.Since(phase).Milliseconds()
	log.Info("spawn phase complete", "phase", "boot", "duration_ms", metrics.BootMs)

	phase = time.Now()
	log.Info("registering runner with GitHub")
	jit, err := s.opts.Registrar.CreateJIT(spawnCtx, s.opts.Target, cfg.Labels, vmName)
	if err != nil {
		return Result{}, fmt.Errorf("create jit: %w", err)
	}
	metrics.RegisterMs = time.Since(phase).Milliseconds()
	log.Info("spawn phase complete", "phase", "register", "duration_ms", metrics.RegisterMs)
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

	log.Info("waiting for job", "runner_id", runnerID, "runner_version", cfg.RunnerVersion,
		"cold_start_ms", metrics.CloneMs+metrics.BootMs+metrics.RegisterMs)
	env := BootstrapEnv(cfg, s.opts.Provider)
	execDone := make(chan error, 1)
	jobPhase := time.Now()
	go func() {
		execDone <- s.opts.Provider.ExecStdin(jobCtx, vmName, "bash", []string{"-c", bootstrapScript}, []byte(jit.Encoded), env)
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-jobCtx.Done():
			if errors.Is(jobCtx.Err(), context.Canceled) {
				log.Info("abandoning idle runner on shutdown")
			} else {
				log.Warn("job timeout reached")
			}
			_ = s.opts.Provider.Stop(context.Background(), vmName)
			return Result{}, jobCtx.Err()
		case err := <-execDone:
			metrics.JobMs = time.Since(jobPhase).Milliseconds()
			log.Info("spawn phase complete", "phase", "job", "duration_ms", metrics.JobMs,
				"total_ms", time.Since(spawnStart).Milliseconds())
			if err != nil {
				return Result{}, fmt.Errorf("runner exec: %w", err)
			}
			runnerRegistered = false
			metrics.Success = true
			return resultFromMetrics(metrics), nil
		case <-ticker.C:
			running, err := s.opts.Provider.IsRunning(jobCtx, vmName)
			if err != nil {
				log.Warn("vm state check failed", "error", err)
				continue
			}
			if !running {
				select {
				case err := <-execDone:
					metrics.JobMs = time.Since(jobPhase).Milliseconds()
					if err != nil && !errors.Is(err, context.Canceled) {
						return Result{}, fmt.Errorf("runner exec: %w", err)
					}
					runnerRegistered = false
					metrics.Success = true
					return resultFromMetrics(metrics), nil
				case <-time.After(30 * time.Second):
					return Result{}, fmt.Errorf("vm stopped before runner exec finished")
				case <-jobCtx.Done():
					return Result{}, jobCtx.Err()
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
