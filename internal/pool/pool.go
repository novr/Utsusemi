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
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/spawn"
	"github.com/novr/utsusemi/internal/target"
)

type Pool struct {
	cfg       *config.Config
	tgt       target.Target
	provider  provider.VMProvider
	registrar registrar.RunnerRegistrar
	spawner   *spawn.Spawner
	logger    *slog.Logger

	mu           sync.Mutex
	active       int
	spawning     bool
	failures     int
	backoffUntil time.Time
	shutdown     bool
	drain        bool
	inFlight     sync.WaitGroup
	inFlightVMs  map[string]struct{}
}

func New(cfg *config.Config, tgt target.Target, vmProvider provider.VMProvider, reg registrar.RunnerRegistrar, logger *slog.Logger) *Pool {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pool{
		cfg:         cfg,
		tgt:         tgt,
		provider:    vmProvider,
		registrar:   reg,
		spawner:     spawn.New(spawn.Options{Config: cfg, Target: tgt, Provider: vmProvider, Registrar: reg, Logger: logger}),
		logger:      logger,
		inFlightVMs: make(map[string]struct{}),
	}
}

func (p *Pool) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.cfg.PoolCheckInterval.Duration())
	defer ticker.Stop()

	reconcileEvery := p.cfg.ReconciliationInterval.Duration()
	if reconcileEvery <= 0 {
		reconcileEvery = config.DefaultReconciliationInterval
	}
	reconcileTicker := time.NewTicker(reconcileEvery)
	defer reconcileTicker.Stop()

	if err := p.reconcile(ctx, true); err != nil {
		p.logger.Warn("initial reconciliation failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return p.drainAndWait()
		case <-ticker.C:
			p.tick(ctx)
		case <-reconcileTicker.C:
			if err := p.reconcile(ctx, false); err != nil {
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
	if p.shutdown || p.drain {
		p.mu.Unlock()
		return
	}
	if p.spawning {
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
	p.spawning = true
	p.active++
	p.mu.Unlock()

	p.inFlight.Add(1)
	go func() {
		defer p.inFlight.Done()
		defer func() {
			p.mu.Lock()
			p.spawning = false
			p.active--
			p.mu.Unlock()
		}()

		vmName, err := p.newVMName()
		if err != nil {
			p.recordFailure(err)
			return
		}

		p.mu.Lock()
		p.inFlightVMs[vmName] = struct{}{}
		p.mu.Unlock()
		defer func() {
			p.mu.Lock()
			delete(p.inFlightVMs, vmName)
			p.mu.Unlock()
		}()

		if err := p.spawner.Run(ctx, vmName); err != nil {
			p.recordFailure(err)
			p.logger.Warn("spawn failed", "vm", vmName, "error", err)
			return
		}
		p.mu.Lock()
		p.failures = 0
		p.backoffUntil = time.Time{}
		p.mu.Unlock()
	}()
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
	return fmt.Sprintf("%s%s", p.cfg.VMNamePrefix, hex.EncodeToString(buf)), nil
}

func (p *Pool) reconcile(ctx context.Context, startup bool) error {
	freeGB, err := p.provider.FreeDiskGB(ctx)
	if err != nil {
		return err
	}
	if freeGB < float64(p.cfg.MinFreeDiskGB) {
		p.logger.Warn("low disk space", "free_gb", freeGB)
	}

	vms, err := p.provider.ListManaged(ctx, p.cfg.VMNamePrefix)
	if err != nil {
		return err
	}

	p.mu.Lock()
	inFlight := make(map[string]struct{}, len(p.inFlightVMs))
	for name := range p.inFlightVMs {
		inFlight[name] = struct{}{}
	}
	p.mu.Unlock()

	known := make(map[string]struct{}, len(vms))
	for _, vm := range vms {
		known[vm.Name] = struct{}{}
		if !startup {
			if _, ok := inFlight[vm.Name]; ok {
				continue
			}
		}
		if err := p.provider.Delete(ctx, vm.Name); err != nil {
			p.logger.Warn("delete managed vm failed", "vm", vm.Name, "error", err)
		}
	}

	runners, err := p.registrar.ListRunners(ctx, p.tgt, p.cfg.VMNamePrefix)
	if err != nil {
		p.logger.Warn("list runners failed", "error", err)
		return nil
	}
	for _, runner := range runners {
		if _, ok := known[runner.Name]; ok {
			if !startup {
				if _, active := inFlight[runner.Name]; active {
					continue
				}
			}
		}
		if err := p.registrar.DeleteRunner(ctx, p.tgt, runner.ID); err != nil {
			p.logger.Warn("delete orphan runner failed", "runner", runner.Name, "error", err)
		}
	}
	return nil
}
