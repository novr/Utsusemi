package pool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/hostid"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/spawn"
	"github.com/novr/utsusemi/internal/target"
)

type Pool struct {
	cfg             *config.Config
	tgt             target.Target
	provider        provider.VMProvider
	registrar       registrar.RunnerRegistrar
	spawner         *spawn.Spawner
	leases          *lease.Registry
	session         *lease.AgentSession
	logger          *slog.Logger
	effectivePrefix string // VMNamePrefix + hostID + "-"; scopes reclaim to this host

	mu           sync.Mutex
	active       int
	failures     int
	shortExits   int
	backoffUntil time.Time
	shutdown     bool
	drain        bool
	lowDisk      bool
	fatalErr     error
	fatalCh      chan error
	inFlight     sync.WaitGroup
	inFlightVMs  map[string]struct{}
}

func New(cfg *config.Config, tgt target.Target, vmProvider provider.VMProvider, reg registrar.RunnerRegistrar, logger *slog.Logger) *Pool {
	if logger == nil {
		logger = slog.Default()
	}
	leases := lease.NewRegistry(cfg.StateDir)
	hostID := hostid.Load(cfg.StateDir)
	effectivePrefix := cfg.VMNamePrefix + hostID + "-"
	return &Pool{
		cfg:             cfg,
		tgt:             tgt,
		provider:        vmProvider,
		registrar:       reg,
		leases:          leases,
		spawner:         spawn.New(spawn.Options{Config: cfg, Target: tgt, Provider: vmProvider, Registrar: reg, Leases: leases, Logger: logger}),
		logger:          logger,
		effectivePrefix: effectivePrefix,
		fatalCh:         make(chan error, 1),
		inFlightVMs:     make(map[string]struct{}),
	}
}

func (p *Pool) Run(ctx context.Context) error {
	session, err := p.leases.BeginAgentSession()
	if err != nil {
		return fmt.Errorf("begin agent session: %w", err)
	}
	p.session = session
	p.spawner.SetSession(session)

	if err := p.startupReclaim(ctx); err != nil {
		if registrar.IsUnauthorized(err) {
			return err
		}
		p.logger.Warn("startup reclaim failed", "error", err)
	}

	p.logger.Info("pool loop started",
		"pool_size", p.cfg.PoolSize,
		"check_interval", p.cfg.PoolCheckInterval.Duration().String(),
		"reconciliation_interval", p.cfg.ReconciliationInterval.Duration().String(),
		"effective_prefix", p.effectivePrefix,
	)
	p.tick(ctx)

	ticker := time.NewTicker(p.cfg.PoolCheckInterval.Duration())
	defer ticker.Stop()

	reconcileEvery := p.cfg.ReconciliationInterval.Duration()
	if reconcileEvery <= 0 {
		reconcileEvery = config.DefaultReconciliationInterval
	}
	reconcileTicker := time.NewTicker(reconcileEvery)
	defer reconcileTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return p.drainAndWait()
		case err := <-p.fatalCh:
			_ = p.drainAndWait()
			return err
		case <-ticker.C:
			p.tick(ctx)
		case <-reconcileTicker.C:
			if err := p.reclaim(ctx, false); err != nil {
				if registrar.IsUnauthorized(err) {
					_ = p.drainAndWait()
					return err
				}
				p.logger.Warn("reconciliation failed", "error", err)
			}
		}
	}
}

func (p *Pool) BeginShutdown() {
	p.mu.Lock()
	p.shutdown = true
	p.mu.Unlock()
}

func (p *Pool) drainAndWait() error {
	p.mu.Lock()
	p.drain = true
	p.mu.Unlock()
	p.inFlight.Wait()
	return nil
}

func (p *Pool) tick(ctx context.Context) {
	p.mu.Lock()
	if p.shutdown || p.drain || p.lowDisk {
		p.mu.Unlock()
		return
	}
	if time.Now().Before(p.backoffUntil) {
		p.mu.Unlock()
		return
	}
	if p.active >= p.cfg.PoolSize {
		p.mu.Unlock()
		return
	}
	p.active++
	p.mu.Unlock()

	p.inFlight.Add(1)
	go func() {
		defer p.inFlight.Done()
		defer func() {
			p.mu.Lock()
			p.active--
			p.mu.Unlock()
		}()

		vmName, err := p.newVMName()
		if err != nil {
			p.recordFailure(err)
			return
		}

		p.logger.Info("spawning runner", "vm", vmName)

		p.mu.Lock()
		p.inFlightVMs[vmName] = struct{}{}
		p.mu.Unlock()
		defer func() {
			p.mu.Lock()
			delete(p.inFlightVMs, vmName)
			p.mu.Unlock()
		}()

		if result, err := p.spawner.Run(ctx, vmName); err != nil {
			if registrar.IsUnauthorized(err) {
				p.reportFatal(err)
				return
			}
			p.recordFailure(err)
			p.logger.Warn("spawn failed", "vm", vmName, "error", err)
			return
		} else {
			p.handleSpawnSuccess(vmName, result)
		}
	}()
}

func (p *Pool) reportFatal(err error) {
	p.mu.Lock()
	already := p.fatalErr != nil
	if !already {
		p.fatalErr = err
		p.shutdown = true
	}
	p.mu.Unlock()
	if already {
		return
	}
	p.logger.Error("stopping agent", "error", err)
	select {
	case p.fatalCh <- err:
	default:
	}
}

// maxUnclaimedJobMs is the upper bound for the job phase when a JIT runner
// exits without ever claiming work. GitHub's idle timeout is ~28 s; allow
// margin for bootstrap overhead inside the job phase.
const maxUnclaimedJobMs int64 = 50_000

// maxConsecutiveShortExits stops the agent after repeated unclaimed exits.
const maxConsecutiveShortExits = 5

func isUnclaimedJobExit(result spawn.Result) bool {
	return result.JobMs > 0 && result.JobMs < maxUnclaimedJobMs
}

func (p *Pool) handleSpawnSuccess(vmName string, result spawn.Result) {
	p.logger.Info("runner finished",
		"vm", vmName,
		"duration_ms", result.TotalMs,
		"job_ms", result.JobMs,
	)
	if isUnclaimedJobExit(result) {
		if err := p.recordShortExit(vmName, result); err != nil {
			p.reportFatal(err)
		}
		return
	}
	p.resetSpawnBackoff()
}

func (p *Pool) resetSpawnBackoff() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures = 0
	p.shortExits = 0
	p.backoffUntil = time.Time{}
}

func (p *Pool) recordShortExit(vmName string, result spawn.Result) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shortExits++
	backoff := time.Duration(p.shortExits) * 30 * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	p.backoffUntil = time.Now().Add(backoff)
	p.logger.Warn("runner finished quickly without claiming a job; check that runner_version is current",
		"vm", vmName,
		"job_ms", result.JobMs,
		"duration_ms", result.TotalMs,
		"consecutive_short_exits", p.shortExits,
		"backoff", backoff,
	)
	if p.shortExits >= maxConsecutiveShortExits {
		return fmt.Errorf("runner exited without claiming a job %d times in a row; check runner_version in config.yaml and the base image", p.shortExits)
	}
	return nil
}

func (p *Pool) recordFailure(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures++
	backoff := time.Duration(p.failures) * 10 * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	p.backoffUntil = time.Now().Add(backoff)
	p.logger.Warn("spawn backoff", "failures", p.failures, "backoff", backoff, "error", err)
}

func (p *Pool) newVMName() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s", p.effectivePrefix, hex.EncodeToString(buf)), nil
}

func (p *Pool) PurgeAll(ctx context.Context, dryRun bool) ([]provider.VM, []int64, error) {
	return p.purgeAllManaged(ctx, dryRun)
}
